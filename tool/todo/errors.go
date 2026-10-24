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

package todo

import "errors"

// ErrEmptyContent is returned for an item with no content.
var ErrEmptyContent = errors.New("todo item content is required")

// ErrInvalidStatus is returned for an item whose status is not pending,
// in_progress or completed.
var ErrInvalidStatus = errors.New("todo item status must be pending, in_progress or completed")

// ErrMultipleInProgress is returned for a list with more than one item in
// progress.
var ErrMultipleInProgress = errors.New("only one todo item can be in progress")
