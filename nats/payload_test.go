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

package nats

import (
	"errors"
	"strings"
	"testing"
)

const oneMiB = 1 << 20

// A configured cap above what the connection will carry disables the guard it
// configures: the entry that slips past it fails later as an opaque transport
// error, at the moment an agent returns a large reply rather than at
// construction.
func TestConfiguredMaxPayloadAboveTheConnectionIsRejected(t *testing.T) {
	cfg := defaultServerConfig()
	WithMaxPayload("4MB")(cfg)

	err := reconcileMaxPayload(cfg, oneMiB)
	if !errors.Is(err, ErrInvalidMaxPayload) {
		t.Fatalf("reconcileMaxPayload = %v, want ErrInvalidMaxPayload", err)
	}

	// The message has to be actionable: both numbers and the value to lower to.
	for _, want := range []string{"4MB", "1048576"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
}

// The default is not the embedder's statement of intent, so it is tightened
// rather than rejected. This is the case that actually bites: phero's 1MB
// default equals NATS's own default max_payload, so it looks reconciled and is
// not — on a server configured lower it admits envelopes the transport refuses.
func TestDefaultMaxPayloadIsTightenedToTheConnection(t *testing.T) {
	cfg := defaultServerConfig()

	if err := reconcileMaxPayload(cfg, 64*1024); err != nil {
		t.Fatalf("reconcileMaxPayload: %v", err)
	}

	got, err := parseMaxPayload(cfg.maxPayload)
	if err != nil {
		t.Fatalf("the tightened value must still parse: %v", err)
	}

	if got != 64*1024 {
		t.Errorf("advertised cap = %d, want it tightened to the connection's %d", got, 64*1024)
	}
}

func TestMaxPayloadUnderTheConnectionIsLeftAlone(t *testing.T) {
	cfg := defaultServerConfig()
	WithMaxPayload("256KB")(cfg)

	if err := reconcileMaxPayload(cfg, oneMiB); err != nil {
		t.Fatalf("reconcileMaxPayload: %v", err)
	}

	if cfg.maxPayload != "256KB" {
		t.Errorf("advertised cap = %q, want it unchanged", cfg.maxPayload)
	}
}

// A connection that has not yet learned the server's limit reports 0; there is
// nothing to reconcile against, and inventing a limit would be worse.
func TestUnknownConnectionLimitReconcilesNothing(t *testing.T) {
	cfg := defaultServerConfig()

	if err := reconcileMaxPayload(cfg, 0); err != nil {
		t.Fatalf("reconcileMaxPayload: %v", err)
	}

	if cfg.maxPayload != defaultMaxPayload {
		t.Errorf("advertised cap = %q, want the default untouched", cfg.maxPayload)
	}
}

// An unparseable value is worse than a wrong one: every client's parse fails
// too, leaving MaxPayloadBytes zero — which disables the client-side guard
// entirely rather than setting it badly.
func TestUnparseableMaxPayloadIsRejected(t *testing.T) {
	for _, bad := range []string{"1 megabyte", "", "MB", "-1MB", "0MB"} {
		cfg := defaultServerConfig()
		WithMaxPayload(bad)(cfg)

		if err := reconcileMaxPayload(cfg, oneMiB); !errors.Is(err, ErrInvalidMaxPayload) {
			t.Errorf("reconcileMaxPayload(%q) = %v, want ErrInvalidMaxPayload", bad, err)
		}
	}
}

func TestPermanentClassifiesPheroErrors(t *testing.T) {
	cases := []struct {
		err       error
		permanent bool
	}{
		{ErrEmptyPrompt, true},
		{ErrPayloadTooLarge, true},
		{ErrAttachmentsNotAllowed, true},
		{ErrMalformedEnvelope, true},
		{ErrInvalidSubjectToken, true},
		{ErrInvalidMaxPayload, true},
		{ErrNilConn, true},
		{ErrEmptyOwner, true},
		{ErrServerStopped, true},
		{&ServiceError{Code: 400, Description: "bad request"}, true},
		{&ServiceError{Code: 404, Description: "not found"}, true},

		{nil, false},
		{ErrNoAgentsFound, false},   // nobody answered *now*
		{ErrStreamTimeout, false},   // the stream went quiet, not the agent
		{ErrDrainIncomplete, false}, // not a prompt fault at all
		{&ServiceError{Code: 500, Description: "boom"}, false},
		{&ServiceError{Code: 503, Description: "draining"}, false},
		{errors.New("some transport blip"), false},
	}

	for _, tc := range cases {
		if got := Permanent(tc.err); got != tc.permanent {
			t.Errorf("Permanent(%v) = %v, want %v", tc.err, got, tc.permanent)
		}
	}
}

// The classification must survive wrapping, since every layer between the fault
// and the caller adds context to it.
func TestPermanentSeesThroughWrapping(t *testing.T) {
	wrapped := errors.Join(errors.New("prompt agent \"writer\""), ErrPayloadTooLarge)
	if !Permanent(wrapped) {
		t.Error("Permanent must see a permanent fault through wrapping")
	}
}
