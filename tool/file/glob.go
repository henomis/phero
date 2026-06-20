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
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/henomis/phero/llm"
)

// GlobInput is the input schema for the glob tool.
type GlobInput struct {
	Pattern string `json:"pattern" jsonschema:"description=The glob pattern to match files against"`
	Path    string `json:"path,omitempty" jsonschema:"description=The directory to search in. If omitted, the current working directory is used"`
}

// GlobOutput is the output schema for the glob tool.
type GlobOutput struct {
	Paths []string `json:"paths"`
}

// GlobTool lists files matching a glob pattern.
type GlobTool struct {
	tool       *llm.Tool
	workingDir string
}

// NewGlobTool creates a new glob tool.
func NewGlobTool(opts ...Option) (*GlobTool, error) {
	o := applyOptions(opts...)

	g := &GlobTool{workingDir: o.workingDir}

	tool, err := llm.NewTool(
		"glob",
		"Fast file pattern matching. Returns paths sorted by modification time (most recent first).",
		g.glob,
	)
	if err != nil {
		return nil, err
	}

	g.tool = tool

	return g, nil
}

// Tool returns the underlying LLM tool.
func (g *GlobTool) Tool() *llm.Tool {
	return g.tool
}

type globEntry struct {
	path    string
	modTime time.Time
}

func (g *GlobTool) glob(_ context.Context, input *GlobInput) (*GlobOutput, error) {
	if input == nil {
		return nil, fmt.Errorf("nil input")
	}

	if strings.TrimSpace(input.Pattern) == "" {
		return nil, ErrPatternRequired
	}

	root, err := resolveSearchRoot(g.workingDir, input.Path)
	if err != nil {
		return nil, err
	}

	matcher, err := compileGlobMatcher(input.Pattern)
	if err != nil {
		return nil, err
	}

	entries := make([]globEntry, 0)

	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}

		if d.IsDir() {
			return nil
		}

		rel, relErr := normalizeRelativePath(root, path)
		if relErr != nil {
			return nil
		}

		if matcher.MatchString(rel) {
			info, infoErr := d.Info()
			if infoErr != nil {
				return nil
			}

			entries = append(entries, globEntry{path: path, modTime: info.ModTime()})
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].modTime.Equal(entries[j].modTime) {
			return entries[i].path < entries[j].path
		}

		return entries[i].modTime.After(entries[j].modTime)
	})

	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		paths = append(paths, entry.path)
	}

	return &GlobOutput{Paths: paths}, nil
}
