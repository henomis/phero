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
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/henomis/phero/agent"
	"github.com/henomis/phero/llm"
	"github.com/henomis/phero/llm/openai"
)

// Outline is the structured document plan produced by the Outliner agent.
type Outline struct {
	Title    string           `json:"title"`
	Sections []OutlineSection `json:"sections"`
}

// OutlineSection represents a single section in the document outline.
type OutlineSection struct {
	Title     string   `json:"title"`
	KeyPoints []string `json:"key_points"`
}

func main() {
	var topic string
	var timeout time.Duration
	flag.StringVar(&topic, "topic", "The impact of artificial intelligence on software development", "Document topic")
	flag.DurationVar(&timeout, "timeout", 5*time.Minute, "Overall timeout for the run")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	llmClient, llmInfo := buildLLMFromEnv()

	outliner, expander, formatter, err := buildAgents(llmClient)
	if err != nil {
		panic(err)
	}

	fmt.Println("multi-agent architecture example: prompt chaining with programmatic gate")
	fmt.Println("- llm:", llmInfo)
	fmt.Println("- topic:", topic)
	fmt.Println()

	// Step 1: Outliner produces a structured JSON outline.
	fmt.Println("step 1: generating outline...")

	outlineOut, err := outliner.Run(ctx, fmt.Sprintf("Create a document outline for the topic: %q", topic))
	if err != nil {
		panic(fmt.Errorf("outliner failed: %w", err))
	}

	outline, err := parseOutline(outlineOut.Content)
	if err != nil {
		panic(fmt.Errorf("outline gate failed: %w", err))
	}

	// Programmatic gate: verify the outline has enough structure before continuing.
	if err := gateOutline(outline); err != nil {
		panic(fmt.Errorf("outline did not pass quality gate: %w", err))
	}

	fmt.Printf("outline validated: %q (%d sections)\n\n", outline.Title, len(outline.Sections))

	// Step 2: Expander writes full prose for each section based on the outline.
	fmt.Println("step 2: expanding outline into prose...")

	expansionPrompt := buildExpansionPrompt(topic, outline)

	expandOut, err := expander.Run(ctx, expansionPrompt)
	if err != nil {
		panic(fmt.Errorf("expander failed: %w", err))
	}

	expanded := strings.TrimSpace(expandOut.Content)

	fmt.Printf("expanded content (%d chars)\n\n", len(expanded))

	// Step 3: Formatter polishes the draft into a final document.
	fmt.Println("step 3: formatting final document...")

	formatPrompt := fmt.Sprintf(
		"Polish and format the following document into a clean, well-structured final article.\n\nTopic: %s\n\nDraft:\n%s",
		topic, expanded,
	)

	formatOut, err := formatter.Run(ctx, formatPrompt)
	if err != nil {
		panic(fmt.Errorf("formatter failed: %w", err))
	}

	fmt.Println("=== final document ===")
	fmt.Println(strings.TrimSpace(formatOut.Content))
}

func buildLLMFromEnv() (llm.LLM, string) {
	apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	baseURL := strings.TrimSpace(os.Getenv("OPENAI_BASE_URL"))
	model := strings.TrimSpace(os.Getenv("OPENAI_MODEL"))

	if apiKey == "" && baseURL == "" {
		baseURL = openai.OllamaBaseURL
	}

	if model == "" {
		if baseURL == openai.OllamaBaseURL && apiKey == "" {
			model = "gpt-oss:20b-cloud"
		} else {
			model = openai.DefaultModel
		}
	}

	opts := []openai.Option{openai.WithModel(model)}
	if baseURL != "" {
		opts = append(opts, openai.WithBaseURL(baseURL))
	}
	client := openai.New(apiKey, opts...)

	info := fmt.Sprintf("model=%s base_url=%s", model, baseURL)
	if baseURL == "" {
		info = fmt.Sprintf("model=%s", model)
	}

	return client, info
}

func buildAgents(llmClient llm.LLM) (outliner, expander, formatter *agent.Agent, err error) {
	outliner, err = agent.New(llmClient, "Outliner Agent", strings.TrimSpace(`You are a document planning agent.

Return ONLY valid JSON - no markdown, no extra text.

Given a topic, produce a structured document outline.

Output schema:
{
  "title": "...",
  "sections": [
    {
      "title": "section title",
      "key_points": ["point 1", "point 2"]
    }
  ]
}

Requirements:
- Include at least 3 sections.
- Each section must have at least 2 key points.
- Keep section titles concise (3-7 words).`))
	if err != nil {
		return nil, nil, nil, err
	}

	expander, err = agent.New(llmClient, "Expander Agent", strings.TrimSpace(`You are a technical writing agent.

You receive a document outline (title, sections, key points) and a topic.
Your task: write full prose paragraphs for each section, covering all key points.

Guidelines:
- Write one paragraph per section (3-5 sentences each).
- Keep a consistent, professional tone.
- Do not introduce new sections; follow the outline exactly.
- Output just the body text, with each section preceded by its title as a plain heading.`))
	if err != nil {
		return nil, nil, nil, err
	}

	formatter, err = agent.New(llmClient, "Formatter Agent", strings.TrimSpace(`You are a document formatting agent.

You receive a draft article and its topic.
Your task: produce the final, polished version.

Guidelines:
- Add a compelling title if one is missing or weak.
- Ensure consistent heading hierarchy.
- Fix any awkward phrasing or repetition.
- Add a brief introduction paragraph if missing.
- Add a brief conclusion paragraph if missing.
- Output the complete, publication-ready document.`))
	if err != nil {
		return nil, nil, nil, err
	}

	return outliner, expander, formatter, nil
}

// parseOutline extracts and parses the JSON outline from the outliner's raw output.
func parseOutline(raw string) (Outline, error) {
	cleaned := extractJSONObject(raw)

	var outline Outline
	if err := json.Unmarshal([]byte(cleaned), &outline); err != nil {
		return Outline{}, fmt.Errorf("parse error: %w (raw: %s)", err, raw)
	}

	return outline, nil
}

// gateOutline is a programmatic quality gate that validates the outline before
// passing it to the next agent. No LLM call is made here - this intentionally
// demonstrates how workflows use deterministic Go logic between agent steps.
func gateOutline(outline Outline) error {
	if strings.TrimSpace(outline.Title) == "" {
		return fmt.Errorf("outline has no title")
	}

	const minSections = 2
	if len(outline.Sections) < minSections {
		return fmt.Errorf("outline has %d section(s); need at least %d", len(outline.Sections), minSections)
	}

	for i, s := range outline.Sections {
		if strings.TrimSpace(s.Title) == "" {
			return fmt.Errorf("section %d has no title", i)
		}

		if len(s.KeyPoints) < 2 {
			return fmt.Errorf("section %d (%q) has fewer than 2 key points", i, s.Title)
		}
	}

	return nil
}

// buildExpansionPrompt renders the outline into a human-readable prompt for the expander.
func buildExpansionPrompt(topic string, outline Outline) string {
	b := &strings.Builder{}
	fmt.Fprintf(b, "topic: %s\n\n", topic)
	fmt.Fprintf(b, "title: %s\n\n", outline.Title)
	fmt.Fprintf(b, "sections:\n")

	for i, s := range outline.Sections {
		fmt.Fprintf(b, "\n%d. %s\n", i+1, s.Title)

		for _, kp := range s.KeyPoints {
			fmt.Fprintf(b, "   - %s\n", kp)
		}
	}

	return b.String()
}

// extractJSONObject extracts the first {...} block from s.
func extractJSONObject(s string) string {
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start == -1 || end == -1 || end < start {
		return s
	}

	return s[start : end+1]
}
