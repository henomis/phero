package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Quote API response structure.
type QuoteResponse struct {
	Content string `json:"content"`
	Author  string `json:"author"`
}

func main() {
	httpReq, err := http.NewRequestWithContext(context.Background(), "GET", "https://zenquotes.io/api/random", http.NoBody)
	if err != nil {
		fmt.Println("Error creating request:", err)
		return
	}

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		fmt.Println("Error making request:", err)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Error reading response body:", err)
		return
	}

	var data []struct {
		Q string `json:"q"`
		A string `json:"a"`
	}
	err = json.Unmarshal(body, &data)
	if err != nil || len(data) == 0 {
		fmt.Println("Invalid response from quote API")
		return
	}
	fmt.Printf("%s - %s\n", data[0].Q, data[0].A)
}
