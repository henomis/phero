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
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v2"
)

const (
	defaultSkillsRootPath   = "skills"
	skillFileName           = "SKILL.md"
	yamlFrontmatterDelim    = "---"
	yamlFrontmatterSections = 3
)

// Parser discovers and parses skills under a root directory.
type Parser struct {
	root string
}

// Skill represents the parsed contents of a SKILL.md file.
//
// Fields correspond to YAML frontmatter keys. Body contains the remaining
// content after the frontmatter.
type Skill struct {
	RootPath      string         `yaml:"-"`
	Name          string         `yaml:"name"`
	Description   string         `yaml:"description"`
	License       string         `yaml:"license,omitempty"`
	Compatibility string         `yaml:"compatibility,omitempty"`
	Metadata      map[string]any `yaml:"metadata,omitempty"`
	AllowedTools  string         `yaml:"allowed-tools,omitempty"`
	Body          string         `yaml:"-"`
}

// Tools returns the entries of the skill's allowed-tools field: the tools
// pre-approved while the skill is in use. It accepts the space-separated form
// of the Agent Skills spec ("Read Grep") and the comma-separated form with
// patterns ("Bash(git status), Read"); a space inside parentheses belongs to
// the pattern.
func (s *Skill) Tools() []string {
	var (
		tools []string
		cur   strings.Builder
		depth int
	)

	flush := func() {
		if t := strings.TrimSpace(cur.String()); t != "" {
			tools = append(tools, t)
		}

		cur.Reset()
	}

	for _, r := range s.AllowedTools {
		switch {
		case r == '(':
			depth++
		case r == ')' && depth > 0:
			depth--
		case (r == ',' || r == ' ' || r == '\t' || r == '\n') && depth == 0:
			flush()
			continue
		}

		cur.WriteRune(r)
	}

	flush()

	return tools
}

// New returns a Parser rooted at skillsRootPath.
// If skillsRootPath is empty, it defaults to "skills".
func New(skillsRootPath string) *Parser {
	if skillsRootPath == "" {
		skillsRootPath = defaultSkillsRootPath
	}

	return &Parser{
		root: skillsRootPath,
	}
}

// List returns the names of subdirectories under the parser root that contain
// a SKILL.md file.
func (p *Parser) List() ([]string, error) {
	dirs := []string{}

	entries, err := os.ReadDir(p.root)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			skillPath := filepath.Join(p.root, entry.Name(), skillFileName)
			if _, statErr := os.Stat(skillPath); statErr == nil {
				dirs = append(dirs, entry.Name())
			}
		}
	}

	return dirs, nil
}

// Parse parses the SKILL.md for the given skillName from the parser root.
func (p *Parser) Parse(skillName string) (*Skill, error) {
	skillFile := filepath.Join(p.root, skillName, skillFileName)

	f, err := os.Open(skillFile) //nolint:gosec // path is constructed from a trusted root and skill name
	if err != nil {
		return nil, err
	}

	defer func() {
		_ = f.Close()
	}()

	skill, err := Parse(f)
	if err != nil {
		return nil, err
	}

	absPath, err := filepath.Abs(filepath.Join(p.root, skillName))
	if err != nil {
		return nil, err
	}

	skill.RootPath = absPath

	return skill, nil
}

// MultiParser discovers skills under several roots, such as a user's own
// skills and a project's. When two roots hold a skill directory of the same
// name, the later root wins.
type MultiParser struct {
	parsers []*Parser
}

// NewMulti returns a MultiParser over roots, in increasing precedence. A
// root that does not exist holds no skills.
func NewMulti(roots ...string) *MultiParser {
	m := &MultiParser{}
	for _, root := range roots {
		m.parsers = append(m.parsers, New(root))
	}

	return m
}

// List returns the skill directory names found under any root, once each.
func (m *MultiParser) List() ([]string, error) {
	seen := map[string]bool{}

	var names []string

	for _, p := range m.parsers {
		dirs, err := p.List()
		if errors.Is(err, os.ErrNotExist) {
			continue
		}

		if err != nil {
			return nil, err
		}

		for _, d := range dirs {
			if !seen[d] {
				seen[d] = true
				names = append(names, d)
			}
		}
	}

	return names, nil
}

// Parse parses skillName from the last root that holds it.
func (m *MultiParser) Parse(skillName string) (*Skill, error) {
	for i := len(m.parsers) - 1; i >= 0; i-- {
		skill, err := m.parsers[i].Parse(skillName)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}

		return skill, err
	}

	return nil, fmt.Errorf("%w: %s", os.ErrNotExist, skillName)
}

// Parse parses a skill definition from an io.Reader.
//
// The input must start with YAML frontmatter delimited by "---" and followed
// by a body.
func Parse(r io.Reader) (*Skill, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	content := string(data)

	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, yamlFrontmatterDelim) {
		return nil, ErrMissingYAMLFrontmatter
	}

	parts := strings.SplitN(content, yamlFrontmatterDelim, yamlFrontmatterSections)
	if len(parts) < yamlFrontmatterSections {
		return nil, ErrInvalidSkillFormat
	}

	yamlPart := parts[1]
	body := strings.TrimSpace(parts[2])

	var skill Skill
	if unmarshalErr := yaml.Unmarshal([]byte(yamlPart), &skill); unmarshalErr != nil {
		return nil, unmarshalErr
	}

	skill.Body = body

	return &skill, nil
}
