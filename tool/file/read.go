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
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/henomis/phero/v2/llm"
)

const (
	// defaultReadLimit is the number of lines read when no limit is given,
	// and the most a limit can ask for.
	defaultReadLimit = 2000
	maxLineChars     = 2000
)

// ReadInput is the input schema for the read tool.
type ReadInput struct {
	FilePath string `json:"file_path" jsonschema:"description=The absolute path to the file to read"`
	Offset   int    `json:"offset,omitempty" jsonschema:"description=The line number to start reading from. Only provide if the file is too large to read at once"` //nolint:lll
	Limit    int    `json:"limit,omitempty" jsonschema:"description=The number of lines to read. Only provide if the file is too large to read at once"`            //nolint:lll
}

// ReadOutput was the output schema for the read tool.
//
// Deprecated: the read tool returns plain text since v2.3.0, so the model
// gets tabs and quotes as they are in the file, not JSON-escaped.
type ReadOutput struct {
	Content string `json:"content"`
}

// ReadTool reads files with cat -n style line numbering.
type ReadTool struct {
	tool        *llm.Tool
	workingDir  string
	maxFileSize int64
	maxOutput   int
	tracker     *ReadTracker
	readRoots   []string
}

// NewReadTool creates a new read tool.
func NewReadTool(opts ...Option) (*ReadTool, error) {
	o := applyOptions(opts...)

	r := &ReadTool{
		workingDir:  o.workingDir,
		maxFileSize: o.maxFileSize,
		maxOutput:   o.maxOutput,
		tracker:     o.tracker,
		readRoots:   o.readRoots,
	}

	tool, err := llm.NewTool(
		"read",
		fmt.Sprintf("Read file contents from an absolute path with optional line offset/limit "+
			"(at most %d lines per read). Page through large files with offset and limit.", defaultReadLimit),
		r.read,
	)
	if err != nil {
		return nil, err
	}

	r.tool = tool

	return r, nil
}

// Tool returns the underlying LLM tool.
func (r *ReadTool) Tool() *llm.Tool {
	return r.tool
}

// read returns the file's lines as plain cat -n text. A string result reaches
// the model as is: a struct would be JSON-encoded, escaping every tab and
// quote, which costs tokens and can leak into an edit's old_string.
func (r *ReadTool) read(_ context.Context, input *ReadInput) (string, error) {
	if input == nil {
		return "", fmt.Errorf("nil input")
	}

	if strings.TrimSpace(input.FilePath) == "" {
		return "", ErrPathRequired
	}

	resolvedPath, err := resolveReadPath(r.workingDir, r.readRoots, input.FilePath)
	if err != nil {
		return "", err
	}

	info, err := os.Stat(resolvedPath)
	if err != nil {
		return "", err
	}

	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory", resolvedPath)
	}

	// The size cap guards memory for a whole-file read: a page of a larger
	// file is read line by line.
	whole := input.Offset <= 0 && input.Limit <= 0
	if whole && r.maxFileSize > 0 && info.Size() > r.maxFileSize {
		return "", &TooLargeError{Path: resolvedPath, Size: info.Size(), Limit: r.maxFileSize}
	}

	f, err := os.Open(resolvedPath) //nolint:gosec // path is resolved and validated above
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	content, err := r.readLines(bufio.NewReader(f), max(input.Offset, 0), readLimit(input.Limit))
	if err != nil {
		return "", err
	}

	r.tracker.record(resolvedPath)

	return content, nil
}

// readLimit returns the number of lines to read for a requested limit.
func readLimit(limit int) int {
	if limit <= 0 || limit > defaultReadLimit {
		return defaultReadLimit
	}

	return limit
}

// readLines returns limit lines of rd from line offset (0-based), numbered
// cat -n style. Output that would pass the output cap stops at the last whole
// line, with a note giving the offset to read on from.
func (r *ReadTool) readLines(rd *bufio.Reader, offset, limit int) (string, error) {
	var b strings.Builder

	for i := 0; i < offset+limit; i++ {
		line, err := readLine(rd)
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return "", err
		}

		if i < offset {
			continue
		}

		numbered := fmt.Sprintf("%6d\t%s\n", i+1, truncateRunes(bytesToUTF8WithHexEscapes(line), maxLineChars))
		if r.maxOutput > 0 && b.Len()+len(numbered) > r.maxOutput && b.Len() > 0 {
			fmt.Fprintf(&b, "[output cut at %d characters: read on with offset=%d]", r.maxOutput, i)
			return b.String(), nil
		}

		b.WriteString(numbered)
	}

	return strings.TrimRight(b.String(), "\n"), nil
}

// readLine returns the next line of rd without its line ending, keeping at
// most the bytes maxLineChars characters can take, so a huge line does not
// fill memory. It returns io.EOF when no line is left.
func readLine(rd *bufio.Reader) ([]byte, error) {
	const maxLineBytes = maxLineChars * utf8.UTFMax

	var line []byte

	for {
		chunk, isPrefix, err := rd.ReadLine()
		if err != nil {
			if errors.Is(err, io.EOF) && line != nil {
				return line, nil
			}

			return nil, err
		}

		if room := maxLineBytes - len(line); room > 0 {
			line = append(line, chunk[:min(len(chunk), room)]...)
		}

		if line == nil {
			line = []byte{}
		}

		if !isPrefix {
			return line, nil
		}
	}
}

func bytesToUTF8WithHexEscapes(b []byte) string {
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

func truncateRunes(s string, limit int) string {
	if limit <= 0 {
		return ""
	}

	r := []rune(s)
	if len(r) <= limit {
		return s
	}

	return string(r[:limit])
}
