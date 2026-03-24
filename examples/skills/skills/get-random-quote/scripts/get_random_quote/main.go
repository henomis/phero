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
	defer func() {
		_ = resp.Body.Close()
	}()

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
