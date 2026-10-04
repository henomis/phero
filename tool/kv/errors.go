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

package kv

import (
	"errors"

	"github.com/henomis/phero/v2/internal/natskv"
)

var (
	// ErrNilKeyValue is returned when a nil bucket is passed to [New].
	ErrNilKeyValue = errors.New("kv: key-value bucket must not be nil")

	// ErrNilConn is returned when a nil NATS connection is passed to [Open].
	ErrNilConn = natskv.ErrNilConn

	// ErrEmptyBucket is returned when an empty bucket name is passed to [Open].
	ErrEmptyBucket = natskv.ErrEmptyBucket
)
