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

package a2a_test

import (
	"encoding/base64"
	"errors"
	"testing"

	sdka2a "github.com/a2aproject/a2a-go/v2/a2a"

	pheroA2A "github.com/henomis/phero/a2a"
	"github.com/henomis/phero/agent"
	"github.com/henomis/phero/llm"
)

// --- result builders --------------------------------------------------------

func agentTextResult(text string) *agent.Result {
	return pheroA2A.MakeAgentResult([]llm.ContentPart{llm.Text(text)})
}

func agentImageURLResult(url string) *agent.Result {
	return pheroA2A.MakeAgentResult([]llm.ContentPart{llm.ImageURL(url)})
}

func agentImageBase64Result(mediaType, b64 string) *agent.Result {
	return pheroA2A.MakeAgentResult([]llm.ContentPart{llm.ImageBase64(mediaType, b64)})
}

// --- translatePartsToPhero --------------------------------------------------

func TestTranslatePartsToPhero_Text(t *testing.T) {
	msg := sdka2a.NewMessage(sdka2a.MessageRoleUser, sdka2a.NewTextPart("hello"))

	parts := pheroA2A.TranslatePartsToPhero(msg)
	if len(parts) != 1 {
		t.Fatalf("want 1 part, got %d", len(parts))
	}

	if parts[0].Type != llm.ContentTypeText {
		t.Errorf("want ContentTypeText, got %v", parts[0].Type)
	}

	if parts[0].Text != "hello" {
		t.Errorf("want %q, got %q", "hello", parts[0].Text)
	}
}

func TestTranslatePartsToPhero_ImageURL(t *testing.T) {
	part := sdka2a.NewFileURLPart("http://example.com/img.png", "image/png")
	msg := sdka2a.NewMessage(sdka2a.MessageRoleUser, part)

	parts := pheroA2A.TranslatePartsToPhero(msg)
	if len(parts) != 1 {
		t.Fatalf("want 1 part, got %d", len(parts))
	}

	if parts[0].Type != llm.ContentTypeImageURL {
		t.Errorf("want ContentTypeImageURL, got %v", parts[0].Type)
	}

	if parts[0].ImageURL != "http://example.com/img.png" {
		t.Errorf("unexpected URL: %s", parts[0].ImageURL)
	}
}

func TestTranslatePartsToPhero_NonImageURL_Skipped(t *testing.T) {
	part := sdka2a.NewFileURLPart("http://example.com/doc.pdf", "application/pdf")
	msg := sdka2a.NewMessage(sdka2a.MessageRoleUser, part)

	parts := pheroA2A.TranslatePartsToPhero(msg)
	if len(parts) != 0 {
		t.Errorf("non-image URL parts should be skipped, got %d parts", len(parts))
	}
}

func TestTranslatePartsToPhero_ImageBase64(t *testing.T) {
	raw := []byte{0xff, 0xd8, 0xff} // JPEG magic bytes
	part := sdka2a.NewRawPart(raw)
	part.MediaType = "image/jpeg"
	msg := sdka2a.NewMessage(sdka2a.MessageRoleUser, part)

	parts := pheroA2A.TranslatePartsToPhero(msg)
	if len(parts) != 1 {
		t.Fatalf("want 1 part, got %d", len(parts))
	}

	if parts[0].Type != llm.ContentTypeImageBase64 {
		t.Errorf("want ContentTypeImageBase64, got %v", parts[0].Type)
	}
}

func TestTranslatePartsToPhero_NilMessage(t *testing.T) {
	parts := pheroA2A.TranslatePartsToPhero(nil)
	if parts != nil {
		t.Errorf("nil message should return nil, got %v", parts)
	}
}

// --- translateResultToA2A ---------------------------------------------------

func TestTranslateResultToA2A_Text(t *testing.T) {
	result := agentTextResult("response")

	parts := pheroA2A.TranslateResultToA2A(result)
	if len(parts) != 1 {
		t.Fatalf("want 1 part, got %d", len(parts))
	}

	if parts[0].Text() != "response" {
		t.Errorf("want %q, got %q", "response", parts[0].Text())
	}
}

func TestTranslateResultToA2A_ImageURL(t *testing.T) {
	result := agentImageURLResult("http://example.com/img.png")

	parts := pheroA2A.TranslateResultToA2A(result)
	if len(parts) != 1 {
		t.Fatalf("want 1 part, got %d", len(parts))
	}

	if string(parts[0].URL()) != "http://example.com/img.png" {
		t.Errorf("unexpected URL: %s", parts[0].URL())
	}
}

func TestTranslateResultToA2A_ImageBase64_Valid(t *testing.T) {
	raw := []byte{0x89, 0x50, 0x4e, 0x47} // PNG magic
	encoded := base64.StdEncoding.EncodeToString(raw)
	result := agentImageBase64Result("image/png", encoded)

	parts := pheroA2A.TranslateResultToA2A(result)
	if len(parts) != 1 {
		t.Fatalf("want 1 part, got %d", len(parts))
	}

	if parts[0].MediaType != "image/png" {
		t.Errorf("want media type %q, got %q", "image/png", parts[0].MediaType)
	}
}

func TestTranslateResultToA2A_ImageBase64_Invalid_Skipped(t *testing.T) {
	result := agentImageBase64Result("image/png", "not-valid-base64!!!")

	parts := pheroA2A.TranslateResultToA2A(result)
	if len(parts) != 0 {
		t.Errorf("invalid base64 should be skipped, got %d parts", len(parts))
	}
}

// --- extractTextFromResult --------------------------------------------------

func TestExtractTextFromResult_Message(t *testing.T) {
	msg := sdka2a.NewMessage(sdka2a.MessageRoleAgent, sdka2a.NewTextPart("agent reply"))

	text, err := pheroA2A.ExtractTextFromResult(msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if text != "agent reply" {
		t.Errorf("want %q, got %q", "agent reply", text)
	}
}

func TestExtractTextFromResult_Message_NoText(t *testing.T) {
	raw := []byte{0x01}
	part := sdka2a.NewRawPart(raw)
	msg := sdka2a.NewMessage(sdka2a.MessageRoleAgent, part)

	_, err := pheroA2A.ExtractTextFromResult(msg)
	if !errors.Is(err, pheroA2A.ErrNoTextContent) {
		t.Errorf("want ErrNoTextContent, got %v", err)
	}
}

func TestExtractTextFromResult_Task_StatusMessage(t *testing.T) {
	statusMsg := sdka2a.NewMessage(sdka2a.MessageRoleAgent, sdka2a.NewTextPart("done"))
	task := &sdka2a.Task{
		ID:        "task-1",
		ContextID: "ctx-1",
		Status:    sdka2a.TaskStatus{State: sdka2a.TaskStateCompleted, Message: statusMsg},
	}

	text, err := pheroA2A.ExtractTextFromResult(task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if text != "done" {
		t.Errorf("want %q, got %q", "done", text)
	}
}

func TestExtractTextFromResult_Task_Artifact(t *testing.T) {
	artifact := &sdka2a.Artifact{
		ID:    "art-1",
		Parts: sdka2a.ContentParts{sdka2a.NewTextPart("artifact content")},
	}
	task := &sdka2a.Task{
		ID:        "task-2",
		ContextID: "ctx-2",
		Status:    sdka2a.TaskStatus{State: sdka2a.TaskStateCompleted},
		Artifacts: []*sdka2a.Artifact{artifact},
	}

	text, err := pheroA2A.ExtractTextFromResult(task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if text != "artifact content" {
		t.Errorf("want %q, got %q", "artifact content", text)
	}
}

func TestExtractTextFromResult_Task_NoText(t *testing.T) {
	task := &sdka2a.Task{
		ID:        "task-3",
		ContextID: "ctx-3",
		Status:    sdka2a.TaskStatus{State: sdka2a.TaskStateFailed},
	}

	_, err := pheroA2A.ExtractTextFromResult(task)
	if !errors.Is(err, pheroA2A.ErrNoTextContent) {
		t.Errorf("want ErrNoTextContent, got %v", err)
	}
}
