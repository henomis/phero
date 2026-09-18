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
	"context"
	"errors"
	"fmt"

	"github.com/nats-io/nats.go"

	"github.com/henomis/phero/internal/natskv"
	"github.com/henomis/phero/llm"
)

// Store exposes a JetStream key-value bucket to an agent as a pair of tools.
//
// It is deliberately two tools rather than one with a mode argument: a model
// picking between "read" and "write" by name is far less likely to write when
// it meant to read than one picking a string parameter.
type Store struct {
	kv     nats.KeyValue
	bucket string
}

// New wraps an already-provisioned bucket.
func New(kv nats.KeyValue) (*Store, error) {
	if kv == nil {
		return nil, ErrNilKeyValue
	}

	return &Store{kv: kv, bucket: kv.Bucket()}, nil
}

// Open binds the bucket named bucket on nc, creating it if it does not exist,
// and wraps it. See [github.com/henomis/phero/memory/nats.Open] for why the
// create-or-bind belongs in the library rather than in each caller.
func Open(nc *nats.Conn, bucket string) (*Store, error) {
	kv, err := natskv.OpenBucket(nc, bucket)
	if err != nil {
		return nil, err
	}

	return New(kv)
}

type readInput struct {
	Key string `json:"key" jsonschema:"The key to read from the store."`
}

type writeInput struct {
	Key   string `json:"key" jsonschema:"The key to write."`
	Value string `json:"value" jsonschema:"The value to store."`
}

// ReadTool returns the tool that reads one key.
//
// A missing key yields an empty string rather than an error: "there is nothing
// under this key yet" is an ordinary answer for an agent checking its own
// notes, and surfacing it as a tool failure invites the model to retry or to
// give up on the whole task.
func (s *Store) ReadTool() (*llm.Tool, error) {
	return llm.NewTool("kv_read", fmt.Sprintf("Read a value from the %q key-value store.", s.bucket),
		func(_ context.Context, in readInput) (string, error) {
			entry, err := s.kv.Get(in.Key)
			if err != nil {
				if errors.Is(err, nats.ErrKeyNotFound) {
					return "", nil
				}

				return "", err
			}

			return string(entry.Value()), nil
		},
	)
}

// WriteTool returns the tool that writes one key.
func (s *Store) WriteTool() (*llm.Tool, error) {
	return llm.NewTool("kv_write", fmt.Sprintf("Write a value to the %q key-value store.", s.bucket),
		func(_ context.Context, in writeInput) (string, error) {
			if _, err := s.kv.Put(in.Key, []byte(in.Value)); err != nil {
				return "", err
			}

			return "ok", nil
		},
	)
}

// Tools returns both tools, which is what an agent normally wants.
func (s *Store) Tools() ([]*llm.Tool, error) {
	read, err := s.ReadTool()
	if err != nil {
		return nil, err
	}

	write, err := s.WriteTool()
	if err != nil {
		return nil, err
	}

	return []*llm.Tool{read, write}, nil
}
