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
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/henomis/phero/llm"
)

type ViewRange struct {
	Start int `json:"start" jsonschema:"description=The starting line number (inclusive) for viewing a text file. Line numbers are 0-based."`
	End   int `json:"end" jsonschema:"description=The ending line number (exclusive) for viewing a text file. Line numbers are 0-based. Specify -1 to indicate the end of the file."`
}

// ViewInput represents the input for the ReadTool, containing the path of the file to read.
type ViewInput struct {
	Description string     `json:"description" jsonschema:"description=Why you're viewing this file."`
	Path        string     `json:"path" jsonschema:"description=The path of the file or directory to view."`
	ViewRange   *ViewRange `json:"view_range,omitempty" jsonschema:"description=Optional range of lines to view when reading text files. If not provided, the entire file will be read. Try to read at least 200 lines."`
}

// ViewOutput represents the output from running the ReadTool, containing the content of the file.
type ViewOutput struct {
	Content string `json:"content"`
}

// ViewTool is a tool that allows reading the content of a file.
type ViewTool struct {
	tool        *llm.Tool
	workingDir  string
	maxFileSize int64
}

// NewViewTool creates a new instance of ViewTool.
func NewViewTool(opts ...Option) (*ViewTool, error) {
	name := "view"
	description := "Supported path types: Directories (lists files and directories up to 2 levels deep, ignoring hidden items and node_modules), Image files (.jpg, .jpeg, .png, .gif, .webp) (displays the image visually), Text files (displays numbered lines). You can optionally specify a view_range to see specific lines. Note: Files with non-UTF-8 encoding will display hex escapes for invalid bytes."

	o := &toolOptions{}
	for _, opt := range opts {
		opt(o)
	}

	viewTool := &ViewTool{
		workingDir:  o.workingDir,
		maxFileSize: o.maxFileSize,
	}

	tool, err := llm.NewTool(
		name,
		description,
		viewTool.view,
	)
	if err != nil {
		return nil, err
	}

	viewTool.tool = tool

	return viewTool, nil
}

// Tool returns the llm.FunctionTool representation of the ViewTool.
func (r *ViewTool) Tool() *llm.Tool {
	return r.tool
}

func (r *ViewTool) view(ctx context.Context, input *ViewInput) (*ViewOutput, error) {
	if input == nil {
		return nil, errors.New("nil input")
	}
	if strings.TrimSpace(input.Path) == "" {
		return nil, errors.New("path is required")
	}

	path := input.Path
	resolvedPath, err := resolveToolPath(r.workingDir, path)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(resolvedPath)
	if err != nil {
		return nil, err
	}

	if info.IsDir() {
		content, err := formatDirectoryListing(resolvedPath)
		if err != nil {
			return nil, err
		}
		return &ViewOutput{Content: content}, nil
	}

	if isSupportedImagePath(resolvedPath) {
		if r.maxFileSize > 0 && info.Size() > r.maxFileSize {
			return nil, &ImageTooLargeError{Path: resolvedPath, Size: info.Size(), Limit: r.maxFileSize}
		}
		content, err := formatImageMarkdownDataURI(resolvedPath)
		if err != nil {
			return nil, err
		}
		return &ViewOutput{Content: content}, nil
	}

	if r.maxFileSize > 0 && info.Size() > r.maxFileSize {
		return nil, &FileTooLargeError{Path: resolvedPath, Size: info.Size(), Limit: r.maxFileSize}
	}

	contentBytes, err := os.ReadFile(resolvedPath)
	if err != nil {
		return nil, err
	}

	viewRange, err := normalizeViewRange(input.ViewRange)
	if err != nil {
		return nil, err
	}
	content := formatTextWithLineNumbers(contentBytes, viewRange)
	return &ViewOutput{Content: content}, nil
}

type normalizedRange struct {
	start int // 0-based inclusive
	end   int // 0-based exclusive; -1 means end of file
}

func normalizeViewRange(rng *ViewRange) (*normalizedRange, error) {
	if rng == nil || (rng.Start == 0 && rng.End == 0) {
		return nil, nil
	}

	start := rng.Start
	end := rng.End

	if start < 0 {
		return nil, fmt.Errorf("view_range start must be >= 0 (got %d)", start)
	}
	if end != -1 && end < 0 {
		return nil, fmt.Errorf("view_range end must be -1 or >= 0 (got %d)", end)
	}
	if end != -1 && end < start {
		return nil, fmt.Errorf("view_range end must be -1 or >= start (got start=%d end=%d)", start, end)
	}
	if end == start {
		// Empty range by definition of end-exclusive.
		return &normalizedRange{start: start, end: end}, nil
	}
	return &normalizedRange{start: start, end: end}, nil
}

func isHiddenName(name string) bool {
	return strings.HasPrefix(name, ".")
}

func shouldSkipDirEntry(name string) bool {
	if name == "" {
		return true
	}
	if name == "node_modules" {
		return true
	}
	return isHiddenName(name)
}

type entry struct {
	name  string
	isDir bool
}

func formatDirectoryListing(dirPath string) (string, error) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return "", err
	}

	level1 := make([]entry, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if shouldSkipDirEntry(name) {
			continue
		}
		level1 = append(level1, entry{name: name, isDir: e.IsDir()})
	}
	sortEntries(level1)

	var b strings.Builder
	for _, e := range level1 {
		if e.isDir {
			fmt.Fprintf(&b, "- %s/\n", e.name)

			childPath := filepath.Join(dirPath, e.name)
			children, err := os.ReadDir(childPath)
			if err != nil {
				// Keep listing robust: report errors inline rather than failing the whole tool.
				fmt.Fprintf(&b, "  - [error reading directory: %v]\n", err)
				continue
			}
			level2 := make([]entry, 0, len(children))
			for _, c := range children {
				childName := c.Name()
				if shouldSkipDirEntry(childName) {
					continue
				}
				level2 = append(level2, entry{name: childName, isDir: c.IsDir()})
			}
			sortEntries(level2)
			for _, c := range level2 {
				if c.isDir {
					fmt.Fprintf(&b, "  - %s/\n", c.name)
				} else {
					fmt.Fprintf(&b, "  - %s\n", c.name)
				}
			}
			continue
		}

		fmt.Fprintf(&b, "- %s\n", e.name)
	}

	if b.Len() == 0 {
		return "(empty)", nil
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func sortEntries(entries []entry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].isDir != entries[j].isDir {
			return entries[i].isDir // dirs first
		}
		return strings.ToLower(entries[i].name) < strings.ToLower(entries[j].name)
	})
}

func isSupportedImagePath(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp":
		return true
	default:
		return false
	}
}

func imageMimeType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}

func formatImageMarkdownDataURI(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	mime := imageMimeType(path)
	b64 := base64.StdEncoding.EncodeToString(data)
	name := filepath.Base(path)
	return fmt.Sprintf("![%s](data:%s;base64,%s)", name, mime, b64), nil
}

func formatTextWithLineNumbers(content []byte, rng *normalizedRange) string {
	decoded := bytesToUTF8WithHexEscapes(content)
	lines := strings.Split(decoded, "\n")

	start := 0
	end := len(lines) // 0-based exclusive
	if rng != nil {
		start = rng.start
		if rng.end == -1 {
			end = len(lines)
		} else {
			end = rng.end
		}
	}

	if start >= len(lines) {
		return ""
	}
	if end > len(lines) {
		end = len(lines)
	}
	if end < start {
		return ""
	}

	var b strings.Builder
	for i := start; i < end; i++ {
		fmt.Fprintf(&b, "%d\t%s\n", i, lines[i])
	}
	return strings.TrimRight(b.String(), "\n")
}

func bytesToUTF8WithHexEscapes(b []byte) string {
	// If it's already valid UTF-8, this is fast.
	if utf8.Valid(b) {
		return string(b)
	}

	var out strings.Builder
	out.Grow(len(b))
	for len(b) > 0 {
		r, size := utf8.DecodeRune(b)
		if r == utf8.RuneError && size == 1 {
			fmt.Fprintf(&out, "\\x%02x", b[0])
			b = b[1:]
			continue
		}
		out.WriteRune(r)
		b = b[size:]
	}
	return out.String()
}
