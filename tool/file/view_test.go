package file

import (
	"bytes"
	"encoding/base64"
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
