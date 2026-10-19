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
	"log/slog"
)

// Log attribute keys used by LogAttrs.
const (
	LogKeyTraceID = "trace_id"
	LogKeySpanID  = "span_id"
)

// LogAttrs returns the trace_id and span_id of the current span in ctx (or of
// the remote parent set with Extract) as slog attributes, so log records can
// be correlated with traces. It returns nil when ctx carries no span.
func LogAttrs(ctx context.Context) []slog.Attr {
	sc := currentSpanContext(ctx)
	if !sc.IsValid() {
		return nil
	}

	return []slog.Attr{
		slog.String(LogKeyTraceID, sc.TraceID.String()),
		slog.String(LogKeySpanID, sc.SpanID.String()),
	}
}
