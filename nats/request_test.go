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
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"

	natsclient "github.com/nats-io/nats.go"
	natsio "github.com/nats-io/nats.go/micro"

	"github.com/henomis/phero/agent"
	"github.com/henomis/phero/llm"
)

// offlineClient is a Client whose connection is never dialled: enough to build
// and check a request without a broker.
func offlineClient() *Client {
	return NewClient(&natsclient.Conn{})
}

func TestRequestMsg_EncodesAttachmentsAndHeaders(t *testing.T) {
	info := &AgentInfo{PromptSubject: "agents.prompt.a.o.n", AttachmentsOk: true}
	pdf := []byte("%PDF-1.7 not really")

	msg, err := offlineClient().requestMsg(info, &Request{
		Prompt:      "summarise",
		Attachments: []Attachment{{Filename: "report.pdf", Content: pdf}},
		Header:      natsclient.Header{"X-Tenant": []string{"acme"}},
	})
	if err != nil {
		t.Fatalf("requestMsg: %v", err)
	}

	if msg.Subject != info.PromptSubject || msg.Reply == "" || msg.Header.Get("X-Tenant") != "acme" {
		t.Fatalf("msg subject=%q reply=%q header=%v", msg.Subject, msg.Reply, msg.Header)
	}

	var env envelope
	if jsonErr := json.Unmarshal(msg.Data, &env); jsonErr != nil {
		t.Fatalf("body is not an envelope: %v", jsonErr)
	}

	if env.Prompt != "summarise" || len(env.Attachments) != 1 || env.Attachments[0].Filename != "report.pdf" ||
		env.Attachments[0].Content != base64.StdEncoding.EncodeToString(pdf) {
		t.Fatalf("envelope = %+v", env)
	}
}

func TestRequestMsg_Rejects(t *testing.T) {
	open := &AgentInfo{AttachmentsOk: true}
	closed := &AgentInfo{}
	file := []Attachment{{Filename: "a.png", Content: []byte{1}}}

	cases := []struct {
		name string
		info *AgentInfo
		req  *Request
		want error
	}{
		{"nil request", open, nil, ErrEmptyPrompt},
		{"blank prompt", open, &Request{Prompt: "  "}, ErrEmptyPrompt},
		{"attachments not accepted", closed, &Request{Prompt: "hi", Attachments: file}, ErrAttachmentsNotAllowed},
		{
			"attachment without filename", open,
			&Request{Prompt: "hi", Attachments: []Attachment{{Content: []byte{1}}}}, ErrInvalidAttachment,
		},
	}

	for _, tc := range cases {
		_, err := offlineClient().requestMsg(tc.info, tc.req)
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}

		if !Permanent(err) {
			t.Errorf("%s: %v must be permanent", tc.name, err)
		}
	}
}

// TestRequestMsg_HeadersCountTowardMaxPayload verifies that headers are part
// of the size check, as they are for nats.go: a request that fits without its
// headers but not with them fails locally.
func TestRequestMsg_HeadersCountTowardMaxPayload(t *testing.T) {
	req := &Request{Prompt: "hi"}

	msg, err := offlineClient().requestMsg(&AgentInfo{}, req)
	if err != nil {
		t.Fatalf("requestMsg: %v", err)
	}

	limit := int64(len(msg.Data))
	info := &AgentInfo{MaxPayloadBytes: limit}

	if _, err = offlineClient().requestMsg(info, req); err != nil {
		t.Fatalf("a request exactly at the limit must pass: %v", err)
	}

	req.Header = natsclient.Header{"X-Request-Id": []string{"r-1"}}
	if _, err = offlineClient().requestMsg(info, req); !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("err = %v, want ErrPayloadTooLarge once headers push it over", err)
	}
}

// recordingHandler keeps the request it was served, as RequestFrom shows it.
type recordingHandler struct {
	got *Request
}

func (h *recordingHandler) Run(ctx context.Context, _ ...llm.ContentPart) (*agent.Result, error) {
	h.got, _ = RequestFrom(ctx)

	return &agent.Result{Parts: []llm.ContentPart{llm.Text("ok")}}, nil
}

// TestProcessPrompt_RequestFrom is issue #7: the handler sees the caller's
// headers and every attachment's bytes, including one the LLM only gets as a
// placeholder.
func TestProcessPrompt_RequestFrom(t *testing.T) {
	pdf := []byte("%PDF-1.7 the report")

	body, err := json.Marshal(envelope{
		Prompt:      "summarise",
		Attachments: []attachment{{Filename: "report.pdf", Content: base64.StdEncoding.EncodeToString(pdf)}},
	})
	if err != nil {
		t.Fatal(err)
	}

	s, ctx := newTestServer(t, time.Second)
	s.cfg.attachmentsOk = true

	h := &recordingHandler{}
	s.handler = h

	headers := natsio.Headers{"X-Tenant": []string{"acme"}}
	s.processPrompt(ctx, &fakeRequest{data: body, headers: headers})

	if h.got == nil {
		t.Fatal("RequestFrom found no request in the handler's context")
	}

	if h.got.Prompt != "summarise" || h.got.Header.Get("X-Tenant") != "acme" {
		t.Fatalf("request = %+v", h.got)
	}

	if len(h.got.Attachments) != 1 || !bytes.Equal(h.got.Attachments[0].Content, pdf) {
		t.Fatalf("attachments = %+v, want the PDF's bytes", h.got.Attachments)
	}

	// The handler's header is a copy.
	h.got.Header.Set("X-Tenant", "changed")

	if headers["X-Tenant"][0] != "acme" {
		t.Fatal("changing the handler's header changed the request's")
	}
}

func TestRequestFrom_Absent(t *testing.T) {
	if r, ok := RequestFrom(context.Background()); ok || r != nil {
		t.Fatalf("RequestFrom = %v, %v; want nothing outside a served prompt", r, ok)
	}
}

// TestProcessPrompt_RequestReachesTools verifies that an agent's tools see the
// request too: the agent passes the handler's context on to them.
func TestProcessPrompt_RequestReachesTools(t *testing.T) {
	type input struct{}

	var seen string

	tenantTool, err := llm.NewTool("tenant", "returns the caller's tenant",
		func(ctx context.Context, _ input) (string, error) {
			if r, ok := RequestFrom(ctx); ok {
				seen = r.Header.Get("X-Tenant")
			}

			return seen, nil
		})
	if err != nil {
		t.Fatal(err)
	}

	calls := 0
	model := llm.Func(func(context.Context, []llm.Message, ...llm.CallOption) (*llm.Result, error) {
		calls++
		if calls == 1 {
			msg := llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{
				ID: "c1", Type: llm.ToolTypeFunction, Function: llm.FunctionCall{Name: "tenant", Arguments: "{}"},
			}}}

			return &llm.Result{Message: &msg}, nil
		}

		msg := llm.AssistantMessage([]llm.ContentPart{llm.Text("done")})

		return &llm.Result{Message: &msg}, nil
	})

	a, err := agent.New(model, "worker", "answers")
	if err != nil {
		t.Fatal(err)
	}

	if err = a.AddTool(tenantTool); err != nil {
		t.Fatal(err)
	}

	s, ctx := newTestServer(t, time.Second)
	s.handler = a

	s.processPrompt(ctx, &fakeRequest{data: []byte("who am I?"), headers: natsio.Headers{"X-Tenant": []string{"acme"}}})

	if seen != "acme" {
		t.Fatalf("tool saw tenant %q, want acme", seen)
	}
}
