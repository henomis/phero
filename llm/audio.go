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

package llm

import (
	"context"
	"io"
)

// AudioInput identifies an audio payload used for transcription.
type AudioInput struct {
	// FilePath is either a real filesystem path or a synthetic filename used when
	// Reader is provided.
	FilePath string
	// Reader provides the audio bytes when the input does not come from a local file.
	Reader io.Reader
}

// AudioFile returns an AudioInput that reads audio from the given local path.
func AudioFile(path string) AudioInput {
	return AudioInput{FilePath: path}
}

// AudioReader returns an AudioInput that reads audio from reader and uses filename
// as the multipart upload filename.
func AudioReader(filename string, reader io.Reader) AudioInput {
	return AudioInput{FilePath: filename, Reader: reader}
}

// TranscriptionResponseFormat selects the format returned by a transcription API.
type TranscriptionResponseFormat string

const (
	// TranscriptionResponseFormatJSON returns a JSON response.
	TranscriptionResponseFormatJSON TranscriptionResponseFormat = "json"
	// TranscriptionResponseFormatText returns plain text.
	TranscriptionResponseFormatText TranscriptionResponseFormat = "text"
	// TranscriptionResponseFormatSRT returns SubRip subtitles.
	TranscriptionResponseFormatSRT TranscriptionResponseFormat = "srt"
	// TranscriptionResponseFormatVerboseJSON returns JSON with per-word and per-segment detail.
	TranscriptionResponseFormatVerboseJSON TranscriptionResponseFormat = "verbose_json"
	// TranscriptionResponseFormatVTT returns WebVTT subtitles.
	TranscriptionResponseFormatVTT TranscriptionResponseFormat = "vtt"
)

// TranscriptionTimestampGranularity selects which timestamps should be returned.
type TranscriptionTimestampGranularity string

const (
	// TranscriptionTimestampGranularityWord returns per-word timestamps.
	TranscriptionTimestampGranularityWord TranscriptionTimestampGranularity = "word"
	// TranscriptionTimestampGranularitySegment returns per-segment timestamps.
	TranscriptionTimestampGranularitySegment TranscriptionTimestampGranularity = "segment"
)

// TranscriptionRequest describes an audio-to-text request.
type TranscriptionRequest struct {
	// Model optionally overrides the provider's default transcription model.
	Model string
	// Input identifies the audio payload to transcribe.
	Input AudioInput
	// Prompt provides optional context that can bias transcription.
	Prompt string
	// Temperature controls output randomness when supported by the provider.
	Temperature float32
	// Language hints the spoken language using an ISO-639-1 code when supported.
	Language string
	// Format chooses the response format returned by the provider.
	Format TranscriptionResponseFormat
	// TimestampGranularities requests word- or segment-level timestamps when supported.
	TimestampGranularities []TranscriptionTimestampGranularity
}

// TranscriptionSegment contains a timed segment in a verbose transcription result.
type TranscriptionSegment struct {
	// ID is the provider-assigned segment identifier.
	ID int
	// Seek is the provider-specific offset used internally during decoding.
	Seek int
	// Start is the segment start time in seconds.
	Start float64
	// End is the segment end time in seconds.
	End float64
	// Text is the transcript text for the segment.
	Text string
	// Tokens are provider-specific token IDs when available.
	Tokens []int
	// Temperature is the decoding temperature used for the segment.
	Temperature float64
	// AvgLogprob is the average log probability for the segment.
	AvgLogprob float64
	// CompressionRatio is the provider-reported compression ratio for the segment.
	CompressionRatio float64
	// NoSpeechProb is the provider's probability that no speech was present.
	NoSpeechProb float64
	// Transient reports whether the segment is temporary or unstable.
	Transient bool
}

// TranscriptionWord contains a timed word in a verbose transcription result.
type TranscriptionWord struct {
	// Word is the recognized word.
	Word string
	// Start is the word start time in seconds.
	Start float64
	// End is the word end time in seconds.
	End float64
}

// TranscriptionResult is the normalized output of an audio transcription request.
type TranscriptionResult struct {
	// Task is the provider-reported task type.
	Task string
	// Language is the detected or enforced language code.
	Language string
	// Duration is the input audio duration in seconds when available.
	Duration float64
	// Segments holds verbose segment-level timing when requested and supported.
	Segments []TranscriptionSegment
	// Words holds verbose word-level timing when requested and supported.
	Words []TranscriptionWord
	// Text is the transcript text.
	Text string
}

// Transcriber is implemented by providers that support speech-to-text.
type Transcriber interface {
	// Transcribe converts spoken audio to text.
	Transcribe(context.Context, TranscriptionRequest) (*TranscriptionResult, error)
}

// SpeechResponseFormat selects the encoding returned by a speech synthesis API.
type SpeechResponseFormat string

const (
	// SpeechResponseFormatMP3 returns MP3-encoded audio.
	SpeechResponseFormatMP3 SpeechResponseFormat = "mp3"
	// SpeechResponseFormatOpus returns Opus-encoded audio.
	SpeechResponseFormatOpus SpeechResponseFormat = "opus"
	// SpeechResponseFormatAAC returns AAC-encoded audio.
	SpeechResponseFormatAAC SpeechResponseFormat = "aac"
	// SpeechResponseFormatFLAC returns FLAC-encoded audio.
	SpeechResponseFormatFLAC SpeechResponseFormat = "flac"
	// SpeechResponseFormatWAV returns WAV-encoded audio.
	SpeechResponseFormatWAV SpeechResponseFormat = "wav"
	// SpeechResponseFormatPCM returns raw PCM audio.
	SpeechResponseFormatPCM SpeechResponseFormat = "pcm"
)

// SpeechRequest describes a text-to-speech request.
type SpeechRequest struct {
	// Model optionally overrides the provider's default speech model.
	Model string
	// Input is the text to synthesize.
	Input string
	// Voice chooses the provider voice when supported.
	Voice string
	// Instructions provides optional style guidance when supported.
	Instructions string
	// Format chooses the output audio encoding.
	Format SpeechResponseFormat
	// Speed adjusts the speaking speed when supported.
	Speed float64
}

// SpeechResult is the normalized output of a speech synthesis request.
type SpeechResult struct {
	// Data holds the synthesized audio bytes.
	Data []byte
	// MIMEType identifies the encoding of Data.
	MIMEType string
	// Format identifies the audio encoding used for Data.
	Format SpeechResponseFormat
}

// SpeechSynthesizer is implemented by providers that support text-to-speech.
type SpeechSynthesizer interface {
	// SynthesizeSpeech converts text to spoken audio.
	SynthesizeSpeech(context.Context, SpeechRequest) (*SpeechResult, error)
}

// SpeechMIMEType returns the MIME type associated with the given speech format.
func SpeechMIMEType(format SpeechResponseFormat) string {
	switch format {
	case "", SpeechResponseFormatMP3:
		return "audio/mpeg"
	case SpeechResponseFormatOpus:
		return "audio/opus"
	case SpeechResponseFormatAAC:
		return "audio/aac"
	case SpeechResponseFormatFLAC:
		return "audio/flac"
	case SpeechResponseFormatWAV:
		return "audio/wav"
	case SpeechResponseFormatPCM:
		return "audio/pcm"
	default:
		return "application/octet-stream"
	}
}
