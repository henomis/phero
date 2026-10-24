// Copyright 2026 Simone Vellei
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package permission

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/henomis/phero/v2/llm"
)

// bashTool is the name of the tool whose subject is a shell command line.
const bashTool = "bash"

// Subject arguments of the built-in tools.
const (
	argFilePath = "file_path"
	argPath     = "path"
)

// defaultSubjects names the argument that is the subject of the built-in
// tools' calls.
var defaultSubjects = map[string]string{
	bashTool:     "command",
	"read":       argFilePath,
	"write":      argFilePath,
	"edit":       argFilePath,
	"multi_edit": argFilePath,
	"glob":       argPath,
	"grep":       argPath,
	"web_fetch":  "url",
}

// isPathTool reports whether tool is a built-in tool whose subject is a file
// path: its subjects and rule patterns are cleaned, made absolute against the
// working directory and resolved through symlinks before they are matched.
func isPathTool(tool string) bool {
	arg := defaultSubjects[tool]
	return arg == argFilePath || arg == argPath
}

// Answer is a human's answer to a permission question.
type Answer int

const (
	// Reject refuses the call.
	Reject Answer = iota
	// AllowOnce runs the call.
	AllowOnce
	// AllowAlways runs the call, and allows the same subject from now on.
	AllowAlways
)

// Request is the question an Asker answers.
type Request struct {
	// Tool is the tool's name.
	Tool string
	// CallID is the model's tool call ID, when the agent set it in the context.
	CallID string
	// Arguments is the call's raw JSON arguments.
	Arguments string
	// Subject is what the call touches, such as the command or the file path.
	Subject string
}

// Asker asks a human whether a tool call may run. An error denies the call
// and is returned to the model.
type Asker func(ctx context.Context, req Request) (Answer, error)

// Policy decides whether tool calls may run. Install it on tools with Apply.
// It is safe for concurrent use.
type Policy struct {
	rules      []rule
	specs      []pendingRule // rules given as options, parsed once all are applied
	fallback   Decision
	asker      Asker
	args       map[string]string // the subject argument of each tool
	workingDir string

	mu     sync.Mutex // serializes questions and guards always
	always map[string][]string
}

// pendingRule is a rule given as an option.
type pendingRule struct {
	spec     string
	decision Decision
}

// Option configures a Policy.
type Option func(*Policy) error

// WithAllow adds rules that allow calls.
func WithAllow(specs ...string) Option {
	return withRules(Allow, specs)
}

// WithAsk adds rules that ask before calls.
func WithAsk(specs ...string) Option {
	return withRules(Ask, specs)
}

// WithDeny adds rules that deny calls.
func WithDeny(specs ...string) Option {
	return withRules(Deny, specs)
}

func withRules(decision Decision, specs []string) Option {
	return func(p *Policy) error {
		for _, spec := range specs {
			p.specs = append(p.specs, pendingRule{spec: spec, decision: decision})
		}

		return nil
	}
}

// WithWorkingDir sets the directory that relative and empty file paths are
// resolved against, in subjects and in rule patterns. Set it to the file
// tools' working directory. It is the process's working directory unless set.
func WithWorkingDir(dir string) Option {
	return func(p *Policy) error {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return err
		}

		p.workingDir = abs

		return nil
	}
}

// WithDefault sets the decision for calls no rule matches. It is Ask unless
// set.
func WithDefault(d Decision) Option {
	return func(p *Policy) error {
		p.fallback = d
		return nil
	}
}

// WithAsker sets who answers when a call must be asked. Without one, such
// calls are denied.
func WithAsker(a Asker) Option {
	return func(p *Policy) error {
		p.asker = a
		return nil
	}
}

// WithSubject names the argument whose value rules match for tool's calls.
func WithSubject(tool, argument string) Option {
	return func(p *Policy) error {
		p.args[tool] = argument
		return nil
	}
}

// New returns a Policy with the given rules. It returns ErrInvalidRule for a
// rule it cannot parse.
func New(opts ...Option) (*Policy, error) {
	p := &Policy{fallback: Ask, args: map[string]string{}, always: map[string][]string{}}
	for tool, argument := range defaultSubjects {
		p.args[tool] = argument
	}

	if wd, err := os.Getwd(); err == nil {
		p.workingDir = wd
	}

	for _, opt := range opts {
		if err := opt(p); err != nil {
			return nil, err
		}
	}

	// Path patterns depend on the working directory, which any option may set.
	for _, pending := range p.specs {
		r, err := p.parseRule(pending.spec, pending.decision)
		if err != nil {
			return nil, err
		}

		p.rules = append(p.rules, r)
	}

	return p, nil
}

// Allow adds allow rules while the Policy is in use, for example the tools a
// skill pre-approves when it is loaded. It returns ErrInvalidRule for a rule
// it cannot parse, and then adds none.
func (p *Policy) Allow(specs ...string) error {
	rules := make([]rule, 0, len(specs))

	for _, spec := range specs {
		r, err := p.parseRule(spec, Allow)
		if err != nil {
			return err
		}

		rules = append(rules, r)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	p.rules = append(p.rules, rules...)

	return nil
}

// Apply installs the Policy on tools, as a tool middleware.
func (p *Policy) Apply(tools ...*llm.Tool) {
	for _, t := range tools {
		t.Use(p.Middleware())
	}
}

// Middleware returns the tool middleware that enforces the Policy.
func (p *Policy) Middleware() llm.ToolMiddleware {
	return func(tool *llm.Tool, next llm.ToolHandler) llm.ToolHandler {
		return func(ctx context.Context, arguments string) (any, error) {
			if err := p.check(ctx, tool.Name(), arguments); err != nil {
				return nil, err
			}

			return next(ctx, arguments)
		}
	}
}

// Decide returns what the Policy does with a call of tool with arguments,
// before asking anyone, and the subjects it decided on.
func (p *Policy) Decide(tool, arguments string) (Decision, []string) {
	subjects, ambiguous := p.subjects(tool, arguments)
	if ambiguous {
		return Deny, subjects
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	return p.decide(tool, subjects), subjects
}

// check enforces the Policy on one call.
func (p *Policy) check(ctx context.Context, tool, arguments string) error {
	decision, subjects := p.Decide(tool, arguments)

	subject := joinSubjects(subjects)
	if subjects == nil {
		subject, _, _ = p.subjectValue(tool, arguments)
	}

	switch decision {
	case Allow:
		return nil
	case Deny:
		reason := "denied by a rule"
		if _, _, ambiguous := p.subjectValue(tool, arguments); ambiguous {
			reason = "the arguments give the " + p.args[tool] + " argument more than once"
		}

		return &DeniedError{Tool: tool, Subject: subject, Reason: reason}
	case Ask:
	}

	// One question at a time; an earlier answer may have allowed it since.
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.decide(tool, subjects) == Allow {
		return nil
	}

	if p.asker == nil {
		return &DeniedError{Tool: tool, Subject: subject, Reason: "needs approval and no one can be asked"}
	}

	req := Request{Tool: tool, Arguments: arguments, Subject: subject}
	if call, ok := llm.ToolCallFromContext(ctx); ok {
		req.CallID = call.ID
	}

	answer, err := p.asker(ctx, req)
	if err != nil {
		return &DeniedError{Tool: tool, Subject: subject, Reason: err.Error()}
	}

	switch answer {
	case AllowAlways:
		p.always[tool] = append(p.always[tool], subjects...)
		return nil
	case AllowOnce:
		return nil
	case Reject:
	}

	return &DeniedError{Tool: tool, Subject: subject, Reason: "denied by the user"}
}

// decide combines the decisions of every subject: deny wins over ask, which
// wins over allow. The caller holds p.mu.
func (p *Policy) decide(tool string, subjects []string) Decision {
	if subjects == nil {
		return Ask // a command line it cannot see through
	}

	result := Allow

	for _, subject := range subjects {
		switch d := p.decideOne(tool, subject); d {
		case Deny:
			return Deny
		case Ask:
			result = Ask
		case Allow:
		}
	}

	return result
}

// decideOne applies the rules to one subject.
func (p *Policy) decideOne(tool, subject string) Decision {
	matched := map[Decision]bool{}

	for _, r := range p.rules {
		if r.matches(tool, subject) {
			matched[r.decision] = true
		}
	}

	switch {
	case matched[Deny]:
		return Deny
	case matched[Ask]:
		return Ask
	case matched[Allow], slices.Contains(p.always[tool], subject):
		return Allow
	default:
		return p.fallback
	}
}

// subjects returns what a call touches: the commands of a bash command line
// (nil when it cannot be split safely), or the subject argument's value. It
// reports true when the arguments give the subject argument more than once.
func (p *Policy) subjects(tool, arguments string) ([]string, bool) {
	value, ok, ambiguous := p.subjectValue(tool, arguments)
	if ambiguous {
		return nil, true
	}

	if !ok {
		return []string{""}, false
	}

	if isPathTool(tool) {
		return []string{p.resolvePath(value)}, false
	}

	if tool != bashTool {
		return []string{value}, false
	}

	commands, ok := splitShell(value)
	if !ok {
		return nil, false
	}

	if len(commands) == 0 {
		return []string{""}, false
	}

	return commands, false
}

// subjectValue returns the value of the subject argument of a call of tool,
// false when tool has no subject argument, and true when the arguments give
// it more than once.
//
// The value is the one the tool's handler decodes: encoding/json matches
// object keys to struct fields case-insensitively and keeps the last
// duplicate. Arguments that give the subject twice are reported as ambiguous
// rather than resolved, so the rules and the handler cannot see two values.
func (p *Policy) subjectValue(tool, arguments string) (string, bool, bool) {
	argument, ok := p.args[tool]
	if !ok {
		return "", false, false
	}

	value, count := topLevelString(arguments, argument)

	return value, true, count > 1
}

// topLevelString returns the string value of key in the JSON object
// arguments, matched case-insensitively, and how many keys matched. Arguments
// that do not parse have no value: the call matches only rules without a
// pattern.
func topLevelString(arguments, key string) (string, int) {
	dec := json.NewDecoder(strings.NewReader(arguments))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return "", 0
	}

	var (
		value string
		count int
	)

	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return value, count
		}

		var raw json.RawMessage
		if err = dec.Decode(&raw); err != nil {
			return value, count
		}

		if name, isString := tok.(string); !isString || !strings.EqualFold(name, key) {
			continue
		}

		count++
		value = ""
		_ = json.Unmarshal(raw, &value)
	}

	return value, count
}

// parseRule parses a rule and, for a file tool, resolves its path pattern.
func (p *Policy) parseRule(spec string, decision Decision) (rule, error) {
	return parseRule(spec, decision, func(tool, pattern string) string {
		if !isPathTool(strings.ToLower(tool)) {
			return pattern
		}

		return p.resolvePattern(pattern)
	})
}

// resolvePath cleans path, makes it absolute against the working directory
// and resolves the symlinks of its longest existing prefix, as the file tools
// do before they open it. An empty path is the working directory.
func (p *Policy) resolvePath(path string) string {
	if path == "" {
		path = p.workingDir
	}

	if !filepath.IsAbs(path) && p.workingDir != "" {
		path = filepath.Join(p.workingDir, path)
	}

	path = filepath.Clean(path)
	if !filepath.IsAbs(path) {
		return path
	}

	var missing []string

	for current := path; ; {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}

			return resolved
		}

		parent := filepath.Dir(current)
		if !errors.Is(err, os.ErrNotExist) || parent == current {
			return path
		}

		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

// resolvePattern resolves the directory before a path pattern's first
// wildcard as resolvePath does, so the pattern and the subjects it is matched
// with name files the same way.
func (p *Policy) resolvePattern(pattern string) string {
	star := strings.IndexByte(pattern, '*')
	if star < 0 {
		return p.resolvePath(pattern)
	}

	slash := strings.LastIndexByte(pattern[:star], '/')
	if slash < 0 {
		return pattern // "*.env": no directory to resolve
	}

	dir := pattern[:slash]
	if dir == "" {
		dir = "/"
	}

	return strings.TrimSuffix(p.resolvePath(dir), "/") + pattern[slash:]
}

// joinSubjects renders subjects for an error or a question.
func joinSubjects(subjects []string) string {
	return strings.Join(subjects, " ; ")
}
