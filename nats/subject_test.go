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

	natsclient "github.com/nats-io/nats.go"
)

func TestValidateSubjectToken(t *testing.T) {
	cases := []struct {
		token string
		valid bool
		why   string
	}{
		{"worker", true, "a plain name"},
		{"worker-prod", true, "the composed instance name"},
		{"Worker_1", true, "mixed case, digits and underscore"},
		{strings.Repeat("a", maxSubjectToken), true, "exactly at the limit"},

		{"", false, "empty"},
		{"*", false, "a single-token wildcard"},
		{"acme.*", false, "a wildcard inside a token"},
		{">", false, "a tail wildcard"},
		{"acme.corp", false, "a dot shifts every later token"},
		{"acme corp", false, "whitespace"},
		{"acme\tcorp", false, "a control character"},
		{"acme/corp", false, "outside the name alphabet"},
		{strings.Repeat("a", maxSubjectToken+1), false, "over the limit"},
	}

	for _, tc := range cases {
		err := ValidateSubjectToken(tc.token)

		if tc.valid && err != nil {
			t.Errorf("ValidateSubjectToken(%q) = %v, want nil (%s)", tc.token, err, tc.why)
		}

		if !tc.valid {
			if err == nil {
				t.Errorf("ValidateSubjectToken(%q) = nil, want an error (%s)", tc.token, tc.why)
				continue
			}

			if !errors.Is(err, ErrInvalidSubjectToken) {
				t.Errorf("ValidateSubjectToken(%q) = %v, want it to wrap ErrInvalidSubjectToken", tc.token, err)
			}
		}
	}
}

// TestNewRejectsSubjectWildcards is the security case, and the reason this
// validation exists at all.
//
// owner and name are interpolated verbatim into agents.prompt.{agent}.{owner}.{name}
// and subscribed in the shared "agents" queue group. An owner of "*" therefore
// subscribes a *wildcard inside that queue group*, and the NATS server
// load-balances other tenants' prompts to it: the subscriber reads prompts it
// was never addressed, and the replies it sends are accepted as those agents'
// answers. New used to accept it.
func TestNewRejectsSubjectWildcards(t *testing.T) {
	cases := []struct {
		name  string
		owner string
		agent string
		opts  []ServerOption
	}{
		{"wildcard owner", "*", "worker", nil},
		{"tail-wildcard owner", ">", "worker", nil},
		{"wildcard name", "acme", "*", nil},
		{"tail-wildcard name", "acme", ">", nil},
		{"dotted owner", "acme.corp", "worker", nil},
		{"dotted name", "acme", "worker.v2", nil},
		{"wildcard agent id", "acme", "worker", []ServerOption{WithAgentID("*")}},
		{"dotted agent id", "acme", "worker", []ServerOption{WithAgentID("a.b")}},
		{"wildcard session", "acme", "worker", []ServerOption{WithSession("*")}},
		{"dotted session", "acme", "worker", []ServerOption{WithSession("a.b")}},
	}

	for _, tc := range cases {
		// New only nil-checks the connection; nothing here reaches the wire.
		_, err := New(&natsclient.Conn{}, nopHandler{}, tc.owner, tc.agent, tc.opts...)
		if !errors.Is(err, ErrInvalidSubjectToken) {
			t.Errorf("%s: New = %v, want ErrInvalidSubjectToken", tc.name, err)
		}
	}
}

// A name and a session that are each legal can still overflow once joined, and
// it is the joined value that reaches the wire.
func TestNewRejectsAnOverlongComposedName(t *testing.T) {
	long := strings.Repeat("a", maxSubjectToken-2)

	// Each part is legal on its own.
	if err := ValidateSubjectToken(long); err != nil {
		t.Fatalf("precondition: %q should be a legal token: %v", long, err)
	}

	if err := ValidateSubjectToken("prod"); err != nil {
		t.Fatalf("precondition: %q should be a legal token: %v", "prod", err)
	}

	_, err := New(&natsclient.Conn{}, nopHandler{}, "acme", long, WithSession("prod"))
	if !errors.Is(err, ErrInvalidSubjectToken) {
		t.Errorf("New with a %d-character composed name = %v, want ErrInvalidSubjectToken",
			len(InstanceName(long, "prod")), err)
	}
}

// The empty cases keep their existing sentinels, so callers matching on them
// still work.
func TestNewKeepsEmptySentinels(t *testing.T) {
	if _, err := New(&natsclient.Conn{}, nopHandler{}, "", "worker"); !errors.Is(err, ErrEmptyOwner) {
		t.Errorf("New with empty owner = %v, want ErrEmptyOwner", err)
	}

	if _, err := New(&natsclient.Conn{}, nopHandler{}, "acme", ""); !errors.Is(err, ErrEmptyName) {
		t.Errorf("New with empty name = %v, want ErrEmptyName", err)
	}
}

// The error must name which identifier was wrong: all four land in the same
// subject, and "invalid subject token" alone does not say which one to fix.
func TestNewNamesTheOffendingIdentifier(t *testing.T) {
	cases := []struct {
		field string
		owner string
		agent string
		opts  []ServerOption
	}{
		{"owner", "*", "worker", nil},
		{"name", "acme", "*", nil},
		{"agent id", "acme", "worker", []ServerOption{WithAgentID("*")}},
		{"session", "acme", "worker", []ServerOption{WithSession("*")}},
	}

	for _, tc := range cases {
		_, err := New(&natsclient.Conn{}, nopHandler{}, tc.owner, tc.agent, tc.opts...)
		if err == nil {
			t.Errorf("%s: New = nil, want an error", tc.field)
			continue
		}

		if !strings.Contains(err.Error(), tc.field) {
			t.Errorf("%s: error %q does not name the offending identifier", tc.field, err)
		}
	}
}

func TestNewAcceptsLegalIdentities(t *testing.T) {
	cases := []struct {
		name  string
		owner string
		agent string
		opts  []ServerOption
	}{
		{"plain", "acme", "worker", nil},
		{"hyphens and underscores", "acme-corp_1", "worker-v2", nil},
		{"with a session", "acme", "worker", []ServerOption{WithSession("prod")}},
		{"custom agent id", "acme", "worker", []ServerOption{WithAgentID("claude-code")}},
	}

	for _, tc := range cases {
		if _, err := New(&natsclient.Conn{}, nopHandler{}, tc.owner, tc.agent, tc.opts...); err != nil {
			t.Errorf("%s: New = %v, want nil", tc.name, err)
		}
	}
}
