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
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// DefaultMaxResults is how many paths glob, and entries grep, return at most
// when WithMaxResults is not given, so one search cannot fill the model's
// context.
const DefaultMaxResults = 1000

// binarySniffLen is how many leading bytes are checked for a NUL byte to tell
// a binary file from a text file, as git does.
const binarySniffLen = 8000

// gitDir is git's data directory, never searched.
const gitDir = ".git"

// skippedDirs are not searched unless WithNoIgnore: version control data and
// dependencies.
var skippedDirs = map[string]bool{gitDir: true, ".hg": true, ".svn": true, "node_modules": true}

// ignoreRule is one pattern of a .gitignore file.
type ignoreRule struct {
	re      *regexp.Regexp
	negate  bool
	dirOnly bool
}

// ignorer applies the .gitignore files of a search, from the repository root
// (the nearest ancestor holding .git) down to each directory searched.
type ignorer struct {
	rules map[string][]ignoreRule // by the directory holding the .gitignore
	top   string
}

// newIgnorer loads the .gitignore files from the repository root down to root.
func newIgnorer(root string) *ignorer {
	ig := &ignorer{rules: map[string][]ignoreRule{}, top: root}

	for dir := root; ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, gitDir)); err == nil {
			ig.top = dir
			break
		}

		if filepath.Dir(dir) == dir {
			break
		}
	}

	for dir := root; ; dir = filepath.Dir(dir) {
		ig.load(dir)

		if dir == ig.top || filepath.Dir(dir) == dir {
			break
		}
	}

	return ig
}

// load reads the .gitignore of dir, once. A nil ignorer ignores nothing.
func (ig *ignorer) load(dir string) {
	if ig == nil {
		return
	}

	if _, done := ig.rules[dir]; done {
		return
	}

	ig.rules[dir] = parseGitignore(filepath.Join(dir, ".gitignore"))
}

// ignored reports whether path is ignored: the last matching rule of the
// .gitignore files above it decides, as in git.
func (ig *ignorer) ignored(path string, isDir bool) bool {
	if ig == nil {
		return false
	}

	var chain []string

	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		chain = append(chain, dir)

		if dir == ig.top || filepath.Dir(dir) == dir {
			break
		}
	}

	ignored := false

	for i := len(chain) - 1; i >= 0; i-- {
		rel, err := filepath.Rel(chain[i], path)
		if err != nil {
			continue
		}

		rel = filepath.ToSlash(rel)

		for _, rule := range ig.rules[chain[i]] {
			if (!rule.dirOnly || isDir) && rule.re.MatchString(rel) {
				ignored = !rule.negate
			}
		}
	}

	return ignored
}

// parseGitignore reads the rules of a .gitignore file; a missing file has
// none. A pattern with no slash but a trailing one matches at any depth; one
// with a slash is anchored to the file's directory.
func parseGitignore(path string) []ignoreRule {
	data, err := os.ReadFile(path) //nolint:gosec // a .gitignore inside the searched tree
	if err != nil {
		return nil
	}

	var rules []ignoreRule

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), " \t")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		var rule ignoreRule

		if strings.HasPrefix(line, "!") {
			rule.negate, line = true, line[1:]
		}

		line = strings.TrimPrefix(line, `\`)

		if strings.HasSuffix(line, "/") {
			rule.dirOnly, line = true, strings.TrimSuffix(line, "/")
		}

		if strings.Contains(line, "/") {
			line = strings.TrimPrefix(line, "/")
		} else {
			line = "**/" + line
		}

		if rule.re, err = compileGlobMatcher(line); err == nil {
			rules = append(rules, rule)
		}
	}

	return rules
}

// walkSearch walks the regular files under root for glob and grep. It skips
// version control and dependency directories and, unless noIgnore, what the
// .gitignore files ignore. With a working directory, a symlinked file whose
// target is outside it, and outside readRoots, is skipped.
func walkSearch(
	root, workingDir string, readRoots []string, noIgnore bool, fn func(path string, d fs.DirEntry) error,
) error {
	w := &searchWalk{root: root, workingDir: workingDir, readRoots: readRoots, noIgnore: noIgnore}
	if !noIgnore {
		w.ig = newIgnorer(root)
	}

	return filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		switch {
		case walkErr != nil:
			return nil
		case d.IsDir():
			return w.enterDir(path, d)
		case w.skipFile(path, d):
			return nil
		default:
			return fn(path, d)
		}
	})
}

// searchWalk is the state of one walkSearch.
type searchWalk struct {
	root, workingDir string
	readRoots        []string
	noIgnore         bool
	ig               *ignorer
}

// enterDir returns filepath.SkipDir for a directory not to search, and loads
// the .gitignore of the others.
func (w *searchWalk) enterDir(path string, d fs.DirEntry) error {
	if path != w.root {
		if d.Name() == gitDir || (!w.noIgnore && skippedDirs[d.Name()]) || w.ig.ignored(path, true) {
			return filepath.SkipDir
		}
	}

	w.ig.load(path)

	return nil
}

// skipFile reports whether a file is ignored, or is a symlink out of the
// working directory.
func (w *searchWalk) skipFile(path string, d fs.DirEntry) bool {
	if w.ig.ignored(path, false) {
		return true
	}

	if d.Type()&fs.ModeSymlink != 0 && w.workingDir != "" {
		if _, err := resolveReadPath(w.workingDir, w.readRoots, path); err != nil {
			return true
		}
	}

	return false
}

// isBinary reports whether content looks binary: a NUL byte near its start.
func isBinary(content []byte) bool {
	return bytes.IndexByte(content[:min(len(content), binarySniffLen)], 0) >= 0
}
