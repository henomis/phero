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

package openai

import (
	"context"
	"io"
	"strings"

	openaiapi "github.com/sashabaranov/go-openai"

	"github.com/henomis/phero/llm"
)

var _ llm.Transcriber = (*Client)(nil)
var _ llm.SpeechSynthesizer = (*Client)(nil)

const (
	// DefaultTranscriptionModel is the model used when no transcription model is specified.
	DefaultTranscriptionModel = openaiapi.Whisper1
	// DefaultSpeechModel is the model used when no text-to-speech model is specified.
	DefaultSpeechModel = string(openaiapi.TTSModelGPT4oMini)
	// DefaultSpeechVoice is the voice used when no explicit voice is specified.
	DefaultSpeechVoice = string(openaiapi.VoiceAlloy)
)

// Transcribe converts spoken audio to text using OpenAI's audio transcription API.
func (c *Client) Transcribe(ctx context.Context, req llm.TranscriptionRequest) (*llm.TranscriptionResult, error) {
	if strings.TrimSpace(req.Input.FilePath) == "" && req.Input.Reader == nil {
		return nil, ErrTranscriptionInputRequired
	}

	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = DefaultTranscriptionModel
	}

	request := openaiapi.AudioRequest{
		Model:       model,
		FilePath:    req.Input.FilePath,
		Reader:      req.Input.Reader,
		Prompt:      req.Prompt,
		Temperature: req.Temperature,
		Language:    req.Language,
		Format:      openaiapi.AudioResponseFormat(req.Format),
	}
	if len(req.TimestampGranularities) > 0 {
		request.TimestampGranularities = make([]openaiapi.TranscriptionTimestampGranularity, 0, len(req.TimestampGranularities))
		for _, granularity := range req.TimestampGranularities {
			request.TimestampGranularities = append(
				request.TimestampGranularities,
				openaiapi.TranscriptionTimestampGranularity(granularity),
			)
		}
	}

	res, err := c.client.CreateTranscription(ctx, request)
	if err != nil {
		return nil, err
	}

	return transcriptionResultFromOpenAI(res), nil
}

// SynthesizeSpeech converts text to audio using OpenAI's text-to-speech API.
func (c *Client) SynthesizeSpeech(ctx context.Context, req llm.SpeechRequest) (*llm.SpeechResult, error) {
	if strings.TrimSpace(req.Input) == "" {
		return nil, ErrSpeechInputRequired
	}

	model := openaiapi.SpeechModel(strings.TrimSpace(req.Model))
	if model == "" {
		model = openaiapi.SpeechModel(DefaultSpeechModel)
	}

	voice := openaiapi.SpeechVoice(strings.TrimSpace(req.Voice))
	if voice == "" {
		voice = openaiapi.SpeechVoice(DefaultSpeechVoice)
	}

	format := req.Format
	if format == "" {
		format = llm.SpeechResponseFormatMP3
	}

	raw, err := c.client.CreateSpeech(ctx, openaiapi.CreateSpeechRequest{
		Model:          model,
		Input:          req.Input,
		Voice:          voice,
		Instructions:   req.Instructions,
		ResponseFormat: openaiapi.SpeechResponseFormat(format),
		Speed:          req.Speed,
	})
	if err != nil {
		return nil, err
	}
	defer raw.Close()

	data, err := io.ReadAll(raw)
	if err != nil {
		return nil, err
	}

	return &llm.SpeechResult{
		Data:     data,
		MIMEType: llm.SpeechMIMEType(format),
		Format:   format,
	}, nil
}

func transcriptionResultFromOpenAI(res openaiapi.AudioResponse) *llm.TranscriptionResult {
	out := &llm.TranscriptionResult{
		Task:     res.Task,
		Language: res.Language,
		Duration: res.Duration,
		Text:     res.Text,
	}

	if len(res.Segments) > 0 {
		out.Segments = make([]llm.TranscriptionSegment, 0, len(res.Segments))
		for _, segment := range res.Segments {
			out.Segments = append(out.Segments, llm.TranscriptionSegment{
				ID:               segment.ID,
				Seek:             segment.Seek,
				Start:            segment.Start,
				End:              segment.End,
				Text:             segment.Text,
				Tokens:           segment.Tokens,
				Temperature:      segment.Temperature,
				AvgLogprob:       segment.AvgLogprob,
				CompressionRatio: segment.CompressionRatio,
				NoSpeechProb:     segment.NoSpeechProb,
				Transient:        segment.Transient,
			})
		}
	}

	if len(res.Words) > 0 {
		out.Words = make([]llm.TranscriptionWord, 0, len(res.Words))
		for _, word := range res.Words {
			out.Words = append(out.Words, llm.TranscriptionWord{
				Word:  word.Word,
				Start: word.Start,
				End:   word.End,
			})
		}
	}

	return out
}
