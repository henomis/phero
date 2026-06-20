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
	"sync"
	"time"

	natsclient "github.com/nats-io/nats.go"
)

// HeartbeatTracker subscribes to the heartbeat wildcard agents.hb.*.*.* and
// tracks the liveness of each agent instance by instance_id (§8.1–§8.2).
//
// It is safe for concurrent use.
type HeartbeatTracker struct {
	mu   sync.RWMutex
	seen map[string]*heartbeatEntry
	sub  *natsclient.Subscription
}

type heartbeatEntry struct {
	payload  heartbeatPayload
	lastSeen time.Time
}

// NewHeartbeatTracker subscribes to agents.hb.*.*.* on nc and starts
// tracking liveness.  Call [HeartbeatTracker.Stop] when done.
func NewHeartbeatTracker(nc *natsclient.Conn) (*HeartbeatTracker, error) {
	t := &HeartbeatTracker{seen: make(map[string]*heartbeatEntry)}

	sub, err := nc.Subscribe("agents.hb.*.*.*", func(msg *natsclient.Msg) {
		var p heartbeatPayload
		if err := json.Unmarshal(msg.Data, &p); err != nil || p.InstanceID == "" {
			return
		}

		t.mu.Lock()
		t.seen[p.InstanceID] = &heartbeatEntry{payload: p, lastSeen: time.Now()}
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

	if !ok {
		return false
	}

	threshold := time.Duration(e.payload.IntervalS) * time.Second * 3

	return time.Since(e.lastSeen) <= threshold
}

// Stop cancels the wildcard subscription.
func (t *HeartbeatTracker) Stop() error {
	return t.sub.Unsubscribe()
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
			Ts:         time.Now().UTC().Format(time.RFC3339),
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
