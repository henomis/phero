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
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/henomis/phero/llm"
	skillpkg "github.com/henomis/phero/skill"
)

type stubParser struct {
	list  func() ([]string, error)
	parse func(skillName string) (*skillpkg.Skill, error)
}

func (s *stubParser) List() ([]string, error) {
	return s.list()
}

func (s *stubParser) Parse(skillName string) (*skillpkg.Skill, error) {
	return s.parse(skillName)
}

func TestNewExposesFixedToolIdentity(t *testing.T) {
	tool, err := New("", WithParser(&stubParser{
		list: func() ([]string, error) { return []string{}, nil },
		parse: func(_ string) (*skillpkg.Skill, error) {
			return nil, errors.New("should not be called")
		},
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if got := tool.Tool().Name(); got != toolName {
		t.Fatalf("Tool().Name() = %q, want %q", got, toolName)
	}
	if got := tool.Tool().Description(); !strings.Contains(got, "<available_skills>") {
		t.Fatalf("Tool().Description() does not contain available skills section: %q", got)
	}
}

func TestHandleNilInput(t *testing.T) {
	tool, err := New("", WithParser(&stubParser{
		list: func() ([]string, error) { return []string{}, nil },
		parse: func(_ string) (*skillpkg.Skill, error) {
			return nil, errors.New("should not be called")
		},
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = tool.Tool().Handle(context.Background(), `null`)
	if !errors.Is(err, ErrNilInput) {
		t.Fatalf("Handle() error = %v, want %v", err, ErrNilInput)
	}
}

func TestHandleCommandRequired(t *testing.T) {
	tool, err := New("", WithParser(&stubParser{
		list: func() ([]string, error) { return []string{}, nil },
		parse: func(_ string) (*skillpkg.Skill, error) {
			return nil, errors.New("should not be called")
		},
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = tool.Tool().Handle(context.Background(), `{"command":"   "}`)
	if !errors.Is(err, ErrCommandRequired) {
		t.Fatalf("Handle() error = %v, want %v", err, ErrCommandRequired)
	}
}

func TestHandleInvalidCommandFormat(t *testing.T) {
	tool, err := New("", WithParser(&stubParser{
		list: func() ([]string, error) { return []string{}, nil },
		parse: func(_ string) (*skillpkg.Skill, error) {
			return nil, errors.New("should not be called")
		},
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = tool.Tool().Handle(context.Background(), `{"command":"pdf --force"}`)
	if !errors.Is(err, ErrInvalidCommandFormat) {
		t.Fatalf("Handle() error = %v, want %v", err, ErrInvalidCommandFormat)
	}
}

func TestHandleSkillNotFound(t *testing.T) {
	tool, err := New("", WithParser(&stubParser{
		list: func() ([]string, error) { return []string{"pdf"}, nil },
		parse: func(_ string) (*skillpkg.Skill, error) {
			return &skillpkg.Skill{Name: "pdf", Description: "Parse PDFs"}, nil
		},
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = tool.Tool().Handle(context.Background(), `{"command":"csv"}`)
	var notFound *SkillNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("Handle() error = %v, want SkillNotFoundError", err)
	}
}

func TestHandleSuccess(t *testing.T) {
	parseCalls := 0
	tool, err := New("", WithParser(&stubParser{
		list: func() ([]string, error) { return []string{"pdf"}, nil },
		parse: func(skillName string) (*skillpkg.Skill, error) {
			parseCalls++
			if skillName != "pdf" {
				return nil, errors.New("unexpected skill")
			}
			return &skillpkg.Skill{
				RootPath:    "/tmp/skills/pdf",
				Name:        "pdf",
				Description: "Extract and summarize PDF files",
				Body:        "Run parser.py then summarize the output.",
			}, nil
		},
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	res, err := tool.Tool().Handle(context.Background(), `{"command":"pdf"}`)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	out, ok := res.(*Output)
	if !ok {
		t.Fatalf("Handle() result type = %T, want *Output", res)
	}
	if out.CommandName != "pdf" {
		t.Fatalf("CommandName = %q, want %q", out.CommandName, "pdf")
	}
	if out.BasePath != "/tmp/skills/pdf" {
		t.Fatalf("BasePath = %q, want %q", out.BasePath, "/tmp/skills/pdf")
	}
	if !strings.Contains(out.Expansion, "Base Path: /tmp/skills/pdf") {
		t.Fatalf("Expansion missing base path: %q", out.Expansion)
	}
	if len(out.AvailableSkills) != 1 {
		t.Fatalf("AvailableSkills length = %d, want 1", len(out.AvailableSkills))
	}
	if parseCalls != 2 {
		t.Fatalf("parseCalls = %d, want 2 (catalog + runtime)", parseCalls)
	}
}

func TestBuildCatalogSortedByName(t *testing.T) {
	tool, err := New("", WithParser(&stubParser{
		list: func() ([]string, error) { return []string{"b", "a"}, nil },
		parse: func(skillName string) (*skillpkg.Skill, error) {
			if skillName == "b" {
				return &skillpkg.Skill{Name: "zeta", Description: "z"}, nil
			}
			return &skillpkg.Skill{Name: "alpha", Description: "a"}, nil
		},
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	res, err := tool.Tool().Handle(context.Background(), `{"command":"a"}`)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	out, ok := res.(*Output)
	if !ok {
		t.Fatalf("Handle() result type = %T, want *Output", res)
	}

	got := []string{out.AvailableSkills[0].Name, out.AvailableSkills[1].Name}
	want := []string{"alpha", "zeta"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AvailableSkills order = %v, want %v", got, want)
	}
}

func TestDuplicateSkillNameError(t *testing.T) {
	_, err := New("", WithParser(&stubParser{
		list: func() ([]string, error) { return []string{"s1", "s2"}, nil },
		parse: func(_ string) (*skillpkg.Skill, error) {
			return &skillpkg.Skill{Name: "pdf", Description: "Parse PDFs"}, nil
		},
	}))

	var duplicateErr *DuplicateSkillNameError
	if !errors.As(err, &duplicateErr) {
		t.Fatalf("New() error = %v, want DuplicateSkillNameError", err)
	}
}

func TestMiddlewareOrder(t *testing.T) {
	tool, err := New("", WithParser(&stubParser{
		list: func() ([]string, error) { return []string{"pdf"}, nil },
		parse: func(_ string) (*skillpkg.Skill, error) {
			return &skillpkg.Skill{Name: "pdf", Description: "Parse PDFs", Body: "Body", RootPath: "/tmp/pdf"}, nil
		},
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	steps := make([]string, 0, 4)
	m1 := func(_ *llm.Tool, next llm.ToolHandler) llm.ToolHandler {
		return func(ctx context.Context, arguments string) (any, error) {
			steps = append(steps, "m1-before")
			res, err := next(ctx, arguments)
			steps = append(steps, "m1-after")
			return res, err
		}
	}
	m2 := func(_ *llm.Tool, next llm.ToolHandler) llm.ToolHandler {
		return func(ctx context.Context, arguments string) (any, error) {
			steps = append(steps, "m2-before")
			res, err := next(ctx, arguments)
			steps = append(steps, "m2-after")
			return res, err
		}
	}

	tool.Tool().Use(m1, m2)
	_, err = tool.Tool().Handle(context.Background(), `{"command":"pdf"}`)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	want := []string{"m1-before", "m2-before", "m2-after", "m1-after"}
	if !reflect.DeepEqual(steps, want) {
		t.Fatalf("middleware steps = %v, want %v", steps, want)
	}
}

func TestDefaultAvailableSkillLocationUsesRootPathAndSkillDir(t *testing.T) {
	tool, err := New("./skills", WithParser(&stubParser{
		list: func() ([]string, error) { return []string{"pdf"}, nil },
		parse: func(_ string) (*skillpkg.Skill, error) {
			return &skillpkg.Skill{Name: "pdf", Description: "Parse PDFs", Body: "Body"}, nil
		},
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	res, err := tool.Tool().Handle(context.Background(), `{"command":"pdf"}`)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	out, ok := res.(*Output)
	if !ok {
		t.Fatalf("Handle() result type = %T, want *Output", res)
	}
	if len(out.AvailableSkills) != 1 {
		t.Fatalf("AvailableSkills length = %d, want 1", len(out.AvailableSkills))
	}

	wantLocation := "skills/pdf"

	if out.AvailableSkills[0].Location != wantLocation {
		t.Fatalf("Location = %q, want %q", out.AvailableSkills[0].Location, wantLocation)
	}
}

func TestWithAvailableSkillsLocationOverridesDerivedPath(t *testing.T) {
	tool, err := New("./skills", WithParser(&stubParser{
		list: func() ([]string, error) { return []string{"pdf"}, nil },
		parse: func(_ string) (*skillpkg.Skill, error) {
			return &skillpkg.Skill{Name: "pdf", Description: "Parse PDFs", Body: "Body"}, nil
		},
	}), WithAvailableSkillsLocation("project"))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	res, err := tool.Tool().Handle(context.Background(), `{"command":"pdf"}`)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	out, ok := res.(*Output)
	if !ok {
		t.Fatalf("Handle() result type = %T, want *Output", res)
	}
	if len(out.AvailableSkills) != 1 {
		t.Fatalf("AvailableSkills length = %d, want 1", len(out.AvailableSkills))
	}
	if out.AvailableSkills[0].Location != "project" {
		t.Fatalf("Location = %q, want %q", out.AvailableSkills[0].Location, "project")
	}
}

func TestBasePathFallsBackToRootPlusSkillName(t *testing.T) {
	tool, err := New("./skills", WithParser(&stubParser{
		list: func() ([]string, error) { return []string{"pdf"}, nil },
		parse: func(_ string) (*skillpkg.Skill, error) {
			return &skillpkg.Skill{
				Name:        "pdf",
				Description: "Parse PDFs",
				Body:        "Body",
				RootPath:    "",
			}, nil
		},
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	res, err := tool.Tool().Handle(context.Background(), `{"command":"pdf"}`)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	out, ok := res.(*Output)
	if !ok {
		t.Fatalf("Handle() result type = %T, want *Output", res)
	}

	if out.BasePath != "skills/pdf" {
		t.Fatalf("BasePath = %q, want %q", out.BasePath, "skills/pdf")
	}
}
