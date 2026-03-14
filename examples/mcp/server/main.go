package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Quote API response structure.
type QuoteResponse struct {
	Content string `json:"content"`
	Author  string `json:"author"`
}

// Input structure for the tool.
type Input struct{}

// Output structure for the tool.
type Output struct {
	Content string `json:"content"`
}

func main() {
	// Create a server with a single tool.
	server := mcp.NewServer(&mcp.Implementation{Name: "random_quote", Version: "v1.0.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "get_random_quote", Description: "Fetches a random inspirational quote"}, getRandomQuoteHandler)
	// Run the server over stdin/stdout, until the client disconnects.
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}

func getRandomQuoteHandler(ctx context.Context, _ *mcp.CallToolRequest, _ *Input) (
	*mcp.CallToolResult,
	Output,
	error,
) {
	httpReq, err := http.NewRequestWithContext(ctx, "GET", "https://zenquotes.io/api/random", http.NoBody)
	if err != nil {
		return nil, Output{}, err
	}

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, Output{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, Output{}, err
	}

	var data []struct {
		Q string `json:"q"`
		A string `json:"a"`
	}
	err = json.Unmarshal(body, &data)
	if err != nil || len(data) == 0 {
		return nil, Output{}, fmt.Errorf("invalid response from quote API")
	}
	return nil, Output{Content: fmt.Sprintf("%s - %s", data[0].Q, data[0].A)}, nil
}
