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

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/henomis/phero/agent"
	"github.com/henomis/phero/llm"
	simplemem "github.com/henomis/phero/memory/simple"
)

// Persona represents a simulated participant in the social simulation.
type Persona struct {
	Name        string `json:"name"`
	Background  string `json:"background"`
	Stance      string `json:"stance"`
	Personality string `json:"personality"`
}

// personaResponse is the JSON envelope returned by the persona-generation agent.
type personaResponse struct {
	Personas []Persona `json:"personas"`
}

// generatePersonas uses an LLM agent to produce n diverse personas grounded in worldFacts.
func generatePersonas(ctx context.Context, llmClient llm.LLM, worldFacts string, n int) ([]Persona, error) {
	orchestrator, err := agent.New(
		llmClient,
		"PersonaOrchestrator",
		strings.TrimSpace(`You are a persona generation agent.

Given world facts about a situation, generate diverse fictional personas who might interact in a social simulation about that topic.

Each persona must have:
- name: a realistic full name
- background: 1-2 sentence bio relevant to the topic
- stance: their specific, opinionated position on the central topic (must be unique and distinct per persona)
- personality: 2-3 comma-separated personality traits that influence how they express their views

Generate exactly the requested number of personas with genuinely conflicting stances.
Avoid echo chambers — ensure a wide spread of supporting, opposing, and nuanced views.

Return ONLY valid JSON with no markdown fencing, matching this exact structure:
{"personas": [{"name":"...","background":"...","stance":"...","personality":"..."}]}`),
	)
	if err != nil {
		return nil, fmt.Errorf("build persona orchestrator: %w", err)
	}

	prompt := fmt.Sprintf(
		"World facts:\n%s\n\nGenerate exactly %d personas with diverse and conflicting stances.",
		worldFacts, n,
	)

	result, err := orchestrator.Run(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("persona generation: %w", err)
	}

	raw := extractJSONObject(result.Content)

	var resp personaResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		return nil, fmt.Errorf("parse persona JSON: %w\nraw=%s", err, raw)
	}

	if len(resp.Personas) == 0 {
		return nil, fmt.Errorf("orchestrator returned zero personas")
	}

	return resp.Personas, nil
}

// buildPersonaAgent creates a persona agent with its own bounded memory.
// roundsHint is used to size the memory buffer (2 messages per round minimum).
func buildPersonaAgent(llmClient llm.LLM, p Persona, roundsHint int) (*personaAgent, error) {
	systemPrompt := fmt.Sprintf(
		strings.TrimSpace(`You are %s in a social simulation.

Background: %s
Your stance: %s
Personality: %s

Stay fully in character. React authentically to what others say.
Do not break character or refer to yourself as an AI.
Keep your posts concise (3–5 sentences).`),
		p.Name, p.Background, p.Stance, p.Personality,
	)

	a, err := agent.New(llmClient, p.Name, systemPrompt)
	if err != nil {
		return nil, fmt.Errorf("build agent for %q: %w", p.Name, err)
	}

	// Each round produces 2 messages (user prompt + assistant reply).
	// Add headroom for any extra context.
	memCapacity := uint(roundsHint*2 + 10)
	a.SetMemory(simplemem.New(memCapacity))

	return &personaAgent{name: p.Name, agent: a}, nil
}

// extractJSONObject extracts the outermost {...} object from s.
// It trims any surrounding prose that an LLM might emit.
func extractJSONObject(s string) string {
	s = strings.TrimSpace(s)

	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')

	if start == -1 || end == -1 || end <= start {
		return s
	}

	return s[start : end+1]
}
