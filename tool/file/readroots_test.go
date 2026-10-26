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
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadOnlyRoots(t *testing.T) {
	work, skills, secret := t.TempDir(), t.TempDir(), t.TempDir()

	writeTree(t, work, map[string]string{"main.go": "package main\n"})
	writeTree(t, skills, map[string]string{"review/SKILL.md": "skill body\n", "review/run.sh": "echo review\n"})
	writeTree(t, secret, map[string]string{"key.txt": "secret\n"})

	// A symlink in the read root that leads out of every allowed directory.
	if err := os.Symlink(filepath.Join(secret, "key.txt"), filepath.Join(skills, "review", "leak.txt")); err != nil {
		t.Fatal(err)
	}

	opts := []Option{WithWorkingDirectory(work), WithReadOnlyRoots(skills)}
	ctx := context.Background()
	script := filepath.Join(skills, "review", "run.sh")

	read, _ := NewReadTool(opts...)
	if out, err := read.read(ctx, &ReadInput{FilePath: script}); err != nil || !strings.Contains(out, "echo review") {
		t.Fatalf("read in a read-only root = %q, %v", out, err)
	}

	for _, path := range []string{filepath.Join(secret, "key.txt"), filepath.Join(skills, "review", "leak.txt")} {
		if _, err := read.read(ctx, &ReadInput{FilePath: path}); !errors.Is(err, ErrPathOutsideWorkingDirectory) {
			t.Fatalf("read %s: err = %v, want ErrPathOutsideWorkingDirectory", path, err)
		}
	}

	grep, _ := NewGrepTool(opts...)

	out, err := grep.grep(ctx, &GrepInput{Pattern: "review|secret", Path: skills})
	if err != nil || strings.Join(out.Output, ",") != script {
		t.Fatalf("grep in a read-only root = %v, %v; want the script only, not the symlink out", out, err)
	}

	glob, _ := NewGlobTool(opts...)
	if globOut, globErr := glob.glob(ctx, &GlobInput{Pattern: "**/*.sh", Path: skills}); globErr != nil || len(globOut.Paths) != 1 {
		t.Fatalf("glob in a read-only root = %v, %v", globOut, globErr)
	}

	if _, err = glob.glob(ctx, &GlobInput{Pattern: "*", Path: secret}); !errors.Is(err, ErrPathOutsideWorkingDirectory) {
		t.Fatalf("glob outside: err = %v, want ErrPathOutsideWorkingDirectory", err)
	}

	// The tools that change files stay in the working directory.
	write, _ := NewWriteTool(opts...)
	if _, err = write.write(ctx, &WriteInput{FilePath: script, Content: "rm -rf ~"}); !errors.Is(err, ErrPathOutsideWorkingDirectory) {
		t.Fatalf("write in a read-only root: err = %v, want ErrPathOutsideWorkingDirectory", err)
	}

	edit, _ := NewEditTool(opts...)
	if _, err = edit.edit(ctx, &EditInput{FilePath: script, OldString: "echo", NewString: "rm"}); !errors.Is(err, ErrPathOutsideWorkingDirectory) {
		t.Fatalf("edit in a read-only root: err = %v, want ErrPathOutsideWorkingDirectory", err)
	}

	if data, _ := os.ReadFile(script); string(data) != "echo review\n" {
		t.Fatalf("script = %q, want it unchanged", data)
	}
}
