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

import "context"

// Attr is a key/value attribute attached to a span or span event.
//
// Value holds one of string, bool, int64, float64 or []string.
type Attr struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

// String returns a string attribute.
func String(key, value string) Attr { return Attr{Key: key, Value: value} }

// Bool returns a bool attribute.
func Bool(key string, value bool) Attr { return Attr{Key: key, Value: value} }

// Int returns an integer attribute stored as int64.
func Int(key string, value int) Attr { return Attr{Key: key, Value: int64(value)} }

// Int64 returns an int64 attribute.
func Int64(key string, value int64) Attr { return Attr{Key: key, Value: value} }

// Float64 returns a float64 attribute.
func Float64(key string, value float64) Attr { return Attr{Key: key, Value: value} }

// Strings returns a string-slice attribute.
func Strings(key string, value []string) Attr { return Attr{Key: key, Value: value} }

type ctxKey int

const (
	spanKey ctxKey = iota
	remoteKey
	providerKey
	attrsKey
	sessionKey
	linksKey
)

// WithAttributes returns a context whose spans all receive attrs, in addition
// to any attributes already carried by ctx. Use it for user, tenant, fleet or
// any other tag that should follow a request.
func WithAttributes(ctx context.Context, attrs ...Attr) context.Context {
	if len(attrs) == 0 {
		return ctx
	}

	prev := attributesFrom(ctx)
	merged := make([]Attr, 0, len(prev)+len(attrs))
	merged = append(merged, prev...)
	merged = append(merged, attrs...)

	return context.WithValue(ctx, attrsKey, merged)
}

func attributesFrom(ctx context.Context) []Attr {
	attrs, _ := ctx.Value(attrsKey).([]Attr)
	return attrs
}

// WithSession returns a context carrying the session (conversation or
// thread) identifier. Spans started from it record the identifier in
// SpanData.SessionID and agent runs copy it into Run.SessionID.
func WithSession(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, sessionKey, id)
}

// SessionFrom returns the session identifier carried by ctx, or "".
func SessionFrom(ctx context.Context) string {
	id, _ := ctx.Value(sessionKey).(string)
	return id
}

// WithLinks returns a context whose next started span is linked to scs. The
// links apply to that span only, not to its children. Use it to link a
// handed-off run back to the handoff that requested it.
func WithLinks(ctx context.Context, scs ...SpanContext) context.Context {
	if len(scs) == 0 {
		return ctx
	}

	return context.WithValue(ctx, linksKey, scs)
}

func linksFrom(ctx context.Context) []SpanContext {
	links, _ := ctx.Value(linksKey).([]SpanContext)
	return links
}
