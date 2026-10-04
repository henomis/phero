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

package llm

import (
	"fmt"
)

// CallOption configures a single LLM call. Pass any number of them to
// [LLM.Execute] or [StreamingLLM.ExecuteStream].
//
// Options are per call rather than per client because they belong to the
// caller: several agents often share one client, each with its own tools and
// output contract.
type CallOption func(*CallConfig)

// ToolChoiceMode selects how the model may use the tools offered on a call.
type ToolChoiceMode string

const (
	// ToolChoiceAuto lets the model decide whether to call a tool. It is the
	// provider default.
	ToolChoiceAuto ToolChoiceMode = "auto"
	// ToolChoiceNone forbids tool calls; the model must answer directly.
	ToolChoiceNone ToolChoiceMode = "none"
	// ToolChoiceRequired forces the model to call at least one tool.
	ToolChoiceRequired ToolChoiceMode = "required"
	// ToolChoiceTool forces the model to call the tool named in ToolChoice.Name.
	// Set it with [WithForcedTool].
	ToolChoiceTool ToolChoiceMode = "tool"
)

// ToolChoice is the resolved tool-choice setting of a call.
type ToolChoice struct {
	// Mode is how the model may use tools.
	Mode ToolChoiceMode
	// Name is the forced tool when Mode is ToolChoiceTool; empty otherwise.
	Name string
}

// CallConfig is the resolved form of a call's options. Backends and middleware
// build it with [NewCallConfig] to inspect what the caller asked for.
type CallConfig struct {
	// Tools are the tools the model may call.
	Tools []*Tool
	// ToolChoice constrains tool use; nil leaves the provider default (auto).
	ToolChoice *ToolChoice
	// ResponseFormat constrains the final answer to a JSON schema; nil means
	// free-form text.
	ResponseFormat *ResponseFormat
}

// NewCallConfig applies opts, in order, to an empty CallConfig. Nil options
// are skipped.
func NewCallConfig(opts ...CallOption) *CallConfig {
	cfg := &CallConfig{}

	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}

	return cfg
}

// Validate reports whether the configuration is coherent: a known tool-choice
// mode, a forced tool that is actually offered, and no tool requirement on a
// call that offers none. Backends call it before building a request, so an
// inconsistent call fails locally rather than as an opaque provider error.
func (c *CallConfig) Validate() error {
	if c.ToolChoice != nil {
		switch c.ToolChoice.Mode {
		case ToolChoiceAuto, ToolChoiceNone:
		case ToolChoiceRequired:
			if len(c.Tools) == 0 {
				return fmt.Errorf("%w: %q requires at least one tool", ErrInvalidToolChoice, c.ToolChoice.Mode)
			}
		case ToolChoiceTool:
			if c.ToolChoice.Name == "" {
				return fmt.Errorf("%w: forced tool has no name", ErrInvalidToolChoice)
			}

			if !c.hasTool(c.ToolChoice.Name) {
				return fmt.Errorf("%w: forced tool %q is not offered on this call", ErrInvalidToolChoice, c.ToolChoice.Name)
			}
		default:
			return fmt.Errorf("%w: unknown mode %q", ErrInvalidToolChoice, c.ToolChoice.Mode)
		}
	}

	if c.ResponseFormat != nil && (c.ResponseFormat.name == "" || c.ResponseFormat.schema == nil) {
		return ErrInvalidResponseFormat
	}

	return nil
}

func (c *CallConfig) hasTool(name string) bool {
	for _, t := range c.Tools {
		if t != nil && t.Name() == name {
			return true
		}
	}

	return false
}

// WithTools offers tools to the model on this call. It appends, so it may be
// passed more than once; nil tools are skipped.
func WithTools(tools ...*Tool) CallOption {
	return func(c *CallConfig) {
		for _, t := range tools {
			if t != nil {
				c.Tools = append(c.Tools, t)
			}
		}
	}
}

// WithToolChoice sets how the model may use the offered tools. To force one
// specific tool use [WithForcedTool].
func WithToolChoice(mode ToolChoiceMode) CallOption {
	return func(c *CallConfig) {
		c.ToolChoice = &ToolChoice{Mode: mode}
	}
}

// WithForcedTool forces the model to call the tool with the given name, which
// must also be offered via [WithTools].
func WithForcedTool(name string) CallOption {
	return func(c *CallConfig) {
		c.ToolChoice = &ToolChoice{Mode: ToolChoiceTool, Name: name}
	}
}

// WithResponseFormat constrains the model's answer to the given JSON schema
// (structured output). Build one with [NewResponseFormat] or
// [NewRawResponseFormat]. A nil format clears any earlier one.
func WithResponseFormat(format *ResponseFormat) CallOption {
	return func(c *CallConfig) {
		c.ResponseFormat = format
	}
}
