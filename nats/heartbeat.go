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
	"encoding/json"
	"strings"
	"sync"
	"time"

	natsclient "github.com/nats-io/nats.go"
)

// heartbeatMissedFactor is the number of missed heartbeat intervals before an
// agent is considered stale (§8.2).
const heartbeatMissedFactor = 3

// HeartbeatTracker subscribes to the heartbeat wildcard agents.hb.*.*.* and
// tracks agent liveness two ways: by instance_id (§8.1–§8.2) and by the
// (owner, name) identity carried in the heartbeat subject. The latter answers
// "is any instance of this agent alive?" — useful to callers that address agents
// by name rather than by the per-restart instance_id.
//
// It is safe for concurrent use.
type HeartbeatTracker struct {
	mu sync.RWMutex
	// seen indexes the most recent beat by instance_id.
	seen map[string]*heartbeatEntry
	// byAgent indexes the most recent beat by "owner/name"; because every beat
	// stamps the key, it reflects the liveness of whichever instance beat last,
	// i.e. "at least one replica of (owner, name) is alive".
	byAgent map[string]*heartbeatEntry
	sub     *natsclient.Subscription
}

type heartbeatEntry struct {
	payload  heartbeatPayload
	lastSeen time.Time
}

// online reports whether this beat is within the 3× interval_s staleness
// threshold (§8.2).
func (e *heartbeatEntry) online() bool {
	threshold := time.Duration(e.payload.IntervalS) * time.Second * heartbeatMissedFactor
	return time.Since(e.lastSeen) <= threshold
}

// NewHeartbeatTracker subscribes to agents.hb.*.*.* on nc and starts
// tracking liveness.  Call [HeartbeatTracker.Stop] when done.
func NewHeartbeatTracker(nc *natsclient.Conn) (*HeartbeatTracker, error) {
	t := &HeartbeatTracker{
		seen:    make(map[string]*heartbeatEntry),
		byAgent: make(map[string]*heartbeatEntry),
	}

	sub, err := nc.Subscribe("agents.hb.*.*.*", func(msg *natsclient.Msg) {
		var p heartbeatPayload
		if err := json.Unmarshal(msg.Data, &p); err != nil || p.InstanceID == "" {
			return
		}

		entry := &heartbeatEntry{payload: p, lastSeen: time.Now()}

		t.mu.Lock()
		t.seen[p.InstanceID] = entry
		// The name is not in the payload; it is the 5th token of the subject
		// agents.hb.{agent}.{owner}.{name}. Index by (owner, name) when present.
		if owner, name, ok := agentFromHeartbeatSubject(msg.Subject); ok {
			t.byAgent[agentKey(owner, name)] = entry
		}
		t.mu.Unlock()
	})
	if err != nil {
		return nil, err
	}

	t.sub = sub

	return t, nil
}

// IsOnline returns true if the given instance has been heard from within the
// 3× interval_s threshold defined in §8.2.
func (t *HeartbeatTracker) IsOnline(instanceID string) bool {
	t.mu.RLock()
	e, ok := t.seen[instanceID]
	t.mu.RUnlock()

	return ok && e.online()
}

// IsAgentOnline returns true if any instance of (owner, name) has been heard
// from within the 3× interval_s threshold (§8.2). Use this when you address an
// agent by its stable owner/name identity rather than the per-restart
// instance_id.
func (t *HeartbeatTracker) IsAgentOnline(owner, name string) bool {
	t.mu.RLock()
	e, ok := t.byAgent[agentKey(owner, name)]
	t.mu.RUnlock()

	return ok && e.online()
}

// Stop cancels the wildcard subscription.
func (t *HeartbeatTracker) Stop() error {
	return t.sub.Unsubscribe()
}

// agentKey is the composite map key for the (owner, name) index.
func agentKey(owner, name string) string { return owner + "/" + name }

// agentFromHeartbeatSubject extracts owner and name from a heartbeat subject
// agents.hb.{agent}.{owner}.{name}. ok is false if the subject is not shaped as
// the agents.hb.*.*.* wildcard delivers.
func agentFromHeartbeatSubject(subject string) (owner, name string, ok bool) {
	parts := strings.SplitN(subject, ".", natsSubjectParts)
	if len(parts) != natsSubjectParts {
		return "", "", false
	}

	return parts[3], parts[4], true
}

// startHeartbeats publishes heartbeats on agents.hb.{agent}.{owner}.{name}
// per §8.1.  The first heartbeat is published immediately so that subscribers
// who connect after the server does not need to wait a full interval (§8.5).
//
// The goroutine exits when ctx is done; callers must call wg.Done() when
// this returns.
func (s *Server) startHeartbeats(ctx context.Context, subject, instanceID string) {
	publish := func() {
		p := heartbeatPayload{
			Agent:      s.cfg.agentID,
			Owner:      s.owner,
			Session:    s.cfg.session,
			InstanceID: instanceID,
			TS:         time.Now().UTC().Format(time.RFC3339),
			IntervalS:  int(s.cfg.heartbeatInterval.Seconds()),
		}
		_ = s.nc.Publish(subject, encodeHeartbeat(p))
	}

	publish()

	ticker := time.NewTicker(s.cfg.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			publish()
		case <-ctx.Done():
			return
		}
	}
}
