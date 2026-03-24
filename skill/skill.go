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
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v2"

	"github.com/henomis/phero/agent"
	"github.com/henomis/phero/llm"
	"github.com/henomis/phero/memory"
	"github.com/henomis/phero/tool/bash"
	"github.com/henomis/phero/tool/file"
)

const (
	defaultSkillsRootPath = "skills"
	skillFileName         = "SKILL.md"
	yamlFrontmatterDelim  = "---"
	toolNameView          = "view"
	toolNameCreateFile    = "create_file"
	toolNameStrReplace    = "str_replace"
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

	memory        memory.Memory `yaml:"-"`
	maxIterations int           `yaml:"-"`
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

// Option is a functional option for configuring a Skill when converting it to a tool.
type Option func(*Skill)

// WithMemory sets a memory.Memory instance to be used by the skill's agent.
func WithMemory(memory memory.Memory) Option {
	return func(s *Skill) {
		s.memory = memory
	}
}

// WithMaxIterations sets the maximum number of iterations the skill's agent can execute before stopping.
func WithMaxIterations(maxIterations int) Option {
	return func(s *Skill) {
		s.maxIterations = maxIterations
	}
}

// AsTool converts a Skill into an llm.FunctionTool.
func (s *Skill) AsTool(client llm.LLM, opts ...Option) (*llm.Tool, error) {
	skillAsAgent, err := agent.New(
		client,
		s.Name,
		s.Body,
	)
	if err != nil {
		return nil, err
	}

	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}

	if s.memory != nil {
		skillAsAgent.SetMemory(s.memory)
	}

	if s.maxIterations > 0 {
		skillAsAgent.SetMaxIterations(s.maxIterations)
	}

	if err := s.addDefaultTools(skillAsAgent); err != nil {
		return nil, err
	}

	return skillAsAgent.AsTool(s.Name, s.Description)
}

func (s *Skill) allowsTool(toolName string) bool {
	if strings.TrimSpace(s.AllowedTools) == "" {
		return true
	}

	for _, allowedTool := range strings.Fields(s.AllowedTools) {
		if allowedTool == toolName {
			return true
		}
	}

	return false
}

func (s *Skill) addDefaultTools(agent *agent.Agent) error {
	if s.allowsTool(toolNameView) {
		viewTool, err := file.NewViewTool(file.WithWorkingDirectory(s.RootPath))
		if err != nil {
			return err
		}
		if err := agent.AddTool(viewTool.Tool()); err != nil {
			return err
		}
	}

	if s.allowsTool(toolNameCreateFile) {
		createTool, err := file.NewCreateFileTool(file.WithWorkingDirectory(s.RootPath))
		if err != nil {
			return err
		}
		createToolLLM := createTool.Tool().Use(func(_ *llm.Tool, next llm.ToolHandler) llm.ToolHandler {
			return func(ctx context.Context, arguments string) (any, error) {
				var input *file.CreateFileInput
				if err := json.Unmarshal([]byte(arguments), &input); err != nil {
					return nil, &llm.ToolArgumentParseError{Err: err}
				}
				if err := createFileValidationFunc(ctx, input); err != nil {
					return nil, err
				}
				return next(ctx, arguments)
			}
		})
		if err := agent.AddTool(createToolLLM); err != nil {
			return err
		}
	}

	if s.allowsTool(toolNameStrReplace) {
		strReplaceTool, err := file.NewStrReplaceTool(file.WithWorkingDirectory(s.RootPath))
		if err != nil {
			return err
		}
		if err := agent.AddTool(strReplaceTool.Tool()); err != nil {
			return err
		}
	}

	if s.allowsTool(toolNameBash) {
		bashTool, err := bash.New(bash.WithWorkingDirectory(s.RootPath))
		if err != nil {
			return err
		}
		bashToolLLM := bashTool.Tool().Use(func(_ *llm.Tool, next llm.ToolHandler) llm.ToolHandler {
			return func(ctx context.Context, arguments string) (any, error) {
				var input *bash.Input
				if err := json.Unmarshal([]byte(arguments), &input); err != nil {
					return nil, &llm.ToolArgumentParseError{Err: err}
				}
				if err := bashValidationFunc(ctx, input); err != nil {
					return nil, err
				}
				return next(ctx, arguments)
			}
		})
		if err := agent.AddTool(bashToolLLM); err != nil {
			return err
		}
	}

	return nil
}

func createFileValidationFunc(_ context.Context, input *file.CreateFileInput) error {
	fmt.Printf("Do you want to write to the file '%s'? (y/N): ", input.Path)
	var permission string
	_, scanErr := fmt.Scanln(&permission)
	if scanErr != nil {
		return fmt.Errorf("user permission denied")
	}

	if strings.EqualFold(permission, "y") {
		return nil
	}

	return fmt.Errorf("user permission denied")
}

func bashValidationFunc(_ context.Context, input *bash.Input) error {
	fmt.Printf("Do you want to execute the bash command '%s'? (y/N): ", input.Command)
	var permission string
	_, scanErr := fmt.Scanln(&permission)
	if scanErr != nil {
		return fmt.Errorf("user permission denied")
	}

	if strings.EqualFold(permission, "y") {
		return nil
	}

	return fmt.Errorf("user permission denied")
}
