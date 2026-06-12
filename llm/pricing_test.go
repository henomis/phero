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
	"math"
	"testing"
)

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

func TestPricingCost(t *testing.T) {
	p := Pricing{InputPer1M: 3.00, OutputPer1M: 15.00, CacheReadPer1M: 0.30, CacheWritePer1M: 3.75}
	u := Usage{InputTokens: 1_000_000, OutputTokens: 1_000_000, CacheReadTokens: 1_000_000, CacheWriteTokens: 1_000_000}

	got := p.Cost(u)
	want := 3.00 + 15.00 + 0.30 + 3.75
	if !almostEqual(got, want) {
		t.Fatalf("Cost() = %v, want %v", got, want)
	}
}

func TestUsageCost_ExactAndPrefixMatch(t *testing.T) {
	// Exact match against a default entry.
	exact := Usage{InputTokens: 1_000_000, OutputTokens: 1_000_000}.Cost("gpt-4o-mini")
	if !almostEqual(exact, 0.15+0.60) {
		t.Fatalf("gpt-4o-mini cost = %v, want %v", exact, 0.15+0.60)
	}

	// Prefix match: dated model name resolves to the base model's pricing.
	prefixed := Usage{InputTokens: 1_000_000, OutputTokens: 1_000_000}.Cost("claude-sonnet-4-6")
	if !almostEqual(prefixed, 3.00+15.00) {
		t.Fatalf("claude-sonnet-4-6 cost = %v, want %v", prefixed, 3.00+15.00)
	}
}

func TestUsageCost_LongestPrefixWins(t *testing.T) {
	// "gpt-4o-mini" must win over "gpt-4o" for a gpt-4o-mini-* model.
	got := Usage{InputTokens: 1_000_000}.Cost("gpt-4o-mini-2024-07-18")
	if !almostEqual(got, 0.15) {
		t.Fatalf("longest-prefix cost = %v, want %v (gpt-4o-mini rate)", got, 0.15)
	}
}

func TestUsageCost_UnknownModelIsZero(t *testing.T) {
	if got := (Usage{InputTokens: 1_000_000}).Cost("some-unknown-model"); got != 0 {
		t.Fatalf("unknown model cost = %v, want 0", got)
	}
}

func TestRegisterPricing_OverrideAndCustom(t *testing.T) {
	RegisterPricing("my-local-model", Pricing{InputPer1M: 1.0, OutputPer1M: 2.0})
	got := Usage{InputTokens: 2_000_000, OutputTokens: 1_000_000}.Cost("my-local-model")
	if !almostEqual(got, 2.0+2.0) {
		t.Fatalf("custom model cost = %v, want %v", got, 4.0)
	}

	if _, ok := PricingFor("my-local-model"); !ok {
		t.Fatal("PricingFor: registered model not found")
	}
}
