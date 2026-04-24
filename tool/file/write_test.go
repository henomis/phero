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
	"path/filepath"
	"testing"
)

func TestWriteTool_NewFileDoesNotRequireRead(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "new.txt")

	tool, err := NewWriteTool(WithWorkingDirectory(tmp))
	if err != nil {
		t.Fatalf("new write tool: %v", err)
	}

	_, err = tool.write(context.Background(), &WriteInput{FilePath: path, Content: "hello"})
	if err != nil {
		t.Fatalf("write new file failed: %v", err)
	}
}
