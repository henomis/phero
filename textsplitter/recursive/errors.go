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

package recursive

import "fmt"

// ErrReadFile is returned when the source file cannot be read.
type ErrReadFile struct {
	Source string
	Err    error
}

func (e *ErrReadFile) Error() string {
	return fmt.Sprintf("reading file %q: %v", e.Source, e.Err)
}

func (e *ErrReadFile) Unwrap() error {
	return e.Err
}
