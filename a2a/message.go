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

package a2a

import (
	"encoding/base64"
	"encoding/json"
	"strings"

	sdka2a "github.com/a2aproject/a2a-go/v2/a2a"

	"github.com/henomis/phero/agent"
	"github.com/henomis/phero/llm"
)

// translatePartsToPhero converts an A2A message into phero ContentParts.
//
// Translation rules:
//   - Text → llm.Text
//   - URL with image/* MediaType → llm.ImageURL
//   - Raw with image/* MediaType → llm.ImageBase64 (base64-encoded)
//   - Data → JSON-encoded llm.Text (best-effort)
//   - URL parts without an image MediaType are skipped
func translatePartsToPhero(msg *sdka2a.Message) []llm.ContentPart {
	if msg == nil {
		return nil
	}

	parts := make([]llm.ContentPart, 0, len(msg.Parts))
	for _, part := range msg.Parts {
		switch {
		case part.Text() != "":
			parts = append(parts, llm.Text(part.Text()))
		case string(part.URL()) != "":
			if strings.HasPrefix(part.MediaType, "image/") {
				parts = append(parts, llm.ImageURL(string(part.URL())))
			}
		case len(part.Raw()) > 0:
			if strings.HasPrefix(part.MediaType, "image/") {
				encoded := base64.StdEncoding.EncodeToString(part.Raw())
				parts = append(parts, llm.ImageBase64(part.MediaType, encoded))
			}
		default:
			if data := part.Data(); data != nil {
				b, err := json.Marshal(data)
				if err == nil && len(b) > 0 {
					parts = append(parts, llm.Text(string(b)))
				}
			}
		}
	}

	return parts
}

// translateResultToA2A converts phero agent ContentParts into A2A Parts.
//
// Translation rules:
//   - llm.ContentTypeText → sdka2a.NewTextPart
//   - llm.ContentTypeImageURL → sdka2a.NewFileURLPart with "image/*" media type
//   - llm.ContentTypeImageBase64 → sdka2a.NewRawPart (decoded) with the original MediaType
func translateResultToA2A(result *agent.Result) []*sdka2a.Part {
	if result == nil {
		return nil
	}

	parts := make([]*sdka2a.Part, 0, len(result.Parts))
	for _, p := range result.Parts {
		switch p.Type {
		case llm.ContentTypeText:
			parts = append(parts, sdka2a.NewTextPart(p.Text))
		case llm.ContentTypeImageURL:
			// llm.ContentPart carries no MIME type for URL images; "image/*" is the best approximation.
			parts = append(parts, sdka2a.NewFileURLPart(sdka2a.URL(p.ImageURL), "image/*"))
		case llm.ContentTypeImageBase64:
			raw, err := base64.StdEncoding.DecodeString(p.ImageBase64)
			if err == nil {
				a2aPart := sdka2a.NewRawPart(raw)
				a2aPart.MediaType = p.MIMEType
				parts = append(parts, a2aPart)
			}
		}
	}

	return parts
}

// extractTextFromResult extracts the first text content from a SendMessageResult.
//
// A result is either a *sdka2a.Message (agent responds inline) or a *sdka2a.Task
// (server creates a task to track the work). Both are handled. Artifacts are also
// checked if neither the status message nor inline parts contain text.
func extractTextFromResult(result sdka2a.SendMessageResult) (string, error) {
	switch v := result.(type) {
	case *sdka2a.Message:
		for _, part := range v.Parts {
			if t := part.Text(); t != "" {
				return t, nil
			}
		}

		return "", ErrNoTextContent

	case *sdka2a.Task:
		if v.Status.Message != nil {
			for _, part := range v.Status.Message.Parts {
				if t := part.Text(); t != "" {
					return t, nil
				}
			}
		}

		for _, artifact := range v.Artifacts {
			for _, part := range artifact.Parts {
				if t := part.Text(); t != "" {
					return t, nil
				}
			}
		}

		return "", ErrNoTextContent

	default:
		return "", ErrNoTextContent
	}
}
