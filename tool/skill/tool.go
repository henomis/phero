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

package skill

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/henomis/phero/v2/llm"
	skillpkg "github.com/henomis/phero/v2/skill"
)

const (
	toolName            = "skill"
	catalogSizeMultiple = 2
)

// Parser defines the minimal skill discovery/parsing contract used by this tool.
type Parser interface {
	List() ([]string, error)
	Parse(skillName string) (*skillpkg.Skill, error)
}

// Option configures a skill dispatcher tool.
type Option func(*Tool)

// Input defines the JSON input schema accepted by the tool.
type Input struct {
	Command   string `json:"command" jsonschema:"description=The skill name to load (no arguments)."`
	Arguments string `json:"arguments,omitempty" jsonschema_description:"Arguments for the skill, or an empty string"` //nolint:lll
}

// AvailableSkill contains frontmatter metadata surfaced in the tool catalog.
type AvailableSkill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Location    string `json:"location"`
}

// Output defines the tool result used to expand prompt instructions.
type Output struct {
	LaunchMessage   string           `json:"launch_message"`
	CommandMessage  string           `json:"command_message"`
	CommandName     string           `json:"command_name"`
	BasePath        string           `json:"base_path"`
	Instructions    string           `json:"instructions"`
	Expansion       string           `json:"expansion"`
	AvailableSkills []AvailableSkill `json:"available_skills"`
	// AllowedTools lists the tools the skill pre-approves (its allowed-tools).
	AllowedTools []string `json:"allowed_tools,omitempty"`
}

// Tool wraps an llm.Tool that dispatches skill instruction loading.
type Tool struct {
	tool     *llm.Tool
	parser   Parser
	catalog  []catalogEntry
	commands map[string]catalogEntry
	skipped  []error
	onLoad   func(ctx context.Context, skill *skillpkg.Skill)
}

type catalogEntry struct {
	dir       string
	skillPath string
	skill     AvailableSkill
}

// WithRoots reads skills from several roots, such as the user's own and the
// project's; when two hold a skill directory of the same name, the earlier
// root wins, so pass the user's root first: a cloned project then cannot
// shadow a skill the user trusts. It replaces the root given to New.
func WithRoots(roots ...string) Option {
	return func(t *Tool) {
		t.parser = skillpkg.NewMulti(roots...)
	}
}

// WithOnLoad calls fn each time the model loads a skill, with the tool call's
// context, for example to pre-approve the skill's allowed tools in a
// permission policy for the current run only, leaving out broad bash grants:
//
//	skilltool.WithOnLoad(func(ctx context.Context, s *skill.Skill) {
//		granted, _ := permission.SkillGrants(s.Tools())
//		_ = policy.AllowForRun(ctx, granted...)
//	})
func WithOnLoad(fn func(ctx context.Context, skill *skillpkg.Skill)) Option {
	return func(t *Tool) {
		t.onLoad = fn
	}
}

// WithParser injects a custom parser, useful for tests.
func WithParser(parser Parser) Option {
	return func(t *Tool) {
		t.parser = parser
	}
}

// New creates a new skill dispatcher tool.
//
// skillsRootPath is the directory containing skill subfolders with SKILL.md files.
// A skill that cannot be used, because its SKILL.md does not parse, it has no
// name or description, or its name is taken, is left out of the catalog and
// reported by Skipped; the others still load. New fails only when the skills
// cannot be listed.
func New(skillsRootPath string, opts ...Option) (*Tool, error) {
	skillsRootPath = strings.TrimSpace(skillsRootPath)

	t := &Tool{
		parser: skillpkg.New(skillsRootPath),
	}

	for _, opt := range opts {
		if opt != nil {
			opt(t)
		}
	}

	if err := t.buildCatalog(skillsRootPath); err != nil {
		return nil, err
	}

	description := t.buildDescription()

	tool, err := llm.NewTool(toolName, description, t.handle)
	if err != nil {
		return nil, err
	}

	t.tool = tool

	return t, nil
}

// Tool returns the underlying llm.Tool.
func (t *Tool) Tool() *llm.Tool {
	return t.tool
}

// Skipped returns why each skill left out of the catalog was skipped: a
// SKILL.md that does not parse, ErrSkillNameRequired or
// ErrSkillDescriptionRequired (each wrapped with the skill's directory), or a
// *DuplicateSkillNameError when an earlier skill, such as one from an earlier
// root (WithRoots), has the same name. It is nil when every skill loaded.
func (t *Tool) Skipped() []error {
	return slices.Clone(t.skipped)
}

// buildCatalog lists and parses the skills, skipping those that cannot be
// used (see Skipped). The first skill of a name wins: the parser lists an
// earlier root's skills first.
//
//nolint:funlen
func (t *Tool) buildCatalog(skillsRootPath string) error {
	dirs, err := t.parser.List()
	if err != nil {
		return err
	}

	entries := make([]catalogEntry, 0, len(dirs))
	commands := make(map[string]catalogEntry, len(dirs)*catalogSizeMultiple)

	for _, dir := range dirs {
		skillItem, parseErr := t.parser.Parse(dir)
		if parseErr != nil {
			t.skipped = append(t.skipped, fmt.Errorf("skill tool: skill %q: %w", dir, parseErr))
			continue
		}

		name := strings.TrimSpace(skillItem.Name)
		if name == "" {
			t.skipped = append(t.skipped, fmt.Errorf("%w: %q", ErrSkillNameRequired, dir))
			continue
		}

		description := strings.TrimSpace(skillItem.Description)
		if description == "" {
			t.skipped = append(t.skipped, fmt.Errorf("%w: %q", ErrSkillDescriptionRequired, dir))
			continue
		}

		skillPath := filepath.Clean(filepath.Join(skillsRootPath, dir))
		if skillItem.RootPath != "" {
			skillPath = skillItem.RootPath
		}

		entry := catalogEntry{
			dir:       dir,
			skillPath: skillPath,
			skill: AvailableSkill{
				Name:        name,
				Description: description,
				Location:    skillPath,
			},
		}

		normalizedName := strings.ToLower(name)
		// An earlier skill's name or directory name wins, so a later root
		// cannot take the command a skill the user trusts answers to.
		if existing, ok := commands[normalizedName]; ok {
			t.skipped = append(t.skipped, &DuplicateSkillNameError{Name: name, ExistingDir: existing.dir, DuplicateDir: dir})
			continue
		}

		commands[normalizedName] = entry

		normalizedDir := strings.ToLower(strings.TrimSpace(dir))
		if normalizedDir != "" {
			if _, exists := commands[normalizedDir]; !exists {
				commands[normalizedDir] = entry
			}
		}

		entries = append(entries, entry)
	}

	sort.Slice(entries, func(i, j int) bool {
		return strings.ToLower(entries[i].skill.Name) < strings.ToLower(entries[j].skill.Name)
	})

	t.catalog = entries
	t.commands = commands

	return nil
}

func (t *Tool) buildDescription() string {
	var b strings.Builder

	b.WriteString("Execute a skill within the main conversation.\n\n")
	b.WriteString("<skills_instructions>\n")
	b.WriteString("When users ask you to perform tasks, check if any available skill " +
		"can help complete the task more effectively.\n")
	b.WriteString("Invoke this tool with the skill name only in the command field, " +
		"and any arguments for the skill in the arguments field.\n")
	b.WriteString("Do not pass arguments inside command.\n")
	b.WriteString("After invocation, use the returned base path and instructions " +
		"to execute the task in the same conversation.\n")
	b.WriteString("</skills_instructions>\n\n")

	b.WriteString("<available_skills>\n")

	for _, entry := range t.catalog {
		b.WriteString("  <skill>\n")
		b.WriteString("    <name>")
		b.WriteString(escapeXML(entry.skill.Name))
		b.WriteString("</name>\n")
		b.WriteString("    <description>")
		b.WriteString(escapeXML(entry.skill.Description))
		b.WriteString("</description>\n")
		b.WriteString("    <location>")
		b.WriteString(escapeXML(entry.skill.Location))
		b.WriteString("</location>\n")
		b.WriteString("  </skill>\n")
	}

	b.WriteString("</available_skills>")

	return b.String()
}

func (t *Tool) handle(ctx context.Context, input *Input) (*Output, error) {
	if input == nil {
		return nil, ErrNilInput
	}

	command := strings.TrimSpace(input.Command)
	if command == "" {
		return nil, ErrCommandRequired
	}

	parts := strings.Fields(command)
	if len(parts) != 1 || parts[0] != command {
		return nil, ErrInvalidCommandFormat
	}

	entry, ok := t.commands[strings.ToLower(command)]
	if !ok {
		return nil, &NotFoundError{Command: command}
	}

	skillItem, err := t.parser.Parse(entry.dir)
	if err != nil {
		return nil, err
	}

	basePath := skillItem.RootPath
	if strings.TrimSpace(basePath) == "" {
		basePath = entry.skillPath
	}

	instructions := withArguments(strings.TrimSpace(skillItem.Body), strings.TrimSpace(input.Arguments))
	expansion := fmt.Sprintf("Base Path: %s\n\n%s", basePath, instructions)

	if t.onLoad != nil {
		t.onLoad(ctx, skillItem)
	}

	available := make([]AvailableSkill, 0, len(t.catalog))
	for _, item := range t.catalog {
		available = append(available, item.skill)
	}

	return &Output{
		LaunchMessage:   fmt.Sprintf("Launching skill: %s", entry.skill.Name),
		CommandMessage:  fmt.Sprintf("The \"%s\" skill is running", entry.skill.Name),
		CommandName:     entry.skill.Name,
		BasePath:        basePath,
		Instructions:    instructions,
		Expansion:       expansion,
		AvailableSkills: available,
		AllowedTools:    skillItem.Tools(),
	}, nil
}

// argumentsPlaceholder is replaced by the skill's arguments in its body.
const argumentsPlaceholder = "$ARGUMENTS"

// withArguments puts args into a skill's instructions: in place of
// $ARGUMENTS when the body has it, else after the body.
func withArguments(instructions, args string) string {
	if strings.Contains(instructions, argumentsPlaceholder) {
		return strings.ReplaceAll(instructions, argumentsPlaceholder, args)
	}

	if args == "" {
		return instructions
	}

	return instructions + "\n\nARGUMENTS: " + args
}

func escapeXML(s string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&apos;",
	)

	return replacer.Replace(s)
}
