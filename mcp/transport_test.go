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

package mcp

import (
	"slices"
	"strings"
	"testing"
)

// The whole point of StdioTransport: an MCP server is third-party code chosen by
// configuration, and the parent of an agent process holds provider API keys,
// database DSNs and broker credentials. exec.Command's default — inherit
// everything — hands all of them over.
func TestStdioTransportDoesNotLeakTheParentEnvironment(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-secret")
	t.Setenv("DATABASE_URL", "postgres://user:pw@host/db")
	t.Setenv("NATS_CREDS", "/run/secrets/nats.creds")
	t.Setenv("PATH", "/usr/bin")

	transport := StdioTransport("echo", []string{"hi"}, nil)

	env := transport.Command.Env
	if env == nil {
		t.Fatal("Env is nil, so the subprocess would inherit the parent environment")
	}

	for _, secret := range []string{"ANTHROPIC_API_KEY", "DATABASE_URL", "NATS_CREDS"} {
		for _, entry := range env {
			if strings.HasPrefix(entry, secret+"=") {
				t.Errorf("%s was forwarded to the subprocess", secret)
			}
		}
	}

	if !slices.Contains(env, "PATH=/usr/bin") {
		t.Error("PATH must be forwarded; the command cannot be found without it")
	}
}

func TestStdioTransportForwardsOnlyWhatIsAsked(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-secret")
	t.Setenv("PATH", "/usr/bin")

	transport := StdioTransport("echo", nil, map[string]string{"MCP_TOKEN": "abc"})

	if !slices.Contains(transport.Command.Env, "MCP_TOKEN=abc") {
		t.Error("an explicitly configured variable must be forwarded")
	}

	for _, entry := range transport.Command.Env {
		if strings.HasPrefix(entry, "ANTHROPIC_API_KEY=") {
			t.Error("configuring one variable must not re-open the whole environment")
		}
	}
}

// An unset whitelisted variable must not become an empty one: "HOME=" is a
// different, worse thing than no HOME at all.
func TestStdioTransportSkipsUnsetVariables(t *testing.T) {
	t.Setenv("PATH", "/usr/bin")
	t.Setenv("LC_ALL", "")

	transport := StdioTransport("echo", nil, nil)

	for _, entry := range transport.Command.Env {
		if entry == "TMPDIR=" || entry == "LANG=" {
			t.Errorf("unset variable forwarded as empty: %q", entry)
		}
	}
}

func TestAllowDeny(t *testing.T) {
	cases := []struct {
		name  string
		allow []string
		deny  []string
		tool  string
		want  bool
	}{
		{"no lists admits everything", nil, nil, "anything", true},
		{"allow admits a listed tool", []string{"read"}, nil, "read", true},
		{"allow excludes an unlisted tool", []string{"read"}, nil, "write", false},
		{"deny excludes a listed tool", nil, []string{"write"}, "write", false},
		{"deny admits an unlisted tool", nil, []string{"write"}, "read", true},
		// Two rules disagreeing is resolved the safe way.
		{"deny wins over allow", []string{"write"}, []string{"write"}, "write", false},
	}

	for _, tc := range cases {
		filter := AllowDeny(tc.allow, tc.deny)
		if filter == nil {
			if !tc.want {
				t.Errorf("%s: a nil filter admits everything, but the tool should be excluded", tc.name)
			}

			continue
		}

		if got := filter(tc.tool); got != tc.want {
			t.Errorf("%s: filter(%q) = %v, want %v", tc.name, tc.tool, got, tc.want)
		}
	}
}

// Both lists empty must yield nil, which AsTools reads as "no filtering" —
// rather than a filter that has to be consulted for every tool to say yes.
func TestAllowDenyEmptyIsNil(t *testing.T) {
	if AllowDeny(nil, nil) != nil {
		t.Error("AllowDeny(nil, nil) should be nil")
	}

	if AllowDeny([]string{}, []string{}) != nil {
		t.Error("AllowDeny with two empty lists should be nil")
	}
}
