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

package trace

import (
	"context"
	"encoding/hex"
	"strings"
)

const (
	traceparentVersion = "00"
	traceparentLen     = 55
	flagSampled        = "01"
	flagNotSampled     = "00"
)

// Traceparent formats sc as a W3C traceparent header value. It returns an
// empty string when sc is not valid.
func (sc SpanContext) Traceparent() string {
	if !sc.IsValid() {
		return ""
	}

	flags := flagNotSampled
	if sc.Sampled {
		flags = flagSampled
	}

	return traceparentVersion + "-" + sc.TraceID.String() + "-" + sc.SpanID.String() + "-" + flags
}

// ParseTraceparent parses a W3C traceparent header value. The returned
// SpanContext has Remote set. It returns ErrInvalidTraceparent when s is
// malformed or carries all-zero identifiers.
func ParseTraceparent(s string) (SpanContext, error) {
	s = strings.TrimSpace(s)
	if len(s) < traceparentLen {
		return SpanContext{}, ErrInvalidTraceparent
	}

	parts := strings.Split(s, "-")
	//nolint:mnd // version-traceid-spanid-flags
	if len(parts) < 4 || len(parts[0]) != 2 || parts[0] == "ff" {
		return SpanContext{}, ErrInvalidTraceparent
	}

	// Version 00 has exactly four fields; later versions may append more.
	//nolint:mnd // see above
	if parts[0] == traceparentVersion && len(parts) != 4 {
		return SpanContext{}, ErrInvalidTraceparent
	}

	var sc SpanContext

	if err := decodeHex(sc.TraceID[:], parts[1]); err != nil {
		return SpanContext{}, err
	}

	if err := decodeHex(sc.SpanID[:], parts[2]); err != nil {
		return SpanContext{}, err
	}

	var flags [1]byte
	if err := decodeHex(flags[:], parts[3]); err != nil {
		return SpanContext{}, err
	}

	if !sc.IsValid() {
		return SpanContext{}, ErrInvalidTraceparent
	}

	sc.Sampled = flags[0]&1 == 1
	sc.Remote = true

	return sc, nil
}

func decodeHex(dst []byte, s string) error {
	if len(s) != hex.EncodedLen(len(dst)) || strings.ToLower(s) != s {
		return ErrInvalidTraceparent
	}

	if _, err := hex.Decode(dst, []byte(s)); err != nil {
		return ErrInvalidTraceparent
	}

	return nil
}

// Inject returns the traceparent of the current span in ctx, or of the remote
// parent set with Extract when no local span is active. It returns "" when
// ctx carries neither.
func Inject(ctx context.Context) string {
	return currentSpanContext(ctx).Traceparent()
}

// InjectState returns the tracestate to send alongside Inject, or "".
func InjectState(ctx context.Context) string {
	return currentSpanContext(ctx).TraceState
}

// Extract returns ctx with a remote parent taken from traceparent and an
// optional tracestate. Spans started from the returned context become children
// of the remote span. Invalid or empty values return ctx unchanged.
func Extract(ctx context.Context, traceparent string, tracestate ...string) context.Context {
	sc, err := ParseTraceparent(traceparent)
	if err != nil {
		return ctx
	}

	if len(tracestate) > 0 {
		sc.TraceState = tracestate[0]
	}

	// The extracted parent replaces any local span already in ctx.
	ctx = context.WithValue(ctx, spanKey, (*Span)(nil))

	return context.WithValue(ctx, remoteKey, sc)
}

func currentSpanContext(ctx context.Context) SpanContext {
	if s := SpanFrom(ctx); s != nil {
		return s.sc
	}

	sc, _ := ctx.Value(remoteKey).(SpanContext)

	return sc
}
