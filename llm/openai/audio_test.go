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

package openai_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/henomis/phero/llm"
	"github.com/henomis/phero/llm/openai"
)

func TestTranscribe(t *testing.T) {
	var gotModel string
	var gotLanguage string
	var gotGranularities []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/transcriptions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("ParseMultipartForm: %v", err)
		}

		gotModel = r.MultipartForm.Value["model"][0]
		gotLanguage = r.MultipartForm.Value["language"][0]
		gotGranularities = append(gotGranularities, r.MultipartForm.Value["timestamp_granularities[]"]...)

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"task":     "transcribe",
			"language": "it",
			"duration": 1.25,
			"text":     "ciao mondo",
			"segments": []map[string]any{{
				"id": 1, "seek": 0, "start": 0.0, "end": 1.25,
				"text": "ciao mondo", "tokens": []int{1, 2}, "temperature": 0.0,
				"avg_logprob": -0.1, "compression_ratio": 1.0, "no_speech_prob": 0.01,
				"transient": false,
			}},
			"words": []map[string]any{{"word": "ciao", "start": 0.0, "end": 0.4}},
		}); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer srv.Close()

	c := openai.New("key", openai.WithBaseURL(srv.URL+"/v1"))
	result, err := c.Transcribe(context.Background(), llm.TranscriptionRequest{
		Input:    llm.AudioReader("sample.mp3", strings.NewReader("fake audio bytes")),
		Language: "it",
		Format:   llm.TranscriptionResponseFormatVerboseJSON,
		TimestampGranularities: []llm.TranscriptionTimestampGranularity{
			llm.TranscriptionTimestampGranularityWord,
		},
	})
	if err != nil {
		t.Fatalf("Transcribe: unexpected error: %v", err)
	}
	if gotModel != openai.DefaultTranscriptionModel {
		t.Fatalf("expected model %q, got %q", openai.DefaultTranscriptionModel, gotModel)
	}
	if gotLanguage != "it" {
		t.Fatalf("expected language %q, got %q", "it", gotLanguage)
	}
	if len(gotGranularities) != 1 || gotGranularities[0] != string(llm.TranscriptionTimestampGranularityWord) {
		t.Fatalf("unexpected timestamp granularities: %#v", gotGranularities)
	}
	if result.Text != "ciao mondo" {
		t.Fatalf("expected transcript %q, got %q", "ciao mondo", result.Text)
	}
	if len(result.Segments) != 1 || len(result.Words) != 1 {
		t.Fatalf("expected segment and word timing data, got segments=%d words=%d", len(result.Segments), len(result.Words))
	}
}

func TestTranscribe_InputRequired(t *testing.T) {
	c := openai.New("key")

	_, err := c.Transcribe(context.Background(), llm.TranscriptionRequest{})
	if !errors.Is(err, openai.ErrTranscriptionInputRequired) {
		t.Fatalf("expected ErrTranscriptionInputRequired, got %v", err)
	}
}

func TestSynthesizeSpeech(t *testing.T) {
	var gotPayload struct {
		Model          string  `json:"model"`
		Input          string  `json:"input"`
		Voice          string  `json:"voice"`
		Instructions   string  `json:"instructions"`
		ResponseFormat string  `json:"response_format"`
		Speed          float64 `json:"speed"`
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/speech" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}

		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("audio-bytes"))
	}))
	defer srv.Close()

	c := openai.New("key", openai.WithBaseURL(srv.URL+"/v1"))
	result, err := c.SynthesizeSpeech(context.Background(), llm.SpeechRequest{
		Input:        "ciao mondo",
		Instructions: "speak calmly",
		Speed:        1.1,
	})
	if err != nil {
		t.Fatalf("SynthesizeSpeech: unexpected error: %v", err)
	}
	if gotPayload.Model != openai.DefaultSpeechModel {
		t.Fatalf("expected model %q, got %q", openai.DefaultSpeechModel, gotPayload.Model)
	}
	if gotPayload.Voice != openai.DefaultSpeechVoice {
		t.Fatalf("expected voice %q, got %q", openai.DefaultSpeechVoice, gotPayload.Voice)
	}
	if gotPayload.ResponseFormat != string(llm.SpeechResponseFormatMP3) {
		t.Fatalf("expected default format %q, got %q", llm.SpeechResponseFormatMP3, gotPayload.ResponseFormat)
	}
	if string(result.Data) != "audio-bytes" {
		t.Fatalf("unexpected speech payload: %q", string(result.Data))
	}
	if result.MIMEType != "audio/mpeg" {
		t.Fatalf("expected MIME type %q, got %q", "audio/mpeg", result.MIMEType)
	}
	if result.Format != llm.SpeechResponseFormatMP3 {
		t.Fatalf("expected format %q, got %q", llm.SpeechResponseFormatMP3, result.Format)
	}
}

func TestSynthesizeSpeech_InputRequired(t *testing.T) {
	c := openai.New("key")

	_, err := c.SynthesizeSpeech(context.Background(), llm.SpeechRequest{})
	if !errors.Is(err, openai.ErrSpeechInputRequired) {
		t.Fatalf("expected ErrSpeechInputRequired, got %v", err)
	}
}
