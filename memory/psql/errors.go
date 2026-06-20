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

package psql

import "errors"

var (
	// ErrNilDB is returned when the database connection is nil.
	ErrNilDB = errors.New("nil db")
	// ErrEmptySessionID is returned when the session ID is empty.
	ErrEmptySessionID = errors.New("empty session id")
	// ErrEmptyTableName is returned when the table name is empty.
	ErrEmptyTableName = errors.New("empty table name")
	// ErrInvalidTableName is returned when the table name contains invalid characters.
	ErrInvalidTableName = errors.New("invalid table name")
)
