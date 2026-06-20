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
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const dotDot = ".."

func resolveSearchRoot(workingDir, inputPath string) (string, error) {
	if strings.TrimSpace(inputPath) == "" {
		if strings.TrimSpace(workingDir) != "" {
			return filepath.Abs(filepath.Clean(workingDir))
		}

		return os.Getwd()
	}

	candidate := filepath.Clean(inputPath)
	if !filepath.IsAbs(candidate) {
		base := workingDir
		if strings.TrimSpace(base) == "" {
			wd, err := os.Getwd()
			if err != nil {
				return "", err
			}

			base = wd
		}

		candidate = filepath.Join(base, candidate)
	}

	absCandidate, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}

	if strings.TrimSpace(workingDir) == "" {
		return absCandidate, nil
	}

	resolvedWorkingDir, err := filepath.Abs(filepath.Clean(workingDir))
	if err != nil {
		return "", err
	}

	resolvedWorkingDir, err = filepath.EvalSymlinks(resolvedWorkingDir)
	if err != nil {
		return "", err
	}

	resolvedCandidate, err := resolvePathWithSymlinks(absCandidate)
	if err != nil {
		return "", err
	}

	rel, err := filepath.Rel(resolvedWorkingDir, resolvedCandidate)
	if err != nil {
		return "", err
	}

	if rel == dotDot || strings.HasPrefix(rel, dotDot+string(os.PathSeparator)) {
		return "", ErrPathOutsideWorkingDirectory
	}

	return resolvedCandidate, nil
}

//nolint:gocognit,funlen
func compileGlobMatcher(pattern string) (*regexp.Regexp, error) {
	p := filepath.ToSlash(strings.TrimSpace(pattern))
	if p == "" {
		return nil, ErrPatternRequired
	}

	var b strings.Builder
	b.WriteString("^")

	for i := 0; i < len(p); i++ {
		ch := p[i]

		if ch == '*' {
			if i+1 < len(p) && p[i+1] == '*' {
				if i+2 < len(p) && p[i+2] == '/' {
					b.WriteString("(?:.*/)?")

					i += 2
				} else {
					b.WriteString(".*")

					i++
				}
			} else {
				b.WriteString("[^/]*")
			}

			continue
		}

		switch ch {
		case '?':
			b.WriteString("[^/]")
		case '.':
			b.WriteString("\\.")
		case '+', '(', ')', '^', '$', '|':
			b.WriteByte('\\')
			b.WriteByte(ch)
		case '{':
			closeIdx := strings.IndexByte(p[i+1:], '}')
			if closeIdx < 0 {
				b.WriteString("\\{")
				continue
			}

			inner := p[i+1 : i+1+closeIdx]
			parts := strings.Split(inner, ",")

			b.WriteString("(?:")

			for pi, part := range parts {
				if pi > 0 {
					b.WriteString("|")
				}

				b.WriteString(regexp.QuoteMeta(part))
			}

			b.WriteString(")")

			i += closeIdx + 1
		case '[':
			closeIdx := strings.IndexByte(p[i+1:], ']')
			if closeIdx < 0 {
				b.WriteString("\\[")
				continue
			}

			cls := p[i+1 : i+1+closeIdx]
			if strings.HasPrefix(cls, "!") {
				cls = "^" + cls[1:]
			}

			b.WriteString("[")
			b.WriteString(cls)
			b.WriteString("]")

			i += closeIdx + 1
		case '/':
			b.WriteString("/")
		default:
			b.WriteString(regexp.QuoteMeta(string(ch)))
		}
	}

	b.WriteString("$")

	re, err := regexp.Compile(b.String())
	if err != nil {
		return nil, fmt.Errorf("compile glob regex: %w", err)
	}

	return re, nil
}

func normalizeRelativePath(root, fullPath string) (string, error) {
	rel, err := filepath.Rel(root, fullPath)
	if err != nil {
		return "", err
	}

	return filepath.ToSlash(rel), nil
}
