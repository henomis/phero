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

package sqlutil

import (
	"errors"
	"regexp"
	"strings"
)

// ErrInvalidIdent is returned by QuoteQualifiedIdent when name is empty,
// contains more than one dot separator, or contains characters outside the safe
// identifier character set.
var ErrInvalidIdent = errors.New("invalid SQL identifier")

// safeIdent matches a single unquoted SQL identifier component: starts with a
// letter or underscore, followed by letters, digits, or underscores.
var safeIdent = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// QuoteQualifiedIdent validates name and returns it as a safely double-quoted
// SQL identifier of the form "ident" or "schema"."table".
//
// name may be a bare identifier or a two-part schema-qualified identifier
// (e.g. "public.my_table"). Any other form, empty string, or components
// containing characters outside [a-zA-Z0-9_] return ErrInvalidIdent.
func QuoteQualifiedIdent(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", ErrInvalidIdent
	}

	parts := strings.Split(name, ".")
	if len(parts) > 2 {
		return "", ErrInvalidIdent
	}

	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if !safeIdent.MatchString(p) {
			return "", ErrInvalidIdent
		}
		out = append(out, `"`+p+`"`)
	}

	return strings.Join(out, "."), nil
}

// SafeIndexName converts a (possibly schema-qualified) table name into a flat
// string suitable for use as a SQL index name component, replacing dots with
// underscores.
//
// It assumes the input has already been validated by QuoteQualifiedIdent.
func SafeIndexName(table string) string {
	return strings.ReplaceAll(strings.TrimSpace(table), ".", "_")
}
