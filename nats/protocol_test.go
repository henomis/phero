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
	"fmt"
	"reflect"
	"testing"
	"time"

	natsclient "github.com/nats-io/nats.go"
	natsio "github.com/nats-io/nats.go/micro"
)

// fakeRequest is a natsio.Request that records what the handler sends.
type fakeRequest struct {
	natsio.Request

	data    []byte
	headers natsio.Headers
	// maxPayload, when set, makes Respond reject a larger message the way a
	// broker with that max_payload would.
	maxPayload int

	errCode, errDesc string
	errBody          []byte
	responses        [][]byte
}

func (r *fakeRequest) Data() []byte { return r.data }

func (r *fakeRequest) Headers() natsio.Headers { return r.headers }

func (r *fakeRequest) Error(code, description string, data []byte, _ ...natsio.RespondOpt) error {
	r.errCode, r.errDesc, r.errBody = code, description, data
	return nil
}

func (r *fakeRequest) Respond(data []byte, _ ...natsio.RespondOpt) error {
	if r.maxPayload > 0 && len(data) > r.maxPayload {
		// micro.Request.Respond flattens the publish error into text, so
		// ErrMaxPayload does not survive in the chain (nats.go micro/request.go).
		return fmt.Errorf("%w: %s", natsio.ErrRespond, natsclient.ErrMaxPayload)
	}

	r.responses = append(r.responses, data)

	return nil
}

// TestHandlePrompt_DrainRefusalUsesTaxonomyCode verifies that a prompt refused
// during a drain carries a code from the §9.2 table, which has no 503, and is
// followed by the terminator (§9.3).
func TestHandlePrompt_DrainRefusalUsesTaxonomyCode(t *testing.T) {
	s, ctx := newTestServer(t, time.Second)
	s.gate.close()

	req := &fakeRequest{}
	s.handlePrompt(ctx, req)

	if req.errCode != "500" {
		t.Fatalf("refusal code = %q, want 500", req.errCode)
	}

	var body struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(req.errBody, &body); err != nil {
		t.Fatalf("error body is not JSON: %v (%s)", err, req.errBody)
	}

	if body.Error != errCodeServerDraining {
		t.Fatalf("error body code = %q, want %q", body.Error, errCodeServerDraining)
	}

	if len(req.responses) != 1 || len(req.responses[0]) != 0 {
		t.Fatalf("responses after the error = %q, want only the zero-byte terminator", req.responses)
	}
}

// TestServerHeartbeat_DeclaresPromptEndpoint verifies the optional §8.3
// declarations: the protocol version and the prompt endpoint, whose subject and
// metadata must match the registration verbatim.
func TestServerHeartbeat_DeclaresPromptEndpoint(t *testing.T) {
	s, _ := newTestServer(t, time.Second)
	s.cfg.attachmentsOk = true
	s.cfg.session = "prod"

	p := s.heartbeat("INSTANCE1")

	if p.InstanceID != "INSTANCE1" || p.Owner != "acme" || p.Session != "prod" || p.IntervalS <= 0 {
		t.Fatalf("core fields = %+v", p)
	}

	if p.ProtocolVersion != protocolVersion {
		t.Fatalf("protocol_version = %q, want %q", p.ProtocolVersion, protocolVersion)
	}

	prompt, ok := p.Endpoints[endpointPrompt]
	if !ok {
		t.Fatalf("endpoints = %+v, want a prompt declaration", p.Endpoints)
	}

	if prompt.Subject != "agents.prompt.phero.acme.worker" {
		t.Fatalf("prompt subject = %q", prompt.Subject)
	}

	if !reflect.DeepEqual(prompt.Metadata, s.promptMetadata()) {
		t.Fatalf("prompt metadata = %v, want the registered %v", prompt.Metadata, s.promptMetadata())
	}

	if prompt.Metadata["attachments_ok"] != "true" || prompt.Metadata["max_payload"] != defaultMaxPayload {
		t.Fatalf("prompt metadata = %v", prompt.Metadata)
	}

	// The wire form round-trips through the receiver's decoder.
	got, err := decodeHeartbeat(encodeHeartbeat(p))
	if err != nil {
		t.Fatalf("decodeHeartbeat: %v", err)
	}

	if !reflect.DeepEqual(got, p) {
		t.Fatalf("round trip = %+v, want %+v", got, p)
	}
}

// TestDecodeHeartbeat_MalformedDeclarationsKeepTheBeat verifies §8.3: a
// receiver ignores a malformed protocol_version or endpoints and keeps the
// heartbeat, so liveness tracking does not depend on the optional fields.
func TestDecodeHeartbeat_MalformedDeclarationsKeepTheBeat(t *testing.T) {
	cases := map[string]string{
		"endpoints not an object": `{"agent":"a","owner":"o","instance_id":"I1","ts":"t","interval_s":30,
			"protocol_version":"0.3","endpoints":"prompt"}`,
		"endpoint subject not a string": `{"agent":"a","owner":"o","instance_id":"I1","ts":"t","interval_s":30,
			"endpoints":{"prompt":{"subject":7}}}`,
		"protocol_version not a string": `{"agent":"a","owner":"o","instance_id":"I1","ts":"t","interval_s":30,
			"protocol_version":0.3}`,
		"declarations absent (pre-draft agent)": `{"agent":"a","owner":"o","instance_id":"I1","ts":"t","interval_s":30}`,
	}

	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			p, err := decodeHeartbeat([]byte(raw))
			if err != nil {
				t.Fatalf("decodeHeartbeat: %v", err)
			}

			if p.InstanceID != "I1" || p.IntervalS != 30 {
				t.Fatalf("core fields lost: %+v", p)
			}
		})
	}

	if _, err := decodeHeartbeat([]byte(`not json`)); err == nil {
		t.Fatal("decodeHeartbeat accepted a non-JSON payload")
	}
}

func TestCompatibleProtocol(t *testing.T) {
	cases := map[string]bool{
		"0.3":       true,
		"0.3.1":     true, // patch qualifiers carry no meaning (§11.1)
		"0.3-rc1":   true,
		"0.2":       false, // 0.x MINOR bumps may break the wire (§11.2)
		"0.4":       false,
		"1.0":       false, // different MAJOR
		"":          false,
		"0":         false,
		"0.":        false,
		"x.3":       false,
		"0.x":       false,
		"v0.3":      false,
		"00.3extra": false,
	}

	for v, want := range cases {
		if got := compatibleProtocol(v); got != want {
			t.Errorf("compatibleProtocol(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestParseAgentInfo_SkipsIncompatibleVersion(t *testing.T) {
	info := func(version string) []byte {
		b, err := json.Marshal(serviceInfoResponse{
			Name:     svcNameAgents,
			ID:       "I1",
			Metadata: map[string]string{metaAgent: defaultAgentID, metaOwner: "acme", metaProtocolVersion: version},
			Endpoints: []endpointInfoRaw{
				{Name: "prompt", Subject: "agents.prompt.phero.acme.worker"},
			},
		})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		return b
	}

	if got := parseAgentInfo(info("0.3")); got == nil || got.Name != "worker" {
		t.Fatalf("0.3 agent = %+v, want it discovered", got)
	}

	for _, v := range []string{"0.2", "1.0", ""} {
		if got := parseAgentInfo(info(v)); got != nil {
			t.Fatalf("protocol_version %q agent = %+v, want it skipped", v, got)
		}
	}
}

// TestStatusReplyMatchesHeartbeat verifies §8.7: the status endpoint replies
// with exactly the heartbeat schema, declarations included.
func TestStatusReplyMatchesHeartbeat(t *testing.T) {
	s, _ := newTestServer(t, time.Second)

	req := &fakeRequest{}
	s.handleStatus(context.Background(), req)

	if len(req.responses) != 1 {
		t.Fatalf("status responses = %d, want 1", len(req.responses))
	}

	p, err := decodeHeartbeat(req.responses[0])
	if err != nil {
		t.Fatalf("decodeHeartbeat: %v", err)
	}

	if p.ProtocolVersion != protocolVersion || p.Endpoints[endpointPrompt].Subject != s.promptSubject() {
		t.Fatalf("status reply = %+v, want the heartbeat declarations", p)
	}
}
