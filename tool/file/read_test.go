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

func TestReadTool_ReadAndTrackSession(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "notes.txt")
	if err := os.WriteFile(path, []byte("a\nb\nc"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	session := NewSession()
	tool, err := NewReadTool(WithWorkingDirectory(tmp), WithSession(session))
	if err != nil {
		t.Fatalf("new read tool: %v", err)
	}

	out, err := tool.read(context.Background(), &ReadInput{FilePath: path, Offset: 1, Limit: 1})
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}

	if !strings.Contains(out.Content, "2\tb") {
		t.Fatalf("unexpected read output: %q", out.Content)
	}
	if !session.HasRead(path) {
		t.Fatalf("expected path to be tracked as read")
	}
}

func TestReadTool_RequiresAbsolutePath(t *testing.T) {
	tool, err := NewReadTool()
	if err != nil {
		t.Fatalf("new read tool: %v", err)
	}

	_, err = tool.read(context.Background(), &ReadInput{FilePath: "relative.txt"})
	if !errors.Is(err, ErrPathMustBeAbsolute) {
		t.Fatalf("expected ErrPathMustBeAbsolute, got %v", err)
	}
}
