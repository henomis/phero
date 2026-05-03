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
	"sort"
	"strings"

	"github.com/henomis/phero/llm"
	skillpkg "github.com/henomis/phero/skill"
)

const (
	toolName = "skill"
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
	Command string `json:"command" jsonschema:"description=The skill name to load (no arguments)."`
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
}

// Tool wraps an llm.Tool that dispatches skill instruction loading.
type Tool struct {
	tool     *llm.Tool
	parser   Parser
	location string
	catalog  []catalogEntry
	commands map[string]catalogEntry
}

type catalogEntry struct {
	dir       string
	skillPath string
	skill     AvailableSkill
}

// WithParser injects a custom parser, useful for tests.
func WithParser(parser Parser) Option {
	return func(t *Tool) {
		t.parser = parser
	}
}

// WithAvailableSkillsLocation sets the location metadata value exposed in the catalog.
func WithAvailableSkillsLocation(location string) Option {
	return func(t *Tool) {
		t.location = strings.TrimSpace(location)
	}
}

// New creates a new skill dispatcher tool.
//
// skillsRootPath is the directory containing skill subfolders with SKILL.md files.
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

	entries, commands, err := t.buildCatalog(skillsRootPath)
	if err != nil {
		return nil, err
	}
	t.catalog = entries
	t.commands = commands

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

func (t *Tool) buildCatalog(skillsRootPath string) ([]catalogEntry, map[string]catalogEntry, error) {
	dirs, err := t.parser.List()
	if err != nil {
		return nil, nil, err
	}

	entries := make([]catalogEntry, 0, len(dirs))
	commands := make(map[string]catalogEntry, len(dirs)*2)

	for _, dir := range dirs {
		skillItem, err := t.parser.Parse(dir)
		if err != nil {
			return nil, nil, err
		}

		name := strings.TrimSpace(skillItem.Name)
		if name == "" {
			return nil, nil, ErrSkillNameRequired
		}

		description := strings.TrimSpace(skillItem.Description)
		if description == "" {
			return nil, nil, ErrSkillDescriptionRequired
		}

		skillPath := filepath.Clean(filepath.Join(skillsRootPath, dir))

		entry := catalogEntry{
			dir:       dir,
			skillPath: skillPath,
			skill: AvailableSkill{
				Name:        name,
				Description: description,
				Location:    t.skillLocation(skillPath),
			},
		}

		normalizedName := strings.ToLower(name)
		if existing, ok := commands[normalizedName]; ok {
			return nil, nil, &DuplicateSkillNameError{Name: name, ExistingDir: existing.dir, DuplicateDir: dir}
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

	return entries, commands, nil
}

func (t *Tool) skillLocation(skillPath string) string {
	if strings.TrimSpace(t.location) != "" {
		return t.location
	}

	return skillPath
}

func (t *Tool) buildDescription() string {
	var b strings.Builder

	b.WriteString("Execute a skill within the main conversation.\n\n")
	b.WriteString("<skills_instructions>\n")
	b.WriteString("When users ask you to perform tasks, check if any available skill can help complete the task more effectively.\n")
	b.WriteString("Invoke this tool with the skill name only, using the command field.\n")
	b.WriteString("Do not pass arguments inside command.\n")
	b.WriteString("After invocation, use the returned base path and instructions to execute the task in the same conversation.\n")
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

func (t *Tool) handle(_ context.Context, input *Input) (*Output, error) {
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
		return nil, &SkillNotFoundError{Command: command}
	}

	skillItem, err := t.parser.Parse(entry.dir)
	if err != nil {
		return nil, err
	}

	basePath := skillItem.RootPath
	if strings.TrimSpace(basePath) == "" {
		basePath = entry.skillPath
	}

	instructions := strings.TrimSpace(skillItem.Body)
	expansion := fmt.Sprintf("Base Path: %s\n\n%s", basePath, instructions)

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
	}, nil
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
