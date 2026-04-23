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
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/henomis/phero/llm"
)

const (
	grepModeContent          = "content"
	grepModeFilesWithMatches = "files_with_matches"
	grepModeCount            = "count"
)

// GrepInput is the input schema for the grep tool.
type GrepInput struct {
	Pattern    string `json:"pattern" jsonschema:"description=The regular expression pattern to search for in file contents"`
	Path       string `json:"path,omitempty" jsonschema:"description=File or directory to search in. Defaults to current working directory"`
	OutputMode string `json:"output_mode,omitempty" jsonschema:"description=Output mode: content, files_with_matches, count. Defaults to files_with_matches"`
	Glob       string `json:"glob,omitempty" jsonschema:"description=Glob pattern to filter files"`
	Type       string `json:"type,omitempty" jsonschema:"description=File type to search (go, js, py, etc.)"`
	I          bool   `json:"-i,omitempty" jsonschema:"description=Case insensitive search"`
	N          bool   `json:"-n,omitempty" jsonschema:"description=Show line numbers in output (content mode only)"`
	A          int    `json:"-A,omitempty" jsonschema:"description=Lines after each match (content mode only)"`
	B          int    `json:"-B,omitempty" jsonschema:"description=Lines before each match (content mode only)"`
	C          int    `json:"-C,omitempty" jsonschema:"description=Lines before and after each match (content mode only)"`
	Multiline  bool   `json:"multiline,omitempty" jsonschema:"description=Enable multiline matching"`
	HeadLimit  int    `json:"head_limit,omitempty" jsonschema:"description=Limit output to first N lines or entries"`
}

// GrepOutput is the output schema for the grep tool.
type GrepOutput struct {
	Output []string `json:"output"`
}

// GrepTool searches file contents using regular expressions.
type GrepTool struct {
	tool       *llm.Tool
	workingDir string
}

// NewGrepTool creates a new grep tool.
func NewGrepTool(opts ...Option) (*GrepTool, error) {
	o := applyOptions(opts...)

	g := &GrepTool{workingDir: o.workingDir}

	tool, err := llm.NewTool(
		"grep",
		"Search file contents using regular expressions.",
		g.grep,
	)
	if err != nil {
		return nil, err
	}
	g.tool = tool

	return g, nil
}

// Tool returns the underlying LLM tool.
func (g *GrepTool) Tool() *llm.Tool {
	return g.tool
}

func (g *GrepTool) grep(_ context.Context, input *GrepInput) (*GrepOutput, error) {
	if input == nil {
		return nil, fmt.Errorf("nil input")
	}
	if strings.TrimSpace(input.Pattern) == "" {
		return nil, ErrPatternRequired
	}

	mode := input.OutputMode
	if mode == "" {
		mode = grepModeFilesWithMatches
	}
	if mode != grepModeContent && mode != grepModeFilesWithMatches && mode != grepModeCount {
		return nil, ErrInvalidOutputMode
	}

	if input.C > 0 {
		input.A = input.C
		input.B = input.C
	}
	if mode != grepModeContent && (input.A > 0 || input.B > 0 || input.C > 0 || input.N) {
		return nil, ErrContextFlagsRequireContentMode
	}

	pattern := input.Pattern
	if input.I {
		pattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}

	root, err := resolveSearchRoot(g.workingDir, input.Path)
	if err != nil {
		return nil, err
	}

	var globMatcher *regexp.Regexp
	if strings.TrimSpace(input.Glob) != "" {
		globMatcher, err = compileGlobMatcher(input.Glob)
		if err != nil {
			return nil, err
		}
	}

	files := make([]string, 0)
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}

		if !matchType(path, input.Type) {
			return nil
		}

		if globMatcher != nil {
			rel, err := normalizeRelativePath(root, path)
			if err != nil {
				return nil
			}
			if !globMatcher.MatchString(rel) {
				return nil
			}
		}

		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)

	out := make([]string, 0)
	for _, path := range files {
		contentBytes, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		content := string(contentBytes)

		switch mode {
		case grepModeFilesWithMatches:
			if re.MatchString(content) {
				out = append(out, path)
			}
		case grepModeCount:
			count := countMatches(re, content, input.Multiline)
			if count > 0 {
				out = append(out, fmt.Sprintf("%s:%d", path, count))
			}
		case grepModeContent:
			matches := grepContentMatches(path, content, re, input.Multiline, input.N, input.A, input.B)
			out = append(out, matches...)
		}

		if input.HeadLimit > 0 && len(out) >= input.HeadLimit {
			out = out[:input.HeadLimit]
			return &GrepOutput{Output: out}, nil
		}
	}

	if input.HeadLimit > 0 && len(out) > input.HeadLimit {
		out = out[:input.HeadLimit]
	}

	return &GrepOutput{Output: out}, nil
}

func matchType(path, t string) bool {
	if strings.TrimSpace(t) == "" {
		return true
	}

	ext := strings.ToLower(filepath.Ext(path))
	typeToExt := map[string]string{
		"go":   ".go",
		"js":   ".js",
		"ts":   ".ts",
		"tsx":  ".tsx",
		"jsx":  ".jsx",
		"py":   ".py",
		"java": ".java",
		"rb":   ".rb",
		"rs":   ".rs",
		"md":   ".md",
		"json": ".json",
		"yaml": ".yaml",
		"yml":  ".yml",
	}

	wantExt, ok := typeToExt[strings.ToLower(t)]
	if !ok {
		return true
	}
	return ext == wantExt
}

func countMatches(re *regexp.Regexp, content string, multiline bool) int {
	if multiline {
		return len(re.FindAllStringIndex(content, -1))
	}

	count := 0
	for _, line := range strings.Split(content, "\n") {
		if re.MatchString(line) {
			count++
		}
	}
	return count
}

func grepContentMatches(path, content string, re *regexp.Regexp, multiline, lineNumbers bool, after, before int) []string {
	if multiline {
		idxs := re.FindAllStringIndex(content, -1)
		out := make([]string, 0, len(idxs))
		for _, idx := range idxs {
			match := content[idx[0]:idx[1]]
			out = append(out, fmt.Sprintf("%s:%s", path, match))
		}
		return out
	}

	lines := strings.Split(content, "\n")
	matched := make(map[int]struct{})
	for i, line := range lines {
		if re.MatchString(line) {
			start := i - before
			if start < 0 {
				start = 0
			}
			end := i + after
			if end >= len(lines) {
				end = len(lines) - 1
			}
			for j := start; j <= end; j++ {
				matched[j] = struct{}{}
			}
		}
	}

	if len(matched) == 0 {
		return nil
	}

	indexes := make([]int, 0, len(matched))
	for idx := range matched {
		indexes = append(indexes, idx)
	}
	sort.Ints(indexes)

	out := make([]string, 0, len(indexes))
	for _, idx := range indexes {
		if lineNumbers {
			out = append(out, fmt.Sprintf("%s:%d:%s", path, idx+1, lines[idx]))
		} else {
			out = append(out, fmt.Sprintf("%s:%s", path, lines[idx]))
		}
	}

	return out
}
