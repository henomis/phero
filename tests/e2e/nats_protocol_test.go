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
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	natsio "github.com/nats-io/nats.go"

	"github.com/henomis/phero/v2/agent"
	"github.com/henomis/phero/v2/llm"
	natsagent "github.com/henomis/phero/v2/nats"
)

// echoHandler answers every prompt with a fixed text, without an LLM.
type echoHandler struct{}

func (echoHandler) Run(_ context.Context, _ ...llm.ContentPart) (*agent.Result, error) {
	return &agent.Result{Parts: []llm.ContentPart{llm.Text("ok")}}, nil
}

// textHandler answers every prompt with a fixed text, without an LLM.
type textHandler string

func (h textHandler) Run(_ context.Context, _ ...llm.ContentPart) (*agent.Result, error) {
	return &agent.Result{Parts: []llm.ContentPart{llm.Text(string(h))}}, nil
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

// TestNATS_LargeAnswerArrivesWhole checks, against a real broker, that an
// answer larger than the connection's max_payload reaches the caller whole,
// split into several response chunks (§6.3), instead of arriving empty.
func TestNATS_LargeAnswerArrivesWhole(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	nc := requireNATS(t)
	owner := "large-" + uuid.NewString()[:8]

	// Three times the broker's limit, with characters that JSON escaping inflates.
	unit := `<p class="x">é & 日本</p>` + "\n"
	answer := strings.Repeat(unit, int(3*nc.MaxPayload())/len(unit)+1)

	hbSub, err := nc.SubscribeSync("agents.hb.phero." + owner + ".*")
	if err != nil {
		t.Fatalf("subscribe heartbeats: %v", err)
	}
	defer hbSub.Unsubscribe() //nolint:errcheck

	srv, err := natsagent.New(nc, textHandler(answer), owner, "writer")
	if err != nil {
		t.Fatalf("natsagent.New: %v", err)
	}

	srvCtx, srvCancel := context.WithCancel(context.Background())
	t.Cleanup(srvCancel)

	go func() { _ = srv.Start(srvCtx) }()

	if _, err = hbSub.NextMsgWithContext(ctx); err != nil {
		t.Fatalf("waiting for a heartbeat: %v", err)
	}

	agents, err := natsagent.NewClient(nc).Discover(ctx, natsagent.FilterByOwner(owner))
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	stream, err := agents[0].Prompt(ctx, "write a long report")
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	defer stream.Close()

	got, err := stream.Text(ctx)
	if err != nil {
		t.Fatalf("Text: %v", err)
	}

	if got != answer {
		t.Fatalf("caller read %d bytes, want %d", len(got), len(answer))
	}
}

// respondWith serves subject with a hand-written agent that answers every
// prompt with msgs, in order, then the terminator. It stands in for an agent
// built with another SDK.
func respondWith(t *testing.T, nc *natsio.Conn, subject string, msgs ...*natsio.Msg) {
	t.Helper()

	sub, err := nc.Subscribe(subject, func(req *natsio.Msg) {
		for _, m := range msgs {
			m.Subject = req.Reply
			_ = nc.PublishMsg(m)
		}

		_ = nc.Publish(req.Reply, nil) // terminator (§6.5)
	})
	if err != nil {
		t.Fatalf("subscribe %s: %v", subject, err)
	}

	t.Cleanup(func() { _ = sub.Unsubscribe() })
}

// TestNATS_QueryEndsTheCall checks that an agent asking a question (§7) ends
// the call at once with the question, rather than after the inactivity timeout.
// The agent below never sends the terminator: it is waiting for the answer.
func TestNATS_QueryEndsTheCall(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	nc := requireNATS(t)
	subject := "agents.prompt.other-sdk.e2e." + uuid.NewString()[:8]

	sub, err := nc.Subscribe(subject, func(req *natsio.Msg) {
		_ = nc.Publish(req.Reply, []byte(`{"type":"status","data":"ack"}`))
		_ = nc.Publish(req.Reply, []byte(
			`{"type":"query","data":{"id":"q-1","reply_subject":"_INBOX.q1","prompt":"Delete 200 files?"}}`))
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer sub.Unsubscribe() //nolint:errcheck

	client := natsagent.NewClient(nc, natsagent.WithInactivityTimeout(20*time.Second))

	stream, err := client.Prompt(ctx, &natsagent.AgentInfo{PromptSubject: subject}, "clean up")
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	defer stream.Close()

	start := time.Now()
	_, err = stream.Text(ctx)

	var qe *natsagent.QueryError
	if !errors.As(err, &qe) || qe.Prompt != "Delete 200 files?" {
		t.Fatalf("Text error = %v, want a QueryError with the question", err)
	}

	if !natsagent.Permanent(err) {
		t.Fatal("a query must be permanent")
	}

	if waited := time.Since(start); waited > 5*time.Second {
		t.Fatalf("Text returned after %v, want right after the query", waited)
	}
}

// TestNATS_RateLimitIsRetryable checks that a 429 from another SDK's agent
// arrives as a retryable ServiceError carrying the body's retry_after_s.
func TestNATS_RateLimitIsRetryable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	nc := requireNATS(t)
	subject := "agents.prompt.other-sdk.e2e." + uuid.NewString()[:8]

	errMsg := natsio.NewMsg("")
	errMsg.Header.Set("Nats-Service-Error-Code", "429")
	errMsg.Header.Set("Nats-Service-Error", "Too Many Requests")
	errMsg.Data = []byte(`{"error":"rate_limited","message":"Too many concurrent requests","retry_after_s":30}`)

	respondWith(t, nc, subject, errMsg)

	stream, err := natsagent.NewClient(nc).Prompt(ctx, &natsagent.AgentInfo{PromptSubject: subject}, "hello")
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	defer stream.Close()

	_, err = stream.Text(ctx)

	var se *natsagent.ServiceError
	if !errors.As(err, &se) {
		t.Fatalf("Text error = %v, want a ServiceError", err)
	}

	if se.Code != 429 || se.ErrCode != "rate_limited" || se.RetryAfter != 30*time.Second {
		t.Fatalf("ServiceError = %+v", se)
	}

	if natsagent.Permanent(err) {
		t.Fatal("a 429 must be retryable")
	}
}

// failingHandler fails every run with err.
type failingHandler struct{ err error }

func (h failingHandler) Run(context.Context, ...llm.ContentPart) (*agent.Result, error) {
	return nil, h.err
}

// TestNATS_CodedErrorReachesTheCaller checks, phero to phero over a real
// broker, that the error a handler chooses arrives as the caller's
// ServiceError: a 429 with its wait is retryable, and the wait survives.
func TestNATS_CodedErrorReachesTheCaller(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	nc := requireNATS(t)
	owner := "coded-" + uuid.NewString()[:8]

	hbSub, err := nc.SubscribeSync("agents.hb.phero." + owner + ".*")
	if err != nil {
		t.Fatalf("subscribe heartbeats: %v", err)
	}
	defer hbSub.Unsubscribe() //nolint:errcheck

	handler := failingHandler{err: &natsagent.CodedError{
		Code: 429, ErrCode: "quota_exceeded", Message: "daily quota used up", RetryAfter: 30 * time.Second,
	}}

	srv, err := natsagent.New(nc, handler, owner, "billing")
	if err != nil {
		t.Fatalf("natsagent.New: %v", err)
	}

	srvCtx, srvCancel := context.WithCancel(context.Background())
	t.Cleanup(srvCancel)

	go func() { _ = srv.Start(srvCtx) }()

	if _, err = hbSub.NextMsgWithContext(ctx); err != nil {
		t.Fatalf("waiting for a heartbeat: %v", err)
	}

	agents, err := natsagent.NewClient(nc).Discover(ctx, natsagent.FilterByOwner(owner))
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	stream, err := agents[0].Prompt(ctx, "charge the invoice")
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	defer stream.Close()

	_, err = stream.Text(ctx)

	var se *natsagent.ServiceError
	if !errors.As(err, &se) {
		t.Fatalf("Text error = %v, want a ServiceError", err)
	}

	if se.Code != 429 || se.ErrCode != "quota_exceeded" || se.Message != "daily quota used up" ||
		se.RetryAfter != 30*time.Second {
		t.Fatalf("ServiceError = %+v", se)
	}

	if natsagent.Permanent(err) {
		t.Fatal("a 429 must be retryable")
	}
}

// echoRequestHandler answers with what RequestFrom shows it: the X-Tenant
// header, the prompt's length, and each attachment's name and size.
type echoRequestHandler struct{}

func (echoRequestHandler) Run(ctx context.Context, _ ...llm.ContentPart) (*agent.Result, error) {
	r, ok := natsagent.RequestFrom(ctx)
	if !ok {
		return nil, errors.New("no request in context")
	}

	answer := fmt.Sprintf("tenant=%s prompt=%d", r.Header.Get("X-Tenant"), len(r.Prompt))
	for _, a := range r.Attachments {
		answer += fmt.Sprintf(" %s=%d", a.Filename, len(a.Content))
	}

	return &agent.Result{Parts: []llm.ContentPart{llm.Text(answer)}}, nil
}

// startEchoRequestAgent serves echoRequestHandler and returns its handle.
func startEchoRequestAgent(ctx context.Context, t *testing.T, nc *natsio.Conn) *natsagent.AgentHandle {
	t.Helper()

	owner := "req-" + uuid.NewString()[:8]

	hbSub, err := nc.SubscribeSync("agents.hb.phero." + owner + ".*")
	if err != nil {
		t.Fatalf("subscribe heartbeats: %v", err)
	}
	defer hbSub.Unsubscribe() //nolint:errcheck

	srv, err := natsagent.New(nc, echoRequestHandler{}, owner, "echo", natsagent.WithAttachmentsOk(true))
	if err != nil {
		t.Fatalf("natsagent.New: %v", err)
	}

	srvCtx, srvCancel := context.WithCancel(context.Background())
	t.Cleanup(srvCancel)

	go func() { _ = srv.Start(srvCtx) }()

	if _, err = hbSub.NextMsgWithContext(ctx); err != nil {
		t.Fatalf("waiting for a heartbeat: %v", err)
	}

	agents, err := natsagent.NewClient(nc).Discover(ctx, natsagent.FilterByOwner(owner))
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	return agents[0]
}

// TestNATS_SendHeadersAndAttachments is issues #6 and #7, phero to phero: the
// client sends a header and a PDF, and the handler sees both.
func TestNATS_SendHeadersAndAttachments(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	nc := requireNATS(t)
	echo := startEchoRequestAgent(ctx, t, nc)

	stream, err := echo.Send(ctx, &natsagent.Request{
		Prompt:      "summarise",
		Attachments: []natsagent.Attachment{{Filename: "report.pdf", Content: make([]byte, 4096)}},
		Header:      natsio.Header{"X-Tenant": []string{"acme"}},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	defer stream.Close()

	got, err := stream.Text(ctx)
	if err != nil {
		t.Fatalf("Text: %v", err)
	}

	if want := "tenant=acme prompt=9 report.pdf=4096"; got != want {
		t.Fatalf("agent saw %q, want %q", got, want)
	}
}

// TestNATS_SendSizeIncludesHeaders checks the local size check against a real
// broker: a request whose payload plus headers is exactly max_payload is
// delivered, and one byte more fails locally with ErrPayloadTooLarge rather
// than at the broker.
func TestNATS_SendSizeIncludesHeaders(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	nc := requireNATS(t)
	echo := startEchoRequestAgent(ctx, t, nc)

	header := natsio.Header{"X-Tenant": []string{"acme"}}
	headerBytes := (&natsio.Msg{Header: header}).Size()
	envelopeOverhead := len(`{"prompt":""}`)
	promptLen := int(nc.MaxPayload()) - headerBytes - envelopeOverhead

	exact := &natsagent.Request{Prompt: strings.Repeat("a", promptLen), Header: header}

	stream, err := echo.Send(ctx, exact)
	if err != nil {
		t.Fatalf("Send at exactly max_payload: %v", err)
	}
	defer stream.Close()

	got, err := stream.Text(ctx)
	if err != nil {
		t.Fatalf("Text: %v", err)
	}

	if want := fmt.Sprintf("tenant=acme prompt=%d", promptLen); got != want {
		t.Fatalf("agent saw %q, want %q", got, want)
	}

	over := &natsagent.Request{Prompt: strings.Repeat("a", promptLen+1), Header: header}
	if _, err = echo.Send(ctx, over); !errors.Is(err, natsagent.ErrPayloadTooLarge) {
		t.Fatalf("Send one byte over: err = %v, want ErrPayloadTooLarge", err)
	}
}
