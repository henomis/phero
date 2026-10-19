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

	"github.com/henomis/phero/v2/llm"
)

// InstrumentLLM wraps an llm.LLM so that each Execute call outside an agent
// is recorded as a span with an *LLMCall payload. Spans go to the provider in
// the call's context (see WithProvider).
//
// Calls made inside an agent run are already traced by the agent; the
// wrapper then adds nothing, so it is safe to share one wrapped client
// between agents and direct calls.
func InstrumentLLM(inner llm.LLM) llm.LLM {
	return &instrumentedLLM{inner: inner}
}

type instrumentedLLM struct {
	inner llm.LLM
}

func (l *instrumentedLLM) Execute(
	ctx context.Context, messages []llm.Message, opts ...llm.CallOption,
) (*llm.Result, error) {
	if _, ok := SpanFrom(ctx).Payload().(*LLMCall); ok {
		return l.inner.Execute(ctx, messages, opts...)
	}

	call := &LLMCall{Messages: messages}
	if d, ok := l.inner.(llm.Describer); ok {
		call.Provider = d.Provider()
		call.RequestModel = d.Model()
	}

	cfg := llm.NewCallConfig(opts...)
	for _, t := range cfg.Tools {
		call.Tools = append(call.Tools, ToolDef{Name: t.Name(), Description: t.Description(), Schema: t.InputSchema()})
	}

	call.ToolChoice = cfg.ToolChoice
	if cfg.ResponseFormat != nil {
		call.ResponseFormat = cfg.ResponseFormat.Name()
	}

	name := string(KindLLM)
	if call.RequestModel != "" {
		name += " " + call.RequestModel
	}

	ctx, span := Start(ctx, name, call)

	res, err := l.inner.Execute(ctx, messages, opts...)
	if res != nil {
		call.Response = res.Message
		call.ResponseModel = res.Model
		call.FinishReason = res.FinishReason
		call.RawFinishReason = res.RawFinishReason

		if res.Usage != nil {
			call.Usage = *res.Usage
		}

		if !call.CacheHit {
			call.CostUSD = call.Usage.Cost(call.ResponseModel)
		}
	}

	span.End(err)

	return res, err
}
