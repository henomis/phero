package file

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
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
	Confirmation string `json:"confirmation"`
}

// StrReplaceTool is a tool that allows replacing a unique string in a file.
type StrReplaceTool struct {
	tool       *llm.Tool
	validateFn func(context.Context, *StrReplaceInput) error
}

func NewStrReplaceTool() (*StrReplaceTool, error) {
	name := "str_replace"
	description := "Replace a unique string in a file with another string. old_str must match the raw file content exactly and appear exactly once. When copying from view output, do NOT include the line number prefix (spaces + line number + tab) — it is display-only. View the file immediately before editing; after any successful str_replace, earlier view output of that file in your context is stale — re-view before further edits to the same file."

	strReplaceTool := &StrReplaceTool{}

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

// WithValidation allows setting a custom validation function for the StrReplaceTool.
func (s *StrReplaceTool) WithValidation(validateFn func(context.Context, *StrReplaceInput) error) *StrReplaceTool {
	s.validateFn = validateFn
	return s
}

func (s *StrReplaceTool) Tool() *llm.Tool {
	return s.tool
}

func (s *StrReplaceTool) replace(ctx context.Context, input *StrReplaceInput) (*StrReplaceOutput, error) {
	if s.validateFn != nil {
		if err := s.validateFn(ctx, input); err != nil {
			return nil, err
		}
	}
	if input == nil {
		return nil, errors.New("nil input")
	}
	if strings.TrimSpace(input.Path) == "" {
		return nil, errors.New("path is required")
	}
	if input.OldStr == "" {
		return nil, errors.New("old_str is required")
	}

	content, err := os.ReadFile(input.Path)
	if err != nil {
		return nil, err
	}

	count := bytes.Count(content, []byte(input.OldStr))
	switch count {
	case 0:
		return nil, fmt.Errorf("old_str not found in %s", input.Path)
	case 1:
		// ok
	default:
		return nil, fmt.Errorf("old_str found %d times in %s", count, input.Path)
	}

	replaced := bytes.Replace(content, []byte(input.OldStr), []byte(input.NewStr), 1)
	if err := os.WriteFile(input.Path, replaced, 0o644); err != nil {
		return nil, err
	}

	confirmation := fmt.Sprintf("replaced 1 occurrence in %s", input.Path)
	if strings.TrimSpace(input.Description) != "" {
		confirmation = fmt.Sprintf("%s (%s)", confirmation, strings.TrimSpace(input.Description))
	}
	return &StrReplaceOutput{Confirmation: confirmation}, nil
}
