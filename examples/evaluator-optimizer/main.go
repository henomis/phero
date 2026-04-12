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

// EvalResult is the structured output returned by the evaluator agent.
type EvalResult struct {
	Score    int    `json:"score"`
	Feedback string `json:"feedback"`
}

func main() {
	var topic string
	var threshold int
	var maxAttempts int
	var timeout time.Duration
	flag.StringVar(&topic, "topic", "Explain how large language models work to a general audience.", "Writing topic for the generator agent")
	flag.IntVar(&threshold, "threshold", 8, "Minimum score (0-10) required to accept the output")
	flag.IntVar(&maxAttempts, "max-attempts", 4, "Maximum number of generator-evaluator iterations")
	flag.DurationVar(&timeout, "timeout", 5*time.Minute, "Overall timeout for the run")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	llmClient, llmInfo := buildLLMFromEnv()

	generator, evaluator, err := buildAgents(llmClient)
	if err != nil {
		panic(err)
	}

	fmt.Println("multi-agent architecture example: evaluator-optimizer")
	fmt.Println("- llm:", llmInfo)
	fmt.Println("- topic:", topic)
	fmt.Println("- threshold:", threshold, "/ 10")
	fmt.Println()

	prompt := topic
	var lastDraft string

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		fmt.Printf("--- iteration %d / %d ---\n", attempt, maxAttempts)

		// Step 1: generate a draft.
		genOut, err := generator.Run(ctx, llm.Text(prompt))
		if err != nil {
			panic(fmt.Errorf("generator failed: %w", err))
		}
		lastDraft = strings.TrimSpace(genOut.TextContent())

		fmt.Printf("draft (%d chars):\n%s\n\n", len(lastDraft), lastDraft)

		// Step 2: evaluate the draft.
		evalPrompt := fmt.Sprintf(
			"Evaluate the following text on the topic %q.\n\nText:\n%s",
			topic, lastDraft,
		)
		evalOut, err := evaluator.Run(ctx, llm.Text(evalPrompt))
		if err != nil {
			panic(fmt.Errorf("evaluator failed: %w", err))
		}

		result, err := parseEvalResult(evalOut.TextContent())
		if err != nil {
			panic(fmt.Errorf("could not parse evaluator output: %w\nraw: %s", err, evalOut.TextContent()))
		}

		fmt.Printf("score: %d / 10\n", result.Score)
		fmt.Printf("feedback: %s\n\n", result.Feedback)

		if result.Score >= threshold {
			fmt.Printf("threshold reached on iteration %d.\n\n", attempt)
			break
		}

		if attempt == maxAttempts {
			fmt.Println("max attempts reached; using best draft.")
			break
		}

		// Build a revision prompt for the next attempt.
		prompt = fmt.Sprintf(
			"Improve your text on the topic %q based on the evaluator's feedback.\n\nFeedback: %s\n\nPrevious draft:\n%s",
			topic, result.Feedback, lastDraft,
		)
	}

	fmt.Println("=== final output ===")
	fmt.Println(lastDraft)
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

func buildAgents(llmClient llm.LLM) (generator, evaluator *agent.Agent, err error) {
	generator, err = agent.New(llmClient, "Generator Agent", strings.TrimSpace(`You are a skilled technical writer.

Your task: write a clear, accurate, and engaging explanation on the given topic.

Guidelines:
- Target a general (non-expert) audience.
- Use plain language; avoid jargon unless you explain it.
- Aim for 150-250 words.
- Structure: 1-2 short paragraphs. No bullet lists.
- Do not include a title or meta-commentary; output the explanation text only.`))
	if err != nil {
		return nil, nil, err
	}

	evaluator, err = agent.New(llmClient, "Evaluator Agent", strings.TrimSpace(`You are a strict writing evaluator.

You will receive a text on a given topic. Evaluate it and return ONLY valid JSON - no markdown, no extra text.

Evaluation criteria:
- Clarity (is it easy to understand?)
- Accuracy (is the content correct?)
- Engagement (is it interesting to read?)
- Appropriate length (150-250 words)

Output schema:
{
  "score": <integer 0-10>,
  "feedback": "<concrete, actionable suggestions for improvement>"
}

Score 0-4: poor, 5-7: acceptable, 8-10: good.
If score >= 8 feedback may be brief praise plus minor tips.`))
	if err != nil {
		return nil, nil, err
	}

	return generator, evaluator, nil
}

func parseEvalResult(raw string) (EvalResult, error) {
	cleaned := extractJSONObject(raw)

	var result EvalResult
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return EvalResult{}, err
	}

	return result, nil
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
