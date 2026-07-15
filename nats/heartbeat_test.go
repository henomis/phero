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
	"testing"
	"time"
)

func TestAgentFromHeartbeatSubject(t *testing.T) {
	cases := []struct {
		subject     string
		owner, name string
		ok          bool
	}{
		{"agents.hb.phero.acme.worker", "acme", "worker", true},
		{"agents.hb.phero.acme.worker-sess", "acme", "worker-sess", true},
		{"agents.hb.phero.acme", "", "", false}, // too few tokens
		{"agents.hb.phero", "", "", false},
		{"", "", "", false},
	}

	for _, tc := range cases {
		owner, name, ok := agentFromHeartbeatSubject(tc.subject)
		if ok != tc.ok || owner != tc.owner || name != tc.name {
			t.Errorf("agentFromHeartbeatSubject(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tc.subject, owner, name, ok, tc.owner, tc.name, tc.ok)
		}
	}
}

// newTestTracker builds a tracker without a NATS subscription so the liveness
// logic can be exercised by injecting beats directly.
func newTestTracker() *HeartbeatTracker {
	return &HeartbeatTracker{
		seen:    make(map[string]*heartbeatEntry),
		byAgent: make(map[string]*heartbeatEntry),
	}
}

func beat(intervalS int, age time.Duration) *heartbeatEntry {
	return &heartbeatEntry{
		payload:  heartbeatPayload{IntervalS: intervalS},
		lastSeen: time.Now().Add(-age),
	}
}

func TestIsAgentOnline(t *testing.T) {
	tr := newTestTracker()

	// Fresh beat → online.
	tr.byAgent[agentKey("acme", "worker")] = beat(10, time.Second)
	if !tr.IsAgentOnline("acme", "worker") {
		t.Error("fresh beat should be online")
	}

	// Stale beat (older than 3× interval) → offline.
	tr.byAgent[agentKey("acme", "old")] = beat(10, 31*time.Second)
	if tr.IsAgentOnline("acme", "old") {
		t.Error("beat older than 3× interval should be offline")
	}

	// Never seen → offline.
	if tr.IsAgentOnline("acme", "ghost") {
		t.Error("an agent that never beat should be offline")
	}
}

// TestIsAgentOnline_IndependentFromInstanceIndex verifies the by-name index does
// not disturb the existing instance_id index, and vice versa.
func TestIsAgentOnline_IndependentFromInstanceIndex(t *testing.T) {
	tr := newTestTracker()

	e := beat(10, time.Second)
	tr.seen["NUID123"] = e
	tr.byAgent[agentKey("acme", "worker")] = e

	if !tr.IsOnline("NUID123") {
		t.Error("instance index should report online")
	}

	if !tr.IsAgentOnline("acme", "worker") {
		t.Error("agent index should report online")
	}

	if tr.IsOnline("acme/worker") {
		t.Error("agent key must not resolve through the instance index")
	}
}
