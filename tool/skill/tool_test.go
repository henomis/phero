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
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/henomis/phero/v2/llm"
	skillpkg "github.com/henomis/phero/v2/skill"
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

	var notFound *NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("Handle() error = %v, want NotFoundError", err)
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

func TestDuplicateSkillNameIsSkipped(t *testing.T) {
	tool, err := New("", WithParser(&stubParser{
		list: func() ([]string, error) { return []string{"s1", "s2"}, nil },
		parse: func(dir string) (*skillpkg.Skill, error) {
			return &skillpkg.Skill{Name: "pdf", Description: "Parse PDFs from " + dir}, nil
		},
	}))
	if err != nil {
		t.Fatalf("New() error = %v, want the first skill loaded", err)
	}

	var duplicateErr *DuplicateSkillNameError

	skipped := tool.Skipped()
	if len(skipped) != 1 || !errors.As(skipped[0], &duplicateErr) || duplicateErr.DuplicateDir != "s2" {
		t.Fatalf("Skipped() = %v, want s2 as a DuplicateSkillNameError", skipped)
	}

	if d := tool.Tool().Description(); !strings.Contains(d, "from s1") || strings.Contains(d, "from s2") {
		t.Fatalf("catalog = %s, want the first skill only", d)
	}
}

func TestBadSkillsAreSkipped(t *testing.T) {
	broken := errors.New("bad frontmatter")

	tool, err := New("", WithParser(&stubParser{
		list: func() ([]string, error) { return []string{"good", "broken", "nameless", "undescribed"}, nil },
		parse: func(dir string) (*skillpkg.Skill, error) {
			switch dir {
			case "broken":
				return nil, broken
			case "nameless":
				return &skillpkg.Skill{Description: "no name"}, nil
			case "undescribed":
				return &skillpkg.Skill{Name: "undescribed"}, nil
			}

			return &skillpkg.Skill{Name: "good", Description: "works"}, nil
		},
	}))
	if err != nil {
		t.Fatalf("New() error = %v, want the good skill loaded", err)
	}

	skipped := tool.Skipped()
	if len(skipped) != 3 || !errors.Is(skipped[0], broken) ||
		!errors.Is(skipped[1], ErrSkillNameRequired) || !errors.Is(skipped[2], ErrSkillDescriptionRequired) {
		t.Fatalf("Skipped() = %v", skipped)
	}

	if _, err = tool.Tool().Handle(context.Background(), `{"command":"good"}`); err != nil {
		t.Fatalf("Handle(good) error = %v", err)
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
			res, nextErr := next(ctx, arguments)

			steps = append(steps, "m1-after")

			return res, nextErr
		}
	}
	m2 := func(_ *llm.Tool, next llm.ToolHandler) llm.ToolHandler {
		return func(ctx context.Context, arguments string) (any, error) {
			steps = append(steps, "m2-before")
			res, nextErr := next(ctx, arguments)

			steps = append(steps, "m2-after")

			return res, nextErr
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

// oneSkill returns a parser serving a single skill with body and allowed tools.
func oneSkill(body, allowed string) *stubParser {
	return &stubParser{
		list: func() ([]string, error) { return []string{"review"}, nil },
		parse: func(string) (*skillpkg.Skill, error) {
			return &skillpkg.Skill{Name: "review", Description: "Review code", Body: body, AllowedTools: allowed, RootPath: "/skills/review"}, nil
		},
	}
}

func TestHandleArguments(t *testing.T) {
	cases := []struct{ body, args, want string }{
		{"Review $ARGUMENTS carefully.", "main.go", "Review main.go carefully."},
		{"Review the code.", "main.go", "Review the code.\n\nARGUMENTS: main.go"},
		{"Review the code.", "", "Review the code."},
	}

	for _, tc := range cases {
		tool, err := New("", WithParser(oneSkill(tc.body, "")))
		if err != nil {
			t.Fatal(err)
		}

		out, err := tool.handle(context.Background(), &Input{Command: "review", Arguments: tc.args})
		if err != nil || out.Instructions != tc.want {
			t.Errorf("body %q, args %q: instructions = %q, %v; want %q", tc.body, tc.args, out.Instructions, err, tc.want)
		}
	}
}

func TestHandleAllowedToolsAndOnLoad(t *testing.T) {
	var loaded []string

	tool, err := New("", WithParser(oneSkill("body", "Bash(git:*), Read")), WithOnLoad(func(_ context.Context, s *skillpkg.Skill) {
		loaded = s.Tools()
	}))
	if err != nil {
		t.Fatal(err)
	}

	out, err := tool.handle(context.Background(), &Input{Command: "review"})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"Bash(git:*)", "Read"}
	if !reflect.DeepEqual(out.AllowedTools, want) || !reflect.DeepEqual(loaded, want) {
		t.Fatalf("allowed tools = %v, loaded = %v; want %v", out.AllowedTools, loaded, want)
	}

	if out.BasePath != "/skills/review" {
		t.Fatalf("base path = %q", out.BasePath)
	}
}

func TestWithRoots(t *testing.T) {
	user, project := t.TempDir(), t.TempDir()

	for root, description := range map[string]string{user: "user version", project: "project version"} {
		dir := root + "/review"
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}

		content := "---\nname: review\ndescription: " + description + "\n---\nbody"
		if err := os.WriteFile(dir+"/SKILL.md", []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	tool, err := New("", WithRoots(user, project))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(tool.Tool().Description(), "user version") || strings.Contains(tool.Tool().Description(), "project version") {
		t.Fatalf("catalog = %s, want the user's skill only", tool.Tool().Description())
	}
}

func TestWithRoots_EarlierRootWinsANameClash(t *testing.T) {
	user, project := t.TempDir(), t.TempDir()

	// The same skill name in directories of different names.
	for dir, description := range map[string]string{user + "/review": "user version", project + "/my-review": "project version"} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}

		content := "---\nname: review\ndescription: " + description + "\n---\nbody"
		if err := os.WriteFile(dir+"/SKILL.md", []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	tool, err := New("", WithRoots(user, project))
	if err != nil {
		t.Fatal(err)
	}

	if d := tool.Tool().Description(); !strings.Contains(d, "user version") || strings.Contains(d, "project version") {
		t.Fatalf("catalog = %s, want the user's skill only", d)
	}

	if len(tool.Skipped()) != 1 {
		t.Fatalf("Skipped() = %v, want the project's skill", tool.Skipped())
	}
}
