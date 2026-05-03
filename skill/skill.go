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
	"io"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v2"
)

const (
	defaultSkillsRootPath = "skills"
	skillFileName         = "SKILL.md"
	yamlFrontmatterDelim  = "---"
	toolNameRead          = "read"
	toolNameWrite         = "write"
	toolNameEdit          = "edit"
	toolNameBash          = "bash"
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
			if _, err := os.Stat(skillPath); err == nil {
				dirs = append(dirs, entry.Name())
			}
		}
	}
	return dirs, nil
}

// Parse parses the SKILL.md for the given skillName from the parser root.
func (p *Parser) Parse(skillName string) (*Skill, error) {
	skillFile := filepath.Join(p.root, skillName, skillFileName)
	f, err := os.Open(skillFile)
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
	parts := strings.SplitN(content, yamlFrontmatterDelim, 3)
	if len(parts) < 3 {
		return nil, ErrInvalidSkillFormat
	}
	yamlPart := parts[1]
	body := strings.TrimSpace(parts[2])

	var skill Skill
	if err := yaml.Unmarshal([]byte(yamlPart), &skill); err != nil {
		return nil, err
	}
	skill.Body = body
	return &skill, nil
}
