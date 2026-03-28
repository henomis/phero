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

package skill

import "testing"

func TestSkillAllowsTool(t *testing.T) {
	tests := []struct {
		name         string
		allowedTools string
		toolName     string
		want         bool
	}{
		{name: "empty allowlist allows all", allowedTools: "", toolName: toolNameBash, want: true},
		{name: "whitespace allowlist allows all", allowedTools: "   ", toolName: toolNameView, want: true},
		{name: "explicitly allowed tool", allowedTools: toolNameView + " " + toolNameCreateFile, toolName: toolNameCreateFile, want: true},
		{name: "explicitly denied tool", allowedTools: toolNameView + " " + toolNameCreateFile, toolName: toolNameBash, want: false},
		{name: "newline separated allowlist", allowedTools: toolNameView + "\n" + toolNameStrReplace, toolName: toolNameStrReplace, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Skill{AllowedTools: tt.allowedTools}
			if got := s.allowsTool(tt.toolName); got != tt.want {
				t.Fatalf("allowsTool(%q) = %v, want %v", tt.toolName, got, tt.want)
			}
		})
	}
}
