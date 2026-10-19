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

package agent

import (
	"context"
	"time"

	"github.com/henomis/phero/v2/llm"
	"github.com/henomis/phero/v2/trace"
)

// startLLMCall starts the span of one model call. The payload records only the
// messages added since the run's previous call, from rs.promptLen on.
func (a *Agent) startLLMCall(
	ctx context.Context, rs *runState, session []llm.Message, iteration int, streaming bool,
) (context.Context, *trace.Span, *trace.LLMCall) {
	offset := min(rs.promptLen, len(session))
	call := &trace.LLMCall{
		Iteration:     iteration,
		MessageOffset: offset,
		Messages:      session[offset:len(session):len(session)],
		Tools:         a.toolDefs(),
		Streaming:     streaming,
	}
	rs.promptLen = len(session)

	if d, ok := a.llm.(llm.Describer); ok {
		call.Provider = d.Provider()
		call.RequestModel = d.Model()
	}

	if a.responseFormat != nil {
		call.ResponseFormat = a.responseFormat.Name()
	}

	name := string(trace.KindLLM)
	if call.RequestModel != "" {
		name += " " + call.RequestModel
	}

	ctx, span := trace.Start(ctx, name, call, trace.WithStartTime(time.Now()))

	return ctx, span, call
}

// endLLMCall records the model result on call and ends span.
func endLLMCall(span *trace.Span, call *trace.LLMCall, res *llm.Result, err error) {
	if res != nil {
		call.Response = res.Message
		call.ResponseModel = res.Model
		call.FinishReason = res.FinishReason
		call.RawFinishReason = res.RawFinishReason

		if res.Usage != nil {
			call.Usage = *res.Usage
		}

		if !call.CacheHit {
			model := call.ResponseModel
			if model == "" {
				model = call.RequestModel
			}

			call.CostUSD = call.Usage.Cost(model)
		}
	}

	span.End(err)
}

// toolDefs describes the agent's tools for the LLM call payload.
func (a *Agent) toolDefs() []trace.ToolDef {
	if len(a.tools) == 0 {
		return nil
	}

	defs := make([]trace.ToolDef, len(a.tools))
	for i, t := range a.tools {
		defs[i] = trace.ToolDef{Name: t.Name(), Description: t.Description(), Schema: t.InputSchema()}
	}

	return defs
}
