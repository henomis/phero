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

// Package textcut cuts tool output to the size caps the tools share: a cap
// counts bytes of UTF-8 text, and a cut never splits a character.
package textcut

import "unicode/utf8"

// Head returns s cut to at most n bytes, without splitting a UTF-8
// character. An n of 0 or less returns "".
func Head(s string, n int) string {
	if len(s) <= n {
		return s
	}

	if n <= 0 {
		return ""
	}

	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}

	return s[:n]
}
