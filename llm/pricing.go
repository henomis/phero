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
	"strings"
	"sync"
)

// Pricing holds per-million-token costs for a model, expressed in US dollars.
//
// Rates are best-effort defaults and may drift as providers change prices; use
// RegisterPricing to override them or to add models not in the default table.
type Pricing struct {
	// InputPer1M is the cost in USD per 1,000,000 (uncached) input tokens.
	InputPer1M float64
	// OutputPer1M is the cost in USD per 1,000,000 output tokens.
	OutputPer1M float64
	// CacheReadPer1M is the cost in USD per 1,000,000 input tokens read from a prompt cache.
	CacheReadPer1M float64
	// CacheWritePer1M is the cost in USD per 1,000,000 input tokens written to a prompt cache.
	CacheWritePer1M float64
}

// Cost returns the US-dollar cost of the given usage under this pricing.
func (p Pricing) Cost(u Usage) float64 {
	const perMillion = 1_000_000.0
	return float64(u.InputTokens)/perMillion*p.InputPer1M +
		float64(u.OutputTokens)/perMillion*p.OutputPer1M +
		float64(u.CacheReadTokens)/perMillion*p.CacheReadPer1M +
		float64(u.CacheWriteTokens)/perMillion*p.CacheWritePer1M
}

// pricingMu guards pricingTable for concurrent access.
var pricingMu sync.RWMutex

// pricingTable maps a model-name prefix to its pricing. Lookups match the
// longest registered prefix, so dated model names (e.g. "gpt-4o-mini-2024-07-18")
// resolve to the base model's pricing.
var pricingTable = map[string]Pricing{
	// OpenAI (USD per 1M tokens).
	"gpt-4o-mini":  {InputPer1M: 0.15, OutputPer1M: 0.60, CacheReadPer1M: 0.075},
	"gpt-4o":       {InputPer1M: 2.50, OutputPer1M: 10.00, CacheReadPer1M: 1.25},
	"gpt-4.1-nano": {InputPer1M: 0.10, OutputPer1M: 0.40, CacheReadPer1M: 0.025},
	"gpt-4.1-mini": {InputPer1M: 0.40, OutputPer1M: 1.60, CacheReadPer1M: 0.10},
	"gpt-4.1":      {InputPer1M: 2.00, OutputPer1M: 8.00, CacheReadPer1M: 0.50},
	"o3-mini":      {InputPer1M: 1.10, OutputPer1M: 4.40, CacheReadPer1M: 0.55},
	"o3":           {InputPer1M: 2.00, OutputPer1M: 8.00, CacheReadPer1M: 0.50},

	// Anthropic (USD per 1M tokens).
	"claude-opus-4":     {InputPer1M: 15.00, OutputPer1M: 75.00, CacheReadPer1M: 1.50, CacheWritePer1M: 18.75},
	"claude-sonnet-4":   {InputPer1M: 3.00, OutputPer1M: 15.00, CacheReadPer1M: 0.30, CacheWritePer1M: 3.75},
	"claude-haiku-4":    {InputPer1M: 0.80, OutputPer1M: 4.00, CacheReadPer1M: 0.08, CacheWritePer1M: 1.00},
	"claude-3-5-sonnet": {InputPer1M: 3.00, OutputPer1M: 15.00, CacheReadPer1M: 0.30, CacheWritePer1M: 3.75},
	"claude-3-5-haiku":  {InputPer1M: 0.80, OutputPer1M: 4.00, CacheReadPer1M: 0.08, CacheWritePer1M: 1.00},
}

// RegisterPricing registers or overrides the pricing for a model name (or name
// prefix). It is safe for concurrent use.
//
// Use it to price custom, self-hosted, or newly released models, or to update a
// default rate that has changed.
func RegisterPricing(model string, p Pricing) {
	pricingMu.Lock()
	defer pricingMu.Unlock()
	pricingTable[model] = p
}

// PricingFor returns the pricing registered for model and whether one was found.
//
// It first tries an exact match, then falls back to the longest registered
// prefix of model (so "claude-sonnet-4-6" resolves to "claude-sonnet-4").
func PricingFor(model string) (Pricing, bool) {
	pricingMu.RLock()
	defer pricingMu.RUnlock()

	if p, ok := pricingTable[model]; ok {
		return p, true
	}

	var (
		best    Pricing
		bestLen = -1
	)
	for prefix, p := range pricingTable {
		if len(prefix) > bestLen && strings.HasPrefix(model, prefix) {
			best, bestLen = p, len(prefix)
		}
	}
	return best, bestLen >= 0
}

// Cost returns the best-effort US-dollar cost of this usage for the given model.
//
// It is best-effort: an unknown model (no registered or default pricing) yields
// 0 with no error, so callers can always sum costs safely.
func (u Usage) Cost(model string) float64 {
	p, ok := PricingFor(model)
	if !ok {
		return 0
	}
	return p.Cost(u)
}
