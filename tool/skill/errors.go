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

import "errors"

var (
	// ErrNilInput is returned when the tool handler receives nil input.
	ErrNilInput = errors.New("skill tool: nil input")
	// ErrCommandRequired is returned when the command field is empty.
	ErrCommandRequired = errors.New("skill tool: command is required")
	// ErrInvalidCommandFormat is returned when command includes arguments.
	ErrInvalidCommandFormat = errors.New("skill tool: command must contain only the skill name")
	// ErrSkillNameRequired is returned when a discovered skill has no frontmatter name.
	ErrSkillNameRequired = errors.New("skill tool: skill name is required")
	// ErrSkillDescriptionRequired is returned when a discovered skill has no description.
	ErrSkillDescriptionRequired = errors.New("skill tool: skill description is required")
)

// SkillNotFoundError is returned when a command references an unknown skill.
type SkillNotFoundError struct {
	Command string
}

// Error returns the formatted error message.
func (e *SkillNotFoundError) Error() string {
	return "skill tool: skill not found: " + e.Command
}

// DuplicateSkillNameError is returned when multiple folders expose the same skill name.
type DuplicateSkillNameError struct {
	Name         string
	ExistingDir  string
	DuplicateDir string
}

// Error returns the formatted error message.
func (e *DuplicateSkillNameError) Error() string {
	return "skill tool: duplicate skill name '" + e.Name + "' found in '" + e.ExistingDir + "' and '" + e.DuplicateDir + "'"
}
