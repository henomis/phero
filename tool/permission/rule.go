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
	"fmt"
	"regexp"
	"strings"
)

// Decision is what a Policy does with a tool call.
type Decision int

const (
	// Ask asks the Asker before running the call.
	Ask Decision = iota
	// Allow runs the call.
	Allow
	// Deny refuses the call.
	Deny
)

// String returns "ask", "allow" or "deny".
func (d Decision) String() string {
	switch d {
	case Allow:
		return "allow"
	case Deny:
		return "deny"
	case Ask:
		return "ask"
	default:
		return fmt.Sprintf("Decision(%d)", int(d))
	}
}

// rule is one parsed rule.
type rule struct {
	spec     string
	decision Decision
	tool     *regexp.Regexp
	subject  *regexp.Regexp // nil matches every call of the tool
}

// parseRule parses "name" or "name(pattern)".
func parseRule(spec string, decision Decision) (rule, error) {
	spec = strings.TrimSpace(spec)
	name, pattern, hasPattern := strings.Cut(spec, "(")

	if hasPattern {
		if !strings.HasSuffix(pattern, ")") {
			return rule{}, fmt.Errorf("%w %q: missing closing parenthesis", ErrInvalidRule, spec)
		}

		pattern = strings.TrimSuffix(pattern, ")")
	}

	name = strings.TrimSpace(name)
	if name == "" {
		return rule{}, fmt.Errorf("%w %q: missing tool name", ErrInvalidRule, spec)
	}

	r := rule{spec: spec, decision: decision, tool: compileWildcard(name)}
	if hasPattern {
		r.subject = compileWildcard(pattern)
	}

	return r, nil
}

// matches reports whether the rule applies to a call of tool on subject.
func (r rule) matches(tool, subject string) bool {
	return r.tool.MatchString(tool) && (r.subject == nil || r.subject.MatchString(subject))
}

// compileWildcard compiles a pattern where * matches any run of characters,
// slashes included, and everything else matches itself.
func compileWildcard(pattern string) *regexp.Regexp {
	parts := strings.Split(pattern, "*")
	for i, p := range parts {
		parts[i] = regexp.QuoteMeta(p)
	}

	return regexp.MustCompile("^" + strings.Join(parts, ".*") + "$")
}
