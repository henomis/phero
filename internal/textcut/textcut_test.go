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

package textcut

import "testing"

func TestHead(t *testing.T) {
	for _, tt := range []struct {
		s    string
		n    int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello", 3, "hel"},
		{"aé", 2, "a"}, // é is two bytes: not split
		{"aé", 3, "aé"},
		{"abc", 0, ""},
	} {
		if got := Head(tt.s, tt.n); got != tt.want {
			t.Errorf("Head(%q, %d) = %q, want %q", tt.s, tt.n, got, tt.want)
		}
	}
}
