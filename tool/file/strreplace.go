package file

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/henomis/phero/llm"
)

// StrReplaceInput represents the input for the StrReplaceTool.
//
// It replaces a unique string in a file with another string.
// old_str must match the raw file content exactly and appear exactly once.
// When copying from view output, do NOT include the line number prefix (spaces + line number + tab).
// View the file immediately before editing; after any successful str_replace, earlier view output of that file is stale.
type StrReplaceInput struct {
	Description string `json:"description" jsonschema:"description=Why you're making this edit."`
	Path        string `json:"path" jsonschema:"description=File to edit."`
	OldStr      string `json:"old_str" jsonschema:"description=Unique string to find (must match exactly once)."`
	NewStr      string `json:"new_str" jsonschema:"description=Replacement (empty string = delete)."`
}

// StrReplaceOutput represents the output from running the StrReplaceTool.
type StrReplaceOutput struct {
	Replaced bool `json:"replaced"`
}

// StrReplaceTool is a tool that allows replacing a unique string in a file.
type StrReplaceTool struct {
	tool       *llm.Tool
	workingDir string
}

func NewStrReplaceTool(opts ...Option) (*StrReplaceTool, error) {
	name := "str_replace"
	description := "Replace a unique string in a file with another string. old_str must match the raw file content exactly and appear exactly once. When copying from view output, do NOT include the line number prefix (spaces + line number + tab) — it is display-only. View the file immediately before editing; after any successful str_replace, earlier view output of that file in your context is stale — re-view before further edits to the same file."

	o := &toolOptions{}
	for _, opt := range opts {
		opt(o)
	}

	strReplaceTool := &StrReplaceTool{workingDir: o.workingDir}

	tool, err := llm.NewTool(
		name,
		description,
		strReplaceTool.replace,
	)
	if err != nil {
		return nil, err
	}

	strReplaceTool.tool = tool
	return strReplaceTool, nil
}
func (s *StrReplaceTool) Tool() *llm.Tool {
	return s.tool
}

func (s *StrReplaceTool) replace(ctx context.Context, input *StrReplaceInput) (*StrReplaceOutput, error) {
	if input == nil {
		return nil, errors.New("nil input")
	}
	if strings.TrimSpace(input.Path) == "" {
		return nil, errors.New("path is required")
	}
	if input.OldStr == "" {
		return nil, errors.New("old_str is required")
	}

	path := input.Path
	if s.workingDir != "" && !filepath.IsAbs(path) {
		path = filepath.Join(s.workingDir, path)
	}
	if s.workingDir != "" {
		rel, relErr := filepath.Rel(filepath.Clean(s.workingDir), filepath.Clean(path))
		if relErr != nil || strings.HasPrefix(rel, "..") {
			return nil, errors.New("path is outside the working directory")
		}
	}

	fileInfo, statErr := os.Stat(path)
	if statErr != nil {
		return nil, statErr
	}
	fileMode := fileInfo.Mode().Perm()

	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	count := countOccurrencesOverlapping(content, []byte(input.OldStr))
	switch count {
	case 0:
		return nil, fmt.Errorf("old_str not found in %s", path)
	case 1:
		// ok
	default:
		return nil, fmt.Errorf("old_str found %d times in %s", count, path)
	}

	replaced := bytes.Replace(content, []byte(input.OldStr), []byte(input.NewStr), 1)
	if err := os.WriteFile(path, replaced, fileMode); err != nil {
		return nil, err
	}

	return &StrReplaceOutput{Replaced: true}, nil
}

func countOccurrencesOverlapping(haystack []byte, needle []byte) int {
	if len(needle) == 0 {
		return 0
	}

	count := 0
	for i := 0; i < len(haystack); {
		j := bytes.Index(haystack[i:], needle)
		if j < 0 {
			break
		}
		count++
		i += j + 1
	}

	return count
}
