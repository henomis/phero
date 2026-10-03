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
	"encoding/base64"
	"fmt"

	natsclient "github.com/nats-io/nats.go"
)

// Request is a prompt request (§5), the same on both ends: a caller builds one
// for [Client.Send], and a handler reads the one it is serving with
// [RequestFrom].
type Request struct {
	// Prompt is the prompt text. It must not be empty.
	Prompt string
	// Attachments are files sent inline with the prompt (§5.2). The agent must
	// advertise attachments_ok.
	Attachments []Attachment
	// Header holds extra NATS headers. The protocol defines none for a prompt,
	// so they are an agreement between caller and agent — a tenant ID, a
	// request ID, a W3C traceparent. Names starting with "Nats-" are reserved
	// by NATS. Headers count toward max_payload.
	//
	// A header is whatever the caller chose to send: it identifies nobody, and
	// is no basis for authorization.
	Header natsclient.Header
}

// Attachment is a file sent inline with a prompt (§5.2). Content holds the raw
// bytes; the wire's base64 encoding is done for you.
type Attachment struct {
	// Filename names the file; its extension tells the agent what it is.
	Filename string
	// Content is the file's bytes.
	Content []byte
}

// requestKey is the context key under which the server stores the request a
// handler is serving.
type requestKey struct{}

// RequestFrom returns the request a [Handler] is serving: the prompt, the
// attachments decoded to bytes, and the headers the caller sent. It is in the
// context the server passes to Run, and so in the context an agent passes to
// its tools.
//
// The attachments are all there, including those the LLM only saw as a
// "[attachment: name]" placeholder because they are not images. The Header is
// a copy: changing it changes nothing for other readers.
func RequestFrom(ctx context.Context) (*Request, bool) {
	r, ok := ctx.Value(requestKey{}).(*Request)

	return r, ok
}

// withRequest returns ctx carrying r for RequestFrom.
func withRequest(ctx context.Context, r *Request) context.Context {
	return context.WithValue(ctx, requestKey{}, r)
}

// envelope encodes r as the JSON envelope's fields (§5.1), base64-encoding the
// attachments (§5.2).
func (r *Request) envelope() envelope {
	env := envelope{Prompt: r.Prompt}

	for _, a := range r.Attachments {
		env.Attachments = append(env.Attachments, attachment{
			Filename: a.Filename,
			Content:  base64.StdEncoding.EncodeToString(a.Content),
		})
	}

	return env
}

// requestFromEnvelope builds the Request a handler sees from a decoded
// envelope and the request's headers, which it copies.
func requestFromEnvelope(env *envelope, header natsclient.Header) (*Request, error) {
	r := &Request{Prompt: env.Prompt}

	for _, a := range env.Attachments {
		content, err := base64.StdEncoding.DecodeString(a.Content)
		if err != nil {
			return nil, fmt.Errorf("%w: attachment %q: invalid base64: %v", ErrMalformedEnvelope, a.Filename, err)
		}

		r.Attachments = append(r.Attachments, Attachment{Filename: a.Filename, Content: content})
	}

	if len(header) > 0 {
		r.Header = make(natsclient.Header, len(header))
		for k, v := range header {
			r.Header[k] = append([]string(nil), v...)
		}
	}

	return r, nil
}
