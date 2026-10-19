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

package otel

import (
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"

	"github.com/henomis/phero/v2/llm"
	"github.com/henomis/phero/v2/trace"
)

// phero-specific attribute keys.
const (
	attrRunStopReason    = attribute.Key("phero.run.stop_reason")
	attrRunIterations    = attribute.Key("phero.run.iterations")
	attrRunMaxIterations = attribute.Key("phero.run.max_iterations")
	attrRunHandoffs      = attribute.Key("phero.run.handoffs")
	attrRunLLMCalls      = attribute.Key("phero.run.llm_calls")
	attrRunToolCalls     = attribute.Key("phero.run.tool_calls")
	attrRunToolErrors    = attribute.Key("phero.run.tool_errors")
	attrRunCostUSD       = attribute.Key("phero.run.cost_usd")
	attrRunTTFT          = attribute.Key("phero.run.time_to_first_token")
	attrLLMIteration     = attribute.Key("phero.llm.iteration")
	attrLLMCostUSD       = attribute.Key("phero.llm.cost_usd")
	attrLLMCacheHit      = attribute.Key("phero.llm.cache_hit")
	attrLLMRawFinish     = attribute.Key("phero.llm.raw_finish_reason")
	attrLLMResponseFmt   = attribute.Key("phero.llm.response_format")
	attrToolIteration    = attribute.Key("phero.tool.iteration")
	attrHandoffFrom      = attribute.Key("phero.handoff.from")
	attrHandoffTo        = attribute.Key("phero.handoff.to")
	attrHandoffContext   = attribute.Key("phero.handoff.context")
	attrMemoryOp         = attribute.Key("phero.memory.op")
	attrMemoryCount      = attribute.Key("phero.memory.count")
	attrMemoryQuery      = attribute.Key("phero.memory.query")
	attrRetrievalTopK    = attribute.Key("phero.retrieval.top_k")
	attrRetrievalHits    = attribute.Key("phero.retrieval.hits")
	attrEmbeddingInputs  = attribute.Key("phero.embedding.inputs")
	attrMiddlewareName   = attribute.Key("phero.middleware.name")
	attrMiddlewareTry    = attribute.Key("phero.middleware.attempt")
	attrMiddlewareWait   = attribute.Key("phero.middleware.wait")
	attrMiddlewareResult = attribute.Key("phero.middleware.outcome")
	attrMiddlewareReason = attribute.Key("phero.middleware.reason")
	attrMiddlewareRule   = attribute.Key("phero.middleware.rule")
	attrCustomType       = attribute.Key("phero.custom.type")
	attrCustomPrefix     = "phero.custom."
	attrSpanKind         = attribute.Key("phero.span.kind")
)

// providerNames maps phero provider names to gen_ai.provider.name values.
var providerNames = map[string]string{ //nolint:gochecknoglobals // read-only table
	"google": "gcp.gemini",
}

// spanName returns the semantic-convention span name of s.
func spanName(s *trace.SpanData) string {
	switch p := s.Payload.(type) {
	case *trace.Run:
		return string(trace.KindRun) + " " + p.Agent
	case *trace.LLMCall:
		if m := model(p); m != "" {
			return string(trace.KindLLM) + " " + m
		}

		return string(trace.KindLLM)
	case *trace.ToolCall:
		return string(trace.KindTool) + " " + p.Name
	case *trace.Embedding:
		if p.Model != "" {
			return string(trace.KindEmbedding) + " " + p.Model
		}

		return string(trace.KindEmbedding)
	default:
		return s.Name
	}
}

func model(p *trace.LLMCall) string {
	if p.RequestModel != "" {
		return p.RequestModel
	}

	return p.ResponseModel
}

// attributes returns the OpenTelemetry attributes of s.
func (e *Exporter) attributes(s *trace.SpanData) []attribute.KeyValue {
	attrs := convertAttrs(s.Attrs)

	switch s.Kind {
	case trace.KindRun, trace.KindLLM, trace.KindTool, trace.KindEmbedding, trace.KindRetrieval:
		attrs = append(attrs, semconv.GenAIOperationNameKey.String(string(s.Kind)))
	case trace.KindHandoff, trace.KindMemory, trace.KindMiddleware, trace.KindCustom:
		attrs = append(attrs, attrSpanKind.String(string(s.Kind)))
	}

	if s.Agent != "" {
		attrs = append(attrs, semconv.GenAIAgentName(s.Agent))
	}

	if s.SessionID != "" {
		attrs = append(attrs, semconv.GenAIConversationID(s.SessionID))
	}

	switch p := s.Payload.(type) {
	case *trace.Run:
		attrs = append(attrs, e.runAttrs(p, &s.Totals)...)
	case *trace.LLMCall:
		attrs = append(attrs, e.llmAttrs(s, p)...)
	case *trace.ToolCall:
		attrs = append(attrs, e.toolAttrs(p)...)
	case *trace.Handoff:
		attrs = append(attrs, attrHandoffFrom.String(p.From), attrHandoffTo.String(p.To))
		if e.capture {
			attrs = append(attrs, e.strAttr(attrHandoffContext, p.Context))
		}
	case *trace.Memory:
		attrs = append(attrs, attrMemoryOp.String(string(p.Op)), attrMemoryCount.Int(p.Count))
		if e.capture && p.Query != "" {
			attrs = append(attrs, e.strAttr(attrMemoryQuery, p.Query))
		}
	case *trace.Retrieval:
		attrs = append(attrs, e.retrievalAttrs(p)...)
	case *trace.Embedding:
		attrs = append(attrs, attrEmbeddingInputs.Int(p.Inputs), semconv.GenAIUsageInputTokens(p.Usage.InputTokens))
		if p.Model != "" {
			attrs = append(attrs, semconv.GenAIRequestModel(p.Model))
		}
	case *trace.Middleware:
		attrs = append(attrs, middlewareAttrs(p)...)
	case *trace.Custom:
		attrs = append(attrs, attrCustomType.String(p.Type))
		for k, v := range p.Data {
			attrs = append(attrs, convertAttr(trace.Attr{Key: attrCustomPrefix + k, Value: v}))
		}
	}

	return attrs
}

func (e *Exporter) runAttrs(r *trace.Run, t *trace.Totals) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		semconv.GenAIAgentName(r.Agent),
		attrRunStopReason.String(string(r.StopReason)),
		attrRunIterations.Int(r.Iterations),
		attrRunMaxIterations.Int(r.MaxIterations),
		attrRunLLMCalls.Int(t.LLMCalls),
		attrRunToolCalls.Int(t.ToolCalls),
		attrRunToolErrors.Int(t.ToolErrors),
		attrRunCostUSD.Float64(t.Usage.CostUSD),
	}
	attrs = append(attrs, usageAttrs(t.Usage.InputTokens, t.Usage.OutputTokens,
		t.Usage.CacheReadTokens, t.Usage.CacheWriteTokens, t.Usage.ReasoningTokens)...)

	if r.TimeToFirstToken > 0 {
		attrs = append(attrs, attrRunTTFT.Float64(r.TimeToFirstToken.Seconds()))
	}

	if len(r.Handoffs) > 0 {
		attrs = append(attrs, attrRunHandoffs.StringSlice(r.Handoffs))
	}

	if e.capture {
		if r.Description != "" {
			attrs = append(attrs, e.strAttr(semconv.GenAIAgentDescriptionKey, r.Description))
		}

		if len(r.Input) > 0 {
			attrs = append(attrs, e.jsonAttr(semconv.GenAIInputMessagesKey,
				[]semMessage{{Role: llm.RoleUser, Parts: semParts(r.Input)}}))
		}

		if len(r.Output) > 0 {
			attrs = append(attrs, e.jsonAttr(semconv.GenAIOutputMessagesKey,
				[]semMessage{{Role: llm.RoleAssistant, Parts: semParts(r.Output)}}))
		}
	}

	return attrs
}

func (e *Exporter) llmAttrs(s *trace.SpanData, c *trace.LLMCall) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attrLLMIteration.Int(c.Iteration),
		attrLLMCostUSD.Float64(c.CostUSD),
		semconv.GenAIRequestStream(c.Streaming),
	}
	attrs = append(attrs, usageAttrs(c.Usage.InputTokens, c.Usage.OutputTokens,
		c.Usage.CacheReadTokens, c.Usage.CacheWriteTokens, c.Usage.ReasoningTokens)...)

	if c.Provider != "" {
		name := c.Provider
		if mapped, ok := providerNames[name]; ok {
			name = mapped
		}

		attrs = append(attrs, semconv.GenAIProviderNameKey.String(name))
	}

	if c.RequestModel != "" {
		attrs = append(attrs, semconv.GenAIRequestModel(c.RequestModel))
	}

	if c.ResponseModel != "" {
		attrs = append(attrs, semconv.GenAIResponseModel(c.ResponseModel))
	}

	if c.FinishReason != "" {
		attrs = append(attrs, semconv.GenAIResponseFinishReasons(string(c.FinishReason)))
	}

	if c.RawFinishReason != "" {
		attrs = append(attrs, attrLLMRawFinish.String(c.RawFinishReason))
	}

	if c.ResponseFormat != "" {
		attrs = append(attrs, attrLLMResponseFmt.String(c.ResponseFormat))
	}

	if c.CacheHit {
		attrs = append(attrs, attrLLMCacheHit.Bool(true))
	}

	if !c.FirstTokenAt.IsZero() {
		attrs = append(attrs, semconv.GenAIResponseTimeToFirstChunk(c.FirstTokenAt.Sub(s.Start).Seconds()))
	}

	if e.capture {
		attrs = append(attrs, e.llmContentAttrs(s, c)...)
	}

	return attrs
}

func (e *Exporter) llmContentAttrs(s *trace.SpanData, c *trace.LLMCall) []attribute.KeyValue {
	var (
		attrs  []attribute.KeyValue
		system []semPart
		input  []semMessage
	)

	for _, m := range e.fullPrompt(s, c) {
		if m.Role == llm.RoleSystem {
			system = append(system, semParts(m.Parts)...)
			continue
		}

		input = append(input, semMessageOf(m, ""))
	}

	if len(system) > 0 {
		attrs = append(attrs, e.jsonAttr(semconv.GenAISystemInstructionsKey, system))
	}

	if len(input) > 0 {
		attrs = append(attrs, e.jsonAttr(semconv.GenAIInputMessagesKey, input))
	}

	if c.Response != nil {
		attrs = append(attrs, e.jsonAttr(semconv.GenAIOutputMessagesKey,
			[]semMessage{semMessageOf(*c.Response, string(c.FinishReason))}))
	}

	if len(c.Tools) > 0 {
		defs := make([]semToolDef, len(c.Tools))
		for i, t := range c.Tools {
			defs[i] = semToolDef{Type: "function", Name: t.Name, Description: t.Description, Parameters: t.Schema}
		}

		attrs = append(attrs, e.jsonAttr(semconv.GenAIToolDefinitionsKey, defs))
	}

	return attrs
}

func (e *Exporter) toolAttrs(t *trace.ToolCall) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		semconv.GenAIToolName(t.Name),
		semconv.GenAIToolTypeKey.String("function"),
		attrToolIteration.Int(t.Iteration),
	}

	if t.CallID != "" {
		attrs = append(attrs, semconv.GenAIToolCallID(t.CallID))
	}

	if e.capture {
		attrs = append(attrs,
			e.strAttr(semconv.GenAIToolCallArgumentsKey, t.Arguments),
			e.strAttr(semconv.GenAIToolCallResultKey, t.Result))
	}

	return attrs
}

func (e *Exporter) retrievalAttrs(r *trace.Retrieval) []attribute.KeyValue {
	attrs := []attribute.KeyValue{attrRetrievalTopK.Int(r.TopK), attrRetrievalHits.Int(len(r.Hits))}

	if e.capture {
		docs := make([]semDocument, len(r.Hits))
		for i, h := range r.Hits {
			docs[i] = semDocument{ID: h.ID, Score: h.Score, Content: h.Content}
		}

		attrs = append(attrs,
			e.strAttr(semconv.GenAIRetrievalQueryTextKey, r.Query),
			e.jsonAttr(semconv.GenAIRetrievalDocumentsKey, docs))
	}

	return attrs
}

func middlewareAttrs(m *trace.Middleware) []attribute.KeyValue {
	attrs := []attribute.KeyValue{attrMiddlewareName.String(m.Name)}

	if m.Attempt > 0 {
		attrs = append(attrs, attrMiddlewareTry.Int(m.Attempt))
	}

	if m.Wait > 0 {
		attrs = append(attrs, attrMiddlewareWait.Float64(m.Wait.Seconds()))
	}

	for _, kv := range []struct {
		key   attribute.Key
		value string
	}{{attrMiddlewareResult, m.Outcome}, {attrMiddlewareReason, m.Reason}, {attrMiddlewareRule, m.Rule}} {
		if kv.value != "" {
			attrs = append(attrs, kv.key.String(kv.value))
		}
	}

	return attrs
}

// usageAttrs returns the GenAI usage attributes. in excludes cached tokens,
// while gen_ai.usage.input_tokens includes every input token, cached or not.
func usageAttrs(in, out, cacheRead, cacheWrite, reasoning int) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		semconv.GenAIUsageInputTokens(in + cacheRead + cacheWrite),
		semconv.GenAIUsageOutputTokens(out),
	}

	if cacheRead > 0 {
		attrs = append(attrs, semconv.GenAIUsageCacheReadInputTokens(cacheRead))
	}

	if cacheWrite > 0 {
		attrs = append(attrs, semconv.GenAIUsageCacheCreationInputTokens(cacheWrite))
	}

	if reasoning > 0 {
		attrs = append(attrs, semconv.GenAIUsageReasoningOutputTokens(reasoning))
	}

	return attrs
}

func convertAttrs(attrs []trace.Attr) []attribute.KeyValue {
	out := make([]attribute.KeyValue, 0, len(attrs))
	for _, a := range attrs {
		out = append(out, convertAttr(a))
	}

	return out
}

func convertAttr(a trace.Attr) attribute.KeyValue {
	key := attribute.Key(a.Key)

	switch v := a.Value.(type) {
	case string:
		return key.String(v)
	case bool:
		return key.Bool(v)
	case int:
		return key.Int(v)
	case int64:
		return key.Int64(v)
	case float64:
		return key.Float64(v)
	case []string:
		return key.StringSlice(v)
	case time.Duration:
		return key.Float64(v.Seconds())
	case fmt.Stringer:
		return key.String(v.String())
	default:
		return key.String(strings.TrimSpace(fmt.Sprint(v)))
	}
}
