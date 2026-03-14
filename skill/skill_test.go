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

package skill_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/henomis/phero/skill"
)

func TestParse_ValidSkill(t *testing.T) {
	skillMD := `---
name: pdf-processing
description: Extract text and tables from PDF files, fill forms, merge documents.
license: Apache-2.0
compatibility: Linux, macOS
metadata:
  author: example-org
  version: "1.0"
allowed-tools: pdftk pdftotext
---
BODY`

	s, err := skill.Parse(strings.NewReader(skillMD))
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if s.Name != "pdf-processing" {
		t.Fatalf("Name: expected %q, got %q", "pdf-processing", s.Name)
	}
	if s.Description == "" {
		t.Fatalf("Description: expected non-empty")
	}
	if s.License != "Apache-2.0" {
		t.Fatalf("License: expected %q, got %q", "Apache-2.0", s.License)
	}
	if s.Compatibility != "Linux, macOS" {
		t.Fatalf("Compatibility: expected %q, got %q", "Linux, macOS", s.Compatibility)
	}
	if s.AllowedTools != "pdftk pdftotext" {
		t.Fatalf("AllowedTools: expected %q, got %q", "pdftk pdftotext", s.AllowedTools)
	}
	if s.Metadata == nil || s.Metadata["author"] != "example-org" {
		t.Fatalf("Metadata.author: expected %q, got %#v", "example-org", s.Metadata)
	}
	if strings.TrimSpace(s.Body) != "BODY" {
		t.Fatalf("Body: expected %q, got %q", "BODY", s.Body)
	}
}

func TestParse_MissingFrontmatter(t *testing.T) {
	_, err := skill.Parse(strings.NewReader("name: x\ndescription: y\n"))
	if !errors.Is(err, skill.ErrMissingYAMLFrontmatter) {
		t.Fatalf("expected ErrMissingYAMLFrontmatter, got: %v", err)
	}
}

func TestParse_InvalidFormat(t *testing.T) {
	_, err := skill.Parse(strings.NewReader("---\nname: x\n"))
	if !errors.Is(err, skill.ErrInvalidSkillFormat) {
		t.Fatalf("expected ErrInvalidSkillFormat, got: %v", err)
	}
}

func TestParser_List_OnlyDirectoriesWithSkillFile(t *testing.T) {
	root := t.TempDir()

	// valid skills
	if err := os.Mkdir(filepath.Join(root, "pdf-processing"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "pdf-processing", "SKILL.md"), []byte("---\nname: pdf-processing\ndescription: x\n---\nBODY"), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	if err := os.Mkdir(filepath.Join(root, "image-tools"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "image-tools", "SKILL.md"), []byte("---\nname: image-tools\ndescription: x\n---\nBODY"), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	// invalid: directory without SKILL.md
	if err := os.Mkdir(filepath.Join(root, "no-skill-file"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// invalid: file at root
	if err := os.WriteFile(filepath.Join(root, "not-a-dir"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write root file: %v", err)
	}

	p := skill.New(root)
	got, err := p.List()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// os.ReadDir returns entries sorted by filename.
	want := []string{"image-tools", "pdf-processing"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("List() = %#v, want %#v", got, want)
	}
}

func TestParser_Parse_ReadsSkillFile(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "pdf-processing"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(root, "pdf-processing", "SKILL.md"),
		[]byte("---\nname: pdf-processing\ndescription: hello\n---\nBODY"),
		0o644,
	); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	p := skill.New(root)
	s, err := p.Parse("pdf-processing")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if s.Name != "pdf-processing" {
		t.Fatalf("Name: expected %q, got %q", "pdf-processing", s.Name)
	}
	if s.Description != "hello" {
		t.Fatalf("Description: expected %q, got %q", "hello", s.Description)
	}
	if strings.TrimSpace(s.Body) != "BODY" {
		t.Fatalf("Body: expected %q, got %q", "BODY", s.Body)
	}
}

func TestParser_Parse_MissingSkillFile(t *testing.T) {
	root := t.TempDir()
	p := skill.New(root)
	_, err := p.Parse("does-not-exist")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
}
