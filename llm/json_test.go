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

package llm_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/henomis/phero/v2/llm"
)

// legacyMessages is []llm.Message as encoded before the types had JSON tags
// (Go field names), as stored by existing memory backends.
const legacyMessages = `[
	{"Role":"user","Parts":[{"Type":"image_base64","Text":"","ImageURL":"","ImageBase64":"aGk=","MIMEType":"image/png","Signature":""}],
	 "ToolCalls":null,"ToolCallID":"","ToolError":false,"Name":""},
	{"Role":"assistant","Parts":null,"ToolCalls":[{"ID":"c1","Type":"function","Function":{"Name":"f","Arguments":"{}"}}],
	 "ToolCallID":"","ToolError":false,"Name":""},
	{"Role":"tool","Parts":[{"Type":"text","Text":"boom","ImageURL":"","ImageBase64":"","MIMEType":"","Signature":""}],
	 "ToolCalls":null,"ToolCallID":"c1","ToolError":true,"Name":""}
]`

func TestMessageJSON_DecodesLegacyFieldNames(t *testing.T) {
	var got []llm.Message
	if err := json.Unmarshal([]byte(legacyMessages), &got); err != nil {
		t.Fatal(err)
	}

	want := []llm.Message{
		{Role: llm.RoleUser, Parts: []llm.ContentPart{{Type: llm.ContentTypeImageBase64, ImageBase64: "aGk=", MIMEType: "image/png"}}},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "c1", Type: "function", Function: llm.FunctionCall{Name: "f", Arguments: "{}"}}}},
		{Role: llm.RoleTool, Parts: []llm.ContentPart{llm.Text("boom")}, ToolCallID: "c1", ToolError: true},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("legacy decode mismatch:\n got %+v\nwant %+v", got, want)
	}
}

func TestMessageJSON_RoundTripCamelCase(t *testing.T) {
	in := []llm.Message{
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "c1", Type: "function", Function: llm.FunctionCall{Name: "f", Arguments: "{}"}}}},
		{Role: llm.RoleTool, Parts: []llm.ContentPart{llm.Text("ok")}, ToolCallID: "c1", ToolError: true},
	}

	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}

	for _, key := range []string{`"toolCalls"`, `"toolCallId"`, `"toolError"`, `"role"`} {
		if !strings.Contains(string(b), key) {
			t.Errorf("expected key %s in %s", key, b)
		}
	}

	var out []llm.Message
	if err = json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round trip mismatch: %+v", out)
	}
}

func TestResultJSON_DecodesLegacyFieldNames(t *testing.T) {
	const legacy = `{"Message":{"Role":"assistant","Parts":[{"Type":"text","Text":"hi"}]},
		"Usage":{"InputTokens":3,"OutputTokens":2,"CacheReadTokens":1,"CacheWriteTokens":0},"Model":"m"}`

	var r llm.Result
	if err := json.Unmarshal([]byte(legacy), &r); err != nil {
		t.Fatal(err)
	}

	if r.Message.TextContent() != "hi" || r.Usage.CacheReadTokens != 1 || r.Model != "m" {
		t.Fatalf("legacy result decode mismatch: %+v %+v", r, r.Usage)
	}
}
