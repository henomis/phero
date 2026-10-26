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
	"unicode/utf8"

	"github.com/henomis/phero/v2/llm"
)

const (
	grepModeContent          = "content"
	grepModeFilesWithMatches = "files_with_matches"
	grepModeCount            = "count"
)

// GrepInput is the input schema for the grep tool.
type GrepInput struct {
	Pattern    string `json:"pattern" jsonschema:"description=The regular expression pattern to search for in file contents"`                                                                                                //nolint:lll
	Path       string `json:"path,omitempty" jsonschema:"description=File or directory to search in. Defaults to current working directory"`                                                                                 //nolint:lll
	OutputMode string `json:"output_mode,omitempty" jsonschema:"enum=content,enum=files_with_matches,enum=count" jsonschema_description:"Output mode: content, files_with_matches or count. Defaults to files_with_matches"` //nolint:lll
	Glob       string `json:"glob,omitempty" jsonschema:"description=Glob pattern to filter files"`
	Type       string `json:"type,omitempty" jsonschema_description:"File type to search (go, js, py, etc.)"`
	I          bool   `json:"-i,omitempty" jsonschema:"description=Case insensitive search"`
	N          bool   `json:"-n,omitempty" jsonschema:"description=Show line numbers in output (content mode only)"`
	A          int    `json:"-A,omitempty" jsonschema:"description=Lines after each match (content mode only)"`
	B          int    `json:"-B,omitempty" jsonschema:"description=Lines before each match (content mode only)"`
	C          int    `json:"-C,omitempty" jsonschema:"description=Lines before and after each match (content mode only)"`
	Multiline  bool   `json:"multiline,omitempty" jsonschema:"description=Enable multiline matching"`
	HeadLimit  int    `json:"head_limit,omitempty" jsonschema:"description=Limit output to first N lines or entries. It can only lower the tool's own limit"` //nolint:lll
}

// GrepOutput is the output schema for the grep tool.
type GrepOutput struct {
	Output []string `json:"output"`
	// Truncated reports that the output was cut at head_limit, at the tool's
	// maximum number of results, or at its output cap.
	Truncated bool `json:"truncated,omitempty"`
}

// GrepTool searches file contents using regular expressions.
type GrepTool struct {
	tool       *llm.Tool
	workingDir string
	maxResults int
	maxOutput  int
	noIgnore   bool
	readRoots  []string
}

// NewGrepTool creates a new grep tool.
func NewGrepTool(opts ...Option) (*GrepTool, error) {
	o := applyOptions(opts...)

	g := &GrepTool{
		workingDir: o.workingDir, maxResults: o.maxResults, maxOutput: o.maxOutput, noIgnore: o.noIgnore,
		readRoots: o.readRoots,
	}

	tool, err := llm.NewTool(
		"grep",
		"Search file contents using regular expressions. Binary files, files ignored by .gitignore, "+
			"and .git and node_modules, are skipped.",
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

//nolint:gocognit,gocyclo,funlen
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

	root, err := resolveSearchRootIn(g.workingDir, g.readRoots, input.Path)
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

	err = walkSearch(root, g.workingDir, g.readRoots, g.noIgnore, func(path string, _ fs.DirEntry) error {
		if !matchType(path, input.Type) {
			return nil
		}

		if globMatcher != nil {
			rel, relErr := normalizeRelativePath(root, path)
			if relErr != nil {
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

	// head_limit only lowers the tool's limit: the model must not raise it.
	limit := g.maxResults
	if input.HeadLimit > 0 && (limit <= 0 || input.HeadLimit < limit) {
		limit = input.HeadLimit
	}

	truncated := false

	out := make([]string, 0)

	for _, path := range files {
		contentBytes, readErr := os.ReadFile(path) //nolint:gosec // path comes from glob results within the working dir
		if readErr != nil || isBinary(contentBytes) {
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

		if limit > 0 && len(out) > limit {
			out, truncated = out[:limit], true
			break
		}
	}

	out, cut := capEntries(out, g.maxOutput)

	return &GrepOutput{Output: out, Truncated: truncated || cut}, nil
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

//nolint:gocognit
func grepContentMatches(
	path, content string, re *regexp.Regexp, multiline, lineNumbers bool, after, before int,
) []string {
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
		line := clipLine(lines[idx])
		if lineNumbers {
			out = append(out, fmt.Sprintf("%s:%d:%s", path, idx+1, line))
		} else {
			out = append(out, fmt.Sprintf("%s:%s", path, line))
		}
	}

	return out
}

// maxLineRunes caps the length of a line grep returns: a minified file holds
// its whole content on one line.
const maxLineRunes = 500

// clipLine cuts line to maxLineRunes runes, marking the cut.
func clipLine(line string) string {
	runes := []rune(line)
	if len(runes) <= maxLineRunes {
		return line
	}

	return string(runes[:maxLineRunes]) + " [...]"
}

// capEntries keeps the entries that fit in maxChars bytes, counting one
// separator after each, and reports whether it dropped any. An entry that
// does not fit is dropped whole, unless it is the first: that one is cut, so
// a result is never empty only because its first entry is long. A maxChars
// of 0 or less keeps every entry.
func capEntries(entries []string, maxChars int) ([]string, bool) {
	if maxChars <= 0 {
		return entries, false
	}

	used := 0

	for i, entry := range entries {
		if used+len(entry)+1 <= maxChars {
			used += len(entry) + 1
			continue
		}

		if i == 0 {
			return []string{cutBytes(entry, maxChars)}, true
		}

		return entries[:i], true
	}

	return entries, false
}

// cutBytes cuts s to at most n bytes without splitting a UTF-8 character.
func cutBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}

	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}

	return s[:n]
}
