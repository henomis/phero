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

package file

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeViewRange_Omitted(t *testing.T) {
	rng, err := normalizeViewRange(&ViewRange{0, 0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rng != nil {
		t.Fatalf("expected nil range, got %+v", rng)
	}
}

func TestFormatTextWithLineNumbers_ViewRange(t *testing.T) {
	rng := &normalizedRange{start: 1, end: 3}
	out := formatTextWithLineNumbers([]byte("a\nb\nc"), rng)
	expected := "1\tb\n2\tc"
	if out != expected {
		t.Fatalf("unexpected output\nexpected: %q\n     got: %q", expected, out)
	}
}

func TestBytesToUTF8WithHexEscapes_InvalidByte(t *testing.T) {
	out := bytesToUTF8WithHexEscapes([]byte{0xff, 'a'})
	expected := "\\xffa"
	if out != expected {
		t.Fatalf("unexpected output\nexpected: %q\n     got: %q", expected, out)
	}
}

func TestFormatDirectoryListing_DepthAndSkips(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".hidden"), []byte("nope"), 0o644); err != nil {
		t.Fatalf("write hidden file: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "node_modules"), 0o755); err != nil {
		t.Fatalf("mkdir node_modules: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "node_modules", "x.txt"), []byte("nope"), 0o644); err != nil {
		t.Fatalf("write file in node_modules: %v", err)
	}

	if err := os.Mkdir(filepath.Join(dir, "dir1"), 0o755); err != nil {
		t.Fatalf("mkdir dir1: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dir1", "b.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write file in dir1: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "dir1", "subdir"), 0o755); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dir1", "subdir", "c.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write file in subdir: %v", err)
	}

	out, err := formatDirectoryListing(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := strings.Join([]string{
		"- dir1/",
		"  - subdir/",
		"  - b.txt",
		"- a.txt",
	}, "\n")

	if out != expected {
		t.Fatalf("unexpected output\nexpected:\n%s\n\n     got:\n%s", expected, out)
	}
}

func TestFormatImageMarkdownDataURI_PNG(t *testing.T) {
	imgPath := filepath.Clean(filepath.Join("..", "..", "web", "images", "phero-logo.png"))
	original, err := os.ReadFile(imgPath)
	if err != nil {
		t.Fatalf("read %s: %v", imgPath, err)
	}
	if len(original) == 0 {
		t.Fatalf("expected non-empty image at %s", imgPath)
	}

	out, err := formatImageMarkdownDataURI(imgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	prefix := "![phero-logo.png](data:image/png;base64,"
	if !strings.HasPrefix(out, prefix) {
		t.Fatalf("unexpected prefix: %q", out)
	}
	if !strings.HasSuffix(out, ")") {
		t.Fatalf("expected closing ')': %q", out)
	}

	payloadWithParen := strings.TrimPrefix(out, prefix)
	payload := strings.TrimSuffix(payloadWithParen, ")")
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("invalid base64 payload: %v", err)
	}
	if !bytes.Equal(decoded, original) {
		t.Fatalf("decoded image bytes do not match original")
	}
}

func TestView_PathTraversal_SymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	if err := os.Symlink(outsideFile, filepath.Join(dir, "escape.txt")); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	tool, err := NewViewTool(WithWorkingDirectory(dir))
	if err != nil {
		t.Fatalf("NewViewTool: %v", err)
	}

	_, err = tool.view(context.TODO(), &ViewInput{Path: "escape.txt"})
	if err == nil {
		t.Fatal("expected error for symlink escape, got nil")
	}
	if !errors.Is(err, ErrPathOutsideWorkingDirectory) {
		t.Fatalf("expected ErrPathOutsideWorkingDirectory, got: %v", err)
	}
}

func TestView_TextFile_ExceedsLimit(t *testing.T) {
	dir := t.TempDir()
	content := []byte("hello world")
	filePath := filepath.Join(dir, "big.txt")
	if err := os.WriteFile(filePath, content, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	tool, err := NewViewTool(WithWorkingDirectory(dir), WithMaxFileSize(5))
	if err != nil {
		t.Fatalf("NewViewTool: %v", err)
	}

	_, err = tool.view(context.TODO(), &ViewInput{Path: "big.txt"})
	if err == nil {
		t.Fatal("expected error for oversized text file, got nil")
	}
	if !errors.Is(err, ErrFileTooLarge) {
		t.Fatalf("expected ErrFileTooLarge, got: %v", err)
	}
}

func TestView_TextFile_AtLimit_Succeeds(t *testing.T) {
	dir := t.TempDir()
	content := []byte("hello")
	filePath := filepath.Join(dir, "exact.txt")
	if err := os.WriteFile(filePath, content, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	tool, err := NewViewTool(WithWorkingDirectory(dir), WithMaxFileSize(int64(len(content))))
	if err != nil {
		t.Fatalf("NewViewTool: %v", err)
	}

	_, err = tool.view(context.TODO(), &ViewInput{Path: "exact.txt"})
	if err != nil {
		t.Fatalf("expected no error for file at exact limit, got: %v", err)
	}
}

func TestView_TextFile_NoLimit_LargeFile_Succeeds(t *testing.T) {
	dir := t.TempDir()
	content := make([]byte, 1<<20) // 1 MiB
	filePath := filepath.Join(dir, "large.txt")
	if err := os.WriteFile(filePath, content, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// maxFileSize == 0 means no limit
	tool, err := NewViewTool(WithWorkingDirectory(dir))
	if err != nil {
		t.Fatalf("NewViewTool: %v", err)
	}

	_, err = tool.view(context.TODO(), &ViewInput{Path: "large.txt"})
	if err != nil {
		t.Fatalf("expected no error when size limit is disabled, got: %v", err)
	}
}

func TestView_Image_ExceedsLimit(t *testing.T) {
	imgPath := filepath.Clean(filepath.Join("..", "..", "web", "images", "phero-logo.png"))
	original, err := os.ReadFile(imgPath)
	if err != nil {
		t.Fatalf("read %s: %v", imgPath, err)
	}
	if len(original) == 0 {
		t.Fatalf("expected non-empty image at %s", imgPath)
	}

	dir := t.TempDir()
	dest := filepath.Join(dir, "phero-logo.png")
	if err := os.WriteFile(dest, original, 0o644); err != nil {
		t.Fatalf("write image: %v", err)
	}

	// Limit is 1 byte — guaranteed to be exceeded.
	tool, err := NewViewTool(WithWorkingDirectory(dir), WithMaxFileSize(1))
	if err != nil {
		t.Fatalf("NewViewTool: %v", err)
	}

	_, err = tool.view(context.TODO(), &ViewInput{Path: "phero-logo.png"})
	if err == nil {
		t.Fatal("expected error for oversized image, got nil")
	}
	if !errors.Is(err, ErrImageTooLarge) {
		t.Fatalf("expected ErrImageTooLarge, got: %v", err)
	}
}
