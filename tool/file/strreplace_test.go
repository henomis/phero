package file

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStrReplace_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("hello OLD world"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	tool, err := NewStrReplaceTool()
	if err != nil {
		t.Fatalf("NewStrReplaceTool: %v", err)
	}

	out, err := tool.replace(context.Background(), &StrReplaceInput{
		Description: "test",
		Path:        path,
		OldStr:      "OLD",
		NewStr:      "NEW",
	})
	if err != nil {
		t.Fatalf("replace: %v", err)
	}
	if out == nil || out.Replaced != true {
		t.Fatalf("unexpected result: %+v", out)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "hello NEW world" {
		t.Fatalf("unexpected content: %q", string(got))
	}
}

func TestStrReplace_NilInput(t *testing.T) {
	tool, err := NewStrReplaceTool()
	if err != nil {
		t.Fatalf("NewStrReplaceTool: %v", err)
	}

	_, err = tool.replace(context.Background(), nil)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestStrReplace_NotFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("hello world"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	tool, err := NewStrReplaceTool()
	if err != nil {
		t.Fatalf("NewStrReplaceTool: %v", err)
	}

	_, err = tool.replace(context.Background(), &StrReplaceInput{
		Path:   path,
		OldStr: "MISSING",
		NewStr: "X",
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStrReplace_MultipleMatches(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("X OLD Y OLD Z"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	tool, err := NewStrReplaceTool()
	if err != nil {
		t.Fatalf("NewStrReplaceTool: %v", err)
	}

	_, err = tool.replace(context.Background(), &StrReplaceInput{
		Path:   path,
		OldStr: "OLD",
		NewStr: "NEW",
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "found") || !strings.Contains(err.Error(), "times") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStrReplace_OverlappingMatches_CountsAsMultiple(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("aaa"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	tool, err := NewStrReplaceTool()
	if err != nil {
		t.Fatalf("NewStrReplaceTool: %v", err)
	}

	_, err = tool.replace(context.Background(), &StrReplaceInput{
		Path:   path,
		OldStr: "aa",
		NewStr: "X",
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "found") || !strings.Contains(err.Error(), "times") {
		t.Fatalf("unexpected error: %v", err)
	}
}
