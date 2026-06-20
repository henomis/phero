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

package nats

import (
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/henomis/phero/llm"
)

const mimeJPEG = "image/jpeg"

// imageExtensions maps lowercase file extensions to MIME types for attachment handling.
var imageExtensions = map[string]string{
	".png":  "image/png",
	".jpg":  mimeJPEG,
	".jpeg": mimeJPEG,
	".gif":  "image/gif",
	".webp": "image/webp",
}

// envelopeToContentParts converts a decoded envelope into phero ContentParts.
//
// The prompt text becomes the first part. Each attachment is translated based
// on its file extension: recognised image extensions become llm.ImageBase64
// parts; everything else is passed as a descriptive text part.
func envelopeToContentParts(env *envelope) ([]llm.ContentPart, error) {
	parts := make([]llm.ContentPart, 0, 1+len(env.Attachments))
	parts = append(parts, llm.Text(env.Prompt))

	for _, a := range env.Attachments {
		// Validate base64 encoding (§5.2 requires RFC 4648 §4 standard-alphabet padded base64).
		if _, err := base64.StdEncoding.DecodeString(a.Content); err != nil {
			return nil, fmt.Errorf("%w: attachment %q: invalid base64: %v", ErrMalformedEnvelope, a.Filename, err)
		}

		ext := strings.ToLower(filepath.Ext(a.Filename))
		if mimeType, ok := imageExtensions[ext]; ok {
			parts = append(parts, llm.ImageBase64(mimeType, a.Content))
		} else {
			parts = append(parts, llm.Text("[attachment: "+a.Filename+"]"))
		}
	}

	return parts, nil
}
