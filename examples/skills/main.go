package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/henomis/phero/agent"
	"github.com/henomis/phero/llm"
	"github.com/henomis/phero/llm/openai"
	"github.com/henomis/phero/skill"
	"github.com/henomis/phero/tool/file"
)

type Options[I, O any] struct {
	ClientName    string
	ClientVersion string
	Command       string
	Args          []string
	Toolname      string
	Input         *I
	Output        *O
}

func main() {
	llmClient, llmInfo := buildLLMFromEnv()
	ctx := context.Background()

	skillParser := skill.New("./skills")
	list, err := skillParser.List()
	if err != nil {
		panic(err)
	}

	tools := make([]*llm.Tool, 0, len(list))
	for _, skillName := range list {
		skill, err := skillParser.Parse(skillName)
		if err != nil {
			panic(err)
		}

		skillAsTool, err := skill.AsTool(llmClient)
		if err != nil {
			panic(err)
		}
		tools = append(tools, skillAsTool)
	}

	a, err := agent.New(llmClient, "Agent", "An agent that helps create web pages and fetch random quotes")
	if err != nil {
		panic(err)
	}

	createFileTool, err := file.NewCreateFileTool()
	if err != nil {
		panic(err)
	}
	tools = append(tools, createFileTool.Tool().Use(func(_ *llm.Tool, next llm.ToolHandler) llm.ToolHandler {
		return func(ctx context.Context, arguments string) (any, error) {
			var input *file.CreateFileInput
			if err := json.Unmarshal([]byte(arguments), &input); err != nil {
				return nil, &llm.ToolArgumentParseError{Err: err}
			}
			if err := writeValidationFunc(ctx, input); err != nil {
				return nil, err
			}
			return next(ctx, arguments)
		}
	}))

	for _, tool := range tools {
		if err := a.AddTool(tool); err != nil {
			panic(err)
		}
	}

	res, err := a.Run(ctx, "create a web page containing a random quote, and save the html to a file called quote.html")
	if err != nil {
		panic(err)
	}

	fmt.Printf("LLM used: %s\n", llmInfo)
	fmt.Printf("Agent response: %s\n", res)
}

func buildLLMFromEnv() (llm.LLM, string) {
	apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	baseURL := strings.TrimSpace(os.Getenv("OPENAI_BASE_URL"))
	model := strings.TrimSpace(os.Getenv("OPENAI_MODEL"))

	// If no key and no base URL are set, assume a local OpenAI-compatible server (e.g. Ollama).
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

func writeValidationFunc(_ context.Context, input *file.CreateFileInput) error {
	fmt.Printf("Do you want to write to the file '%s'? (y/N): ", input.Path)
	var permission string
	_, scanErr := fmt.Scanln(&permission)
	if scanErr != nil {
		return fmt.Errorf("failed to read user input: %w", scanErr)
	}

	if strings.EqualFold(permission, "y") {
		return nil
	}

	return fmt.Errorf("user permission denied")
}
