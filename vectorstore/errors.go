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

package vectorstore

import "errors"

// ErrEmptyPoints is returned when an upsert operation receives no points.
var ErrEmptyPoints = errors.New("empty points")

// ErrEmptyIDs is returned when a delete operation receives no IDs.
var ErrEmptyIDs = errors.New("empty ids")

// ErrEmptyQuery is returned when a query operation receives an empty query vector.
var ErrEmptyQuery = errors.New("empty query")
