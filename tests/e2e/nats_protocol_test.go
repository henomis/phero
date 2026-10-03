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

//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/henomis/phero/agent"
	"github.com/henomis/phero/llm"
	natsagent "github.com/henomis/phero/nats"
)

// echoHandler answers every prompt with a fixed text, without an LLM.
type echoHandler struct{}

func (echoHandler) Run(_ context.Context, _ ...llm.ContentPart) (*agent.Result, error) {
	return &agent.Result{Parts: []llm.ContentPart{llm.Text("ok")}}, nil
}

// wireHeartbeat is the §8.3 payload as another SDK would read it.
type wireHeartbeat struct {
	InstanceID      string `json:"instance_id"`
	ProtocolVersion string `json:"protocol_version"`
	Endpoints       map[string]struct {
		Subject  string            `json:"subject"`
		Metadata map[string]string `json:"metadata"`
	} `json:"endpoints"`
}

// TestNATS_HeartbeatDeclaresPromptEndpoint checks, against a real broker, that
// heartbeats and status replies carry the latest draft's optional declarations
// (§8.3, §8.7) and that they match the discovery record (§3).
func TestNATS_HeartbeatDeclaresPromptEndpoint(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	nc := requireNATS(t)
	owner := "proto-" + uuid.NewString()[:8]

	// Subscribe before the agent starts, so the first heartbeat is not missed (§8.5).
	hbSub, err := nc.SubscribeSync("agents.hb.phero." + owner + ".*")
	if err != nil {
		t.Fatalf("subscribe heartbeats: %v", err)
	}
	defer hbSub.Unsubscribe() //nolint:errcheck

	srv, err := natsagent.New(nc, echoHandler{}, owner, "worker",
		natsagent.WithMaxPayload("512KB"),
		natsagent.WithAttachmentsOk(true),
		natsagent.WithHeartbeatInterval(time.Second),
	)
	if err != nil {
		t.Fatalf("natsagent.New: %v", err)
	}

	srvCtx, srvCancel := context.WithCancel(context.Background())
	t.Cleanup(srvCancel)

	go func() { _ = srv.Start(srvCtx) }()

	msg, err := hbSub.NextMsgWithContext(ctx)
	if err != nil {
		t.Fatalf("waiting for a heartbeat: %v", err)
	}

	agents, err := natsagent.NewClient(nc).Discover(ctx, natsagent.FilterByOwner(owner))
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	info := agents[0].AgentInfo

	statusReply, err := nc.RequestWithContext(ctx, info.StatusSubject, nil)
	if err != nil {
		t.Fatalf("status request: %v", err)
	}

	for source, data := range map[string][]byte{"heartbeat": msg.Data, "status reply": statusReply.Data} {
		var hb wireHeartbeat
		if jsonErr := json.Unmarshal(data, &hb); jsonErr != nil {
			t.Fatalf("%s: decode: %v (%s)", source, jsonErr, data)
		}

		if hb.InstanceID != info.InstanceID {
			t.Fatalf("%s: instance_id = %q, want %q", source, hb.InstanceID, info.InstanceID)
		}

		if hb.ProtocolVersion != info.ProtocolVersion {
			t.Fatalf("%s: protocol_version = %q, want the registered %q", source, hb.ProtocolVersion, info.ProtocolVersion)
		}

		prompt := hb.Endpoints["prompt"]
		if prompt.Subject != info.PromptSubject {
			t.Fatalf("%s: prompt subject = %q, want the registered %q", source, prompt.Subject, info.PromptSubject)
		}

		if prompt.Metadata["max_payload"] != "512KB" || prompt.Metadata["attachments_ok"] != "true" {
			t.Fatalf("%s: prompt metadata = %v, want the registered values", source, prompt.Metadata)
		}
	}
}
