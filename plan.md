# Phero blog series plan

## Goal

Create a blog series that helps Phero get adoption, stars, and mindshare among Go developers and AI builders.

The series should do three things at the same time:

1. Make the project feel technically serious.
2. Make the code feel approachable.
3. Give readers a clear next step after every article.

## Audience

Primary audience:

1. Go developers curious about AI agents.
2. Engineers who want practical agent systems, not abstract theory.
3. People comparing frameworks and deciding what to try.

Secondary audience:

1. Builders exploring MCP, A2A, RAG, and multi agent patterns.
2. People who may contribute examples, docs, or code.

## Positioning

Phero should be presented as:

1. A modern Go framework for multi agent systems.
2. Composable, explicit, and production friendly.
3. Centered on tools, memory, handoffs, tracing, and interoperability.

Avoid presenting it as a vague AI abstraction layer. The strongest positioning is concrete: real Go functions become tools, agents can hand work to each other, memory is explicit, tracing is readable, and integrations are practical.

## Tone and style

Use this voice consistently:

1. Technical and opinionated.
2. Clear and grounded in code.
3. Warm, but not hype heavy.
4. Written by someone who has actually built systems.

## Style constraints

These came from the editing of article1 and should be preserved:

1. No Markdown separator lines using `---`.
2. Use sentence case in headings.
3. Avoid dash heavy phrasing that feels machine written.
4. Prefer short, direct paragraphs over inflated transitions.
5. Keep examples grounded in the actual repo.
6. Do not repeat the same example across consecutive articles.

## Core messaging pillars

Every article does not need all of them, but the series should reinforce them repeatedly:

1. Go is a strong fit for agent runtimes.
2. Phero keeps the core loop small and understandable.
3. Tools are ordinary Go code, not a separate world.
4. Multi agent patterns should feel composable, not magical.
5. Interoperability matters: MCP, A2A, vector stores, provider backends.
6. Observability matters: tracing and run summaries make systems debuggable.

## Narrative arc for the series

The sequence should move from broad framing to increasingly differentiated capabilities.

1. Article 1: establish why the project matters.
2. Article 2: show a concrete capability that feels useful and slightly surprising.
3. Article 3 to 5: expand into multi agent patterns and knowledge workflows.
4. Later articles: show ecosystem leverage and unique differentiators.

## Published article status

### Article 1

File: `article1.md`

Current title:

`Go is the right language for production AI agents`

Purpose:

1. Introduce the case for Go as a runtime for AI agents.
2. Introduce Phero at a high level.
3. Show the project philosophy and core packages.
4. End with a broad teaser for the next post.

Important notes:

1. It currently includes the simple calculator agent example.
2. That means article2 must not reuse the same calculator walkthrough.
3. It already covers typed tools, JSON Schema generation, tracing, and the simple `agent.Run` loop at a high level.

## Series plan by article

### Article 1

Title:

`Go is the right language for production AI agents`

Angle:

Broad framing piece. Sell the need for a Go first framework and introduce Phero's philosophy.

Primary repo sources:

1. `README.md`
2. `examples/simple-agent`
3. `trace/text`

### Article 2

Recommended title:

`Build a support agent that routes itself`

Angle:

Show agent handoffs through a customer support example. This is practical, immediately understandable, and distinct from article1.

Primary repo sources:

1. `examples/handoff/main.go`
2. `examples/handoff/README.md`
3. `agent/agent.go` for `AddHandoff` and `Result.HandoffAgent`
4. `memory/simple`

Main teaching points:

1. A triage agent routes to specialists.
2. Specialists share memory so context survives the handoff.
3. The application loop handles `result.HandoffAgent` explicitly.
4. This is a clean pattern for support bots and internal assistants.

What not to repeat from article1:

1. Do not center the article on the calculator example.
2. Do not re explain basic `llm.NewTool` usage as the main story.
3. Do not spend much time re selling Go itself.

### Article 3

Recommended title:

`Three multi agent patterns you can actually use`

Angle:

Compare debate committee, workflow pipeline, and supervisor blackboard patterns.

Primary repo sources:

1. `examples/debate-committee`
2. `examples/multi-agent-workflow`
3. `examples/supervisor-blackboard`

Main teaching points:

1. Different coordination patterns solve different problems.
2. Parallel debate is good for synthesis and critique.
3. Sequential workflow is good for deterministic steps.
4. Supervisor plus blackboard is good for shared state and coordination.

### Article 4

Recommended title:

`Build a RAG chatbot in Go with Phero and Qdrant`

Angle:

High intent search topic. Show how retrieval works end to end.

Primary repo sources:

1. `examples/rag-chatbot`
2. `rag/`
3. `vectorstore/qdrant`
4. `embedding/openai`
5. `textsplitter/recursive`

Main teaching points:

1. Chunking, embedding, storage, retrieval.
2. Keep the explanation operational, not academic.
3. Show how conversational behavior sits on top of retrieval.

### Article 5

Recommended title:

`How MCP gives your Go agents real leverage`

Angle:

Use current MCP interest to show ecosystem reach.

Primary repo sources:

1. `examples/mcp`
2. `mcp/`

Main teaching points:

1. Phero can turn MCP server tools into agent tools.
2. This makes the framework instantly more useful.
3. Interoperability is more valuable than inventing yet another tool DSL.

### Article 6

Recommended title:

`Expose your Go agent as a service with A2A`

Angle:

Show distributed agent interoperability.

Primary repo sources:

1. `examples/a2a/server`
2. `examples/a2a/client`
3. `a2a/`

Main teaching points:

1. An agent can be exposed as an HTTP service.
2. Another agent can consume it as a tool.
3. This makes agent systems composable across process boundaries.

### Article 7

Recommended title:

`Why skills in Markdown are more powerful than they look`

Angle:

Highlight a distinctive feature that is easy to explain.

Primary repo sources:

1. `examples/skills`
2. `skill/`

Main teaching points:

1. Skills move capability definition closer to documentation.
2. They create a clean seam between prompt like behavior and Go code.
3. They lower the barrier for extending agent behavior.

## Workflow for writing a new article

When another AI agent takes over, it should follow this workflow:

1. Read `README.md`.
2. Read the specific example folder used by the article.
3. Read the corresponding source file, not just the README.
4. Read the previous article to avoid repetition.
5. Identify what the reader should learn in one sentence.
6. Choose one main example, not two or three.
7. Make the article advance the story of the series.

## Recommended article structure

This is the default structure unless the article clearly needs a different one:

1. Short hook tied to a real engineering problem.
2. Brief statement of what the article will build or explain.
3. Walk through the example from the repo.
4. Explain what Phero is doing under the hood.
5. Explain why the pattern matters in real systems.
6. End with a broad teaser for the next article.
7. End with a GitHub star call to action.

## Rules to avoid weak articles

1. Do not repeat the same code example in adjacent articles.
2. Do not re explain the whole framework in every article.
3. Do not turn the post into a reference manual.
4. Do not invent features that are not in the repo.
5. Do not use generic AI buzzwords when a concrete mechanism exists.
6. Do not overload one post with too many concepts.
7. Do not write teaser sections with too much detail.

## What each article should include

Every article should contain:

1. One runnable path tied to an existing example.
2. At least one reason the feature matters in practice.
3. A clear explanation of the control flow.
4. A simple closing that points to the next article.
5. A CTA to star the repo or try an example.

## What each article should avoid

1. Repeating the exact same opening thesis from article1.
2. Copying code blocks that the previous article already used heavily.
3. Getting stuck in provider setup details.
4. Overusing the ant metaphor.

## Notes for article2 specifically

Article2 should now be about handoffs, not the simple calculator agent.

Reason:

1. Article1 already included a substantial simple agent walkthrough.
2. A second article on the same example feels repetitive.
3. Handoffs are a stronger hook for a follow up because they show a visibly more capable system.

Recommended article2 structure:

1. Open with the problem of support bots trying to answer everything themselves.
2. Introduce triage plus specialists.
3. Show the architecture from the handoff example.
4. Explain shared memory and why it matters.
5. Explain `AddHandoff` and `Result.HandoffAgent`.
6. Show the application loop that follows the handoff chain.
7. Close by pointing toward broader multi agent patterns.

## Reusable CTA patterns

Use one, not all, at the end of an article:

1. If you want to try this yourself, start with the example in the repo.
2. If this clarified how Phero works, star the repo.
3. If you are exploring Go for agents, browse the examples and pick the closest pattern.

## Repository sources worth revisiting often

1. `README.md`
2. `examples/README.md`
3. `examples/simple-agent`
4. `examples/handoff`
5. `examples/multi-agent-workflow`
6. `examples/debate-committee`
7. `examples/supervisor-blackboard`
8. `examples/rag-chatbot`
9. `examples/mcp`
10. `examples/a2a`
11. `examples/skills`
12. `examples/tracing`
