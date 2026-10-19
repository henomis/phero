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

// Package jsonfile provides a trace.Processor that writes every ended span as
// one line of newline-delimited JSON (NDJSON) to a file.
//
// Each line holds the span identity and parent, name, kind, timing, error,
// attributes, events, links, totals and the full typed payload (prompts,
// responses, tool arguments and results). The file is opened in append mode,
// so restarts accumulate into the same file.
//
//	proc, err := jsonfile.New("trace.ndjson")
//	if err != nil { ... }
//	p := trace.NewProvider(trace.WithProcessor(proc))
//	defer p.Shutdown(ctx) // closes the file
package jsonfile
