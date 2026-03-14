# Simple Agent Example

**The simplest possible Phero example** - perfect for getting started.

This example demonstrates the core building blocks:

1. Define a Go function with typed input/output structs
2. Convert it into a tool using `llm.NewFunctionTool`
3. Create an agent and add the tool
4. Run the agent with a user request

The example uses a simple calculator that performs basic arithmetic (add, subtract, multiply, divide).

## What you'll learn

- How to create custom function tools from Go functions
- How JSON Schema is automatically generated from Go struct tags
- How agents use tools to solve tasks
- The basic `agent.Run()` pattern

## Run

### Option A: Local Ollama (OpenAI-compatible)

```bash
export OPENAI_BASE_URL=http://localhost:11434/v1
export OPENAI_MODEL=gpt-oss:20b-cloud
# OPENAI_API_KEY can be empty for Ollama

go run ./examples/simple-agent
```

### Option B: OpenAI

```bash
export OPENAI_API_KEY=...your_key...
export OPENAI_MODEL=gpt-4o-mini

go run ./examples/simple-agent
```

## Expected output

The agent will use the calculator tool multiple times to solve the problem step-by-step:

```text
LLM: model=gpt-4o-mini base_url=
User: If I have 15 apples and give away 7, then buy 23 more, how many do I have?

Agent: You have 31 apples.
```

## How it works

1. The user asks a word problem requiring multiple calculations
2. The agent breaks down the problem and calls the `calculator` tool multiple times:
   - First: 15 - 7 = 8
   - Then: 8 + 23 = 31
3. The agent synthesizes the final answer from the tool results

## Customization

Try modifying the example:

- Change the user request to test different scenarios
- Add more operations (power, modulo, etc.)
- Add validation logic in the calculator function
- Create your own tool (e.g., `get_current_time`, `convert_units`)

## Code walkthrough

### 1. Define input/output types

```go
type CalculatorInput struct {
    Operation string  `json:"operation" jsonschema:"description=The operation to perform..."`
    A         float64 `json:"a" jsonschema:"description=The first number"`
    B         float64 `json:"b" jsonschema:"description=The second number"`
}
```

The `jsonschema` tags are used to generate the tool's JSON Schema, which tells the LLM how to use the tool.

### 2. Implement the tool logic

```go
func calculate(ctx context.Context, input *CalculatorInput) (*CalculatorOutput, error) {
    if input == nil {
        return &CalculatorOutput{Error: "missing input"}, nil
    }
    
    switch input.Operation {
    case "add":
        return &CalculatorOutput{Result: input.A + input.B}, nil
    // ... more operations
    }
}
```

Tool functions must have the signature: `func(context.Context, *InputType) (*OutputType, error)`

### 3. Create the tool

```go
calcTool, err := llm.NewFunctionTool(
    "calculator",
    "Performs basic arithmetic operations",
    calculate,
)
```

`NewFunctionTool` automatically:
- Generates JSON Schema from your types
- Handles JSON marshaling/unmarshaling
- Wraps your function for the agent

### 4. Create an agent and add the tool

```go
a, err := agent.New(llmClient, "Math Assistant", "You are a helpful math assistant...")
a.AddTool(calcTool)
```

### 5. Run it

```go
response, err := a.Run(ctx, "If I have 15 apples...")
```

The agent will automatically:
- Call the LLM with your request
- Detect when tools need to be called
- Execute tool calls
- Feed results back to the LLM
- Repeat until done

## Next steps

After mastering this example, explore:

- [Multi-Agent Workflow](../multi-agent-workflow/) - Multiple agents working together
- [Skills](../skills/) - Reusable capabilities defined in SKILL.md files
- [RAG Chatbot](../rag-chatbot/) - Semantic search over documents
