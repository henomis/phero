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
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/henomis/phero/agent"
	"github.com/henomis/phero/llm"
	"github.com/henomis/phero/llm/openai"
	simplemem "github.com/henomis/phero/memory/simple"
)

func main() {
	ctx := context.Background()
	llmClient, llmInfo := buildLLMFromEnv()

	// sharedMemory is given to every agent so the full conversation context
	// is available to the specialist after the triage agent hands off.
	sharedMemory := simplemem.New(50)

	billingAgent, err := agent.New(
		llmClient,
		"Billing Agent",
		"You are a billing specialist for a SaaS company. "+
			"You handle payment issues, subscription changes, refunds, and invoice questions. "+
			"Greet the customer by acknowledging the issue from the conversation history and resolve it. "+
			"If the issue is technical, hand off to the Technical Support Agent. "+
			"If you are unsure how to categorise the request, hand off back to the Triage Agent.",
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent.New billing: %v\n", err)
		os.Exit(1)
	}
	billingAgent.SetMemory(sharedMemory)

	technicalAgent, err := agent.New(
		llmClient,
		"Technical Support Agent",
		"You are a technical support engineer for a SaaS company. "+
			"You handle bugs, outages, integration issues, and product how-to questions. "+
			"Greet the customer by acknowledging the issue from the conversation history and resolve it. "+
			"If the issue is a billing or payment matter, hand off to the Billing Agent. "+
			"If you are unsure how to categorise the request, hand off back to the Triage Agent.",
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent.New technical: %v\n", err)
		os.Exit(1)
	}
	technicalAgent.SetMemory(sharedMemory)

	triageAgent, err := agent.New(
		llmClient,
		"Triage Agent",
		"You are a customer-service triage agent for a SaaS company. "+
			"Analyze the customer's request and hand off to the correct specialist: "+
			"use the billing agent for payment, subscription, or invoice issues, and "+
			"use the technical support agent for bugs, outages, or product how-to questions. "+
			"Do NOT answer the question yourself — always hand off.",
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent.New triage: %v\n", err)
		os.Exit(1)
	}
	triageAgent.SetMemory(sharedMemory)

	if err := triageAgent.AddHandoff(billingAgent); err != nil {
		fmt.Fprintf(os.Stderr, "AddHandoff billing: %v\n", err)
		os.Exit(1)
	}
	if err := triageAgent.AddHandoff(technicalAgent); err != nil {
		fmt.Fprintf(os.Stderr, "AddHandoff technical: %v\n", err)
		os.Exit(1)
	}

	// Cross-handoffs: every agent can route to the other two.
	if err := billingAgent.AddHandoff(technicalAgent); err != nil {
		fmt.Fprintf(os.Stderr, "AddHandoff billing→technical: %v\n", err)
		os.Exit(1)
	}
	if err := billingAgent.AddHandoff(triageAgent); err != nil {
		fmt.Fprintf(os.Stderr, "AddHandoff billing→triage: %v\n", err)
		os.Exit(1)
	}
	if err := technicalAgent.AddHandoff(billingAgent); err != nil {
		fmt.Fprintf(os.Stderr, "AddHandoff technical→billing: %v\n", err)
		os.Exit(1)
	}
	if err := technicalAgent.AddHandoff(triageAgent); err != nil {
		fmt.Fprintf(os.Stderr, "AddHandoff technical→triage: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Handoff Chatbot")
	fmt.Println("───────────────────────────────────────")
	fmt.Printf("LLM: %s\n", llmInfo)
	fmt.Println()
	fmt.Println("Commands: /clear  /exit")
	fmt.Println("Every message is triaged and routed to the right specialist.")
	fmt.Println("───────────────────────────────────────")
	fmt.Print("\n> ")

	const maxHandoffs = 10

	// currentAgent tracks the active specialist across turns.
	// Nil means no specialist has been assigned yet — triage will route the first message.
	var currentAgent *agent.Agent

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 512*1024)

	for scanner.Scan() {
		userInput := strings.TrimSpace(scanner.Text())

		if userInput == "" {
			fmt.Print("> ")
			continue
		}

		switch userInput {
		case "/exit":
			fmt.Println("Goodbye!")
			os.Exit(0)
		case "/clear":
			if err := sharedMemory.Clear(ctx); err != nil {
				fmt.Fprintf(os.Stderr, "clear: %v\n", err)
			} else {
				currentAgent = nil
				fmt.Println("Memory cleared.")
			}
			fmt.Print("\n> ")
			continue
		}

		// Each user turn starts at the triage agent.
		// Start from triage only when no specialist is active yet.
		routingAgent := currentAgent
		if routingAgent == nil {
			routingAgent = triageAgent
		}
		currentInput := userInput

		for handoffs := 0; ; handoffs++ {
			if handoffs > maxHandoffs {
				fmt.Fprintf(os.Stderr, "error: exceeded maximum handoff depth (%d)\n", maxHandoffs)
				break
			}

			result, err := routingAgent.Run(ctx, llm.Text(currentInput))
			if err != nil {
				fmt.Fprintf(os.Stderr, "agent.Run (%s): %v\n", routingAgent.Name(), err)
				break
			}

			if result.HandoffAgent != nil {
				fmt.Printf("[handoff] %s → %s\n", routingAgent.Name(), result.HandoffAgent.Name())
				routingAgent = result.HandoffAgent
				// Empty input: the specialist reads context from shared memory.
				currentInput = ""
				continue
			}

			currentAgent = routingAgent
			fmt.Printf("\n%s: %s\n", currentAgent.Name(), strings.TrimSpace(result.TextContent()))
			break
		}

		fmt.Print("\n> ")
	}
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
