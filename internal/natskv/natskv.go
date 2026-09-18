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

// Package natskv holds the JetStream key-value plumbing shared by the packages
// that store things in one: memory/nats and tool/kv.
package natskv

import (
	"errors"
	"fmt"

	"github.com/nats-io/nats.go"
)

// OpenBucket returns the JetStream key-value bucket named bucket on nc,
// creating it if it does not exist.
//
// The create-then-bind shape is not a convenience. CreateKeyValue fails with
// ErrStreamNameAlreadyInUse when the bucket is already there, which is the
// normal case for every process after the first and a *race* for two starting
// at once — so treating it as an error means a second replica cannot start.
// Getting that right is not something each caller should rediscover.
func OpenBucket(nc *nats.Conn, bucket string) (nats.KeyValue, error) {
	if nc == nil {
		return nil, ErrNilConn
	}

	if bucket == "" {
		return nil, ErrEmptyBucket
	}

	js, err := nc.JetStream()
	if err != nil {
		return nil, fmt.Errorf("natskv: jetstream: %w", err)
	}

	kv, err := js.CreateKeyValue(&nats.KeyValueConfig{Bucket: bucket})
	if err == nil {
		return kv, nil
	}

	if !errors.Is(err, nats.ErrStreamNameAlreadyInUse) {
		return nil, fmt.Errorf("natskv: create bucket %q: %w", bucket, err)
	}

	kv, err = js.KeyValue(bucket)
	if err != nil {
		return nil, fmt.Errorf("natskv: bind bucket %q: %w", bucket, err)
	}

	return kv, nil
}
