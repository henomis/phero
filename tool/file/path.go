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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func resolveToolPath(workingDir, inputPath string) (string, error) {
	path := filepath.Clean(inputPath)
	if workingDir == "" {
		return path, nil
	}

	resolvedWorkingDir, err := filepath.Abs(filepath.Clean(workingDir))
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}
	resolvedWorkingDir, err = filepath.EvalSymlinks(resolvedWorkingDir)
	if err != nil {
		return "", fmt.Errorf("resolve working directory symlinks: %w", err)
	}

	if !filepath.IsAbs(path) {
		path = filepath.Join(resolvedWorkingDir, path)
	}
	path, err = filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}

	resolvedPath, err := resolvePathWithSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("resolve path symlinks: %w", err)
	}

	rel, err := filepath.Rel(resolvedWorkingDir, resolvedPath)
	if err != nil {
		return "", fmt.Errorf("compare path with working directory: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", ErrPathOutsideWorkingDirectory
	}

	return resolvedPath, nil
}

func resolvePathWithSymlinks(path string) (string, error) {
	current := filepath.Clean(path)
	missingParts := make([]string, 0)

	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			for i := len(missingParts) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missingParts[i])
			}
			return filepath.Clean(resolved), nil
		}

		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}

		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}

		missingParts = append(missingParts, filepath.Base(current))
		current = parent
	}
}
