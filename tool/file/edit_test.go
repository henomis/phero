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
	"os"
	"path/filepath"
	"testing"
)

func TestEditTool_ReplaceAll(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "multi.txt")
	if err := os.WriteFile(path, []byte("x x x"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	tool, err := NewEditTool(WithWorkingDirectory(tmp))
	if err != nil {
		t.Fatalf("new edit tool: %v", err)
	}

	out, err := tool.edit(context.Background(), &EditInput{FilePath: path, OldString: "x", NewString: "y", ReplaceAll: true})
	if err != nil {
		t.Fatalf("replace_all failed: %v", err)
	}
	if out.Replacements != 3 {
		t.Fatalf("expected 3 replacements, got %d", out.Replacements)
	}
}
