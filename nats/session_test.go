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
	"context"
	"testing"

	natsclient "github.com/nats-io/nats.go"

	"github.com/henomis/phero/v2/agent"
	"github.com/henomis/phero/v2/llm"
)

// nopHandler satisfies Handler without an LLM, so the registration rules can be
// tested without a broker or a model.
type nopHandler struct{}

func (nopHandler) Run(context.Context, ...llm.ContentPart) (*agent.Result, error) {
	return &agent.Result{}, nil
}

func TestInstanceName(t *testing.T) {
	cases := []struct {
		name, session, want string
	}{
		{"worker", "prod", "worker-prod"},
		{"worker", "", "worker"},
		{"worker-a", "b", "worker-a-b"},
	}

	for _, tc := range cases {
		if got := InstanceName(tc.name, tc.session); got != tc.want {
			t.Errorf("InstanceName(%q, %q) = %q, want %q", tc.name, tc.session, got, tc.want)
		}
	}
}

// TestWithSessionSuffixesTheNameAndKeepsTheSession is the whole of M30: the
// session suffixes the registered name, and remains the session everywhere it is
// advertised. It used to be overwritten with the composed name, so an agent
// reported session="worker-prod" — one distinct value per agent, which is no
// grouping at all, and nothing a FilterBySession could match.
func TestWithSessionSuffixesTheNameAndKeepsTheSession(t *testing.T) {
	// New only nil-checks the connection; nothing here reaches the wire.
	s, err := New(&natsclient.Conn{}, nopHandler{}, "acme", "worker", WithSession("prod"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if s.name != "worker-prod" {
		t.Errorf("registered name = %q, want %q", s.name, "worker-prod")
	}

	if s.cfg.session != "prod" {
		t.Errorf("advertised session = %q, want %q — the session must not become the instance name",
			s.cfg.session, "prod")
	}
}

func TestWithoutSessionTheNameIsUnchanged(t *testing.T) {
	// New only nil-checks the connection; nothing here reaches the wire.
	s, err := New(&natsclient.Conn{}, nopHandler{}, "acme", "worker")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if s.name != "worker" {
		t.Errorf("registered name = %q, want %q", s.name, "worker")
	}

	if s.cfg.session != "" {
		t.Errorf("advertised session = %q, want empty", s.cfg.session)
	}
}

func TestFilterBySession(t *testing.T) {
	info := &AgentInfo{Owner: "acme", Name: "worker-prod", Session: "prod"}

	cases := []struct {
		name  string
		opts  []DiscoverOption
		match bool
	}{
		{"matching session", []DiscoverOption{FilterBySession("prod")}, true},
		{"other session", []DiscoverOption{FilterBySession("staging")}, false},
		{"session with owner", []DiscoverOption{FilterByOwner("acme"), FilterBySession("prod")}, true},
		{"session with wrong owner", []DiscoverOption{FilterByOwner("other"), FilterBySession("prod")}, false},
		// The instance name is not the session: filtering on one must not match
		// the other, which is exactly what the old metadata made indistinguishable.
		{"instance name is not a session", []DiscoverOption{FilterBySession("worker-prod")}, false},
		{"no session filter", nil, true},
	}

	for _, tc := range cases {
		filter := &discoverFilter{}
		for _, opt := range tc.opts {
			opt(filter)
		}

		if got := matchFilter(info, filter); got != tc.match {
			t.Errorf("%s: matchFilter = %v, want %v", tc.name, got, tc.match)
		}
	}
}
