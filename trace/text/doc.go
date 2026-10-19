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

// Package text provides a human-readable, colour-coded trace.Processor that
// prints the span tree of agent runs to an io.Writer.
//
//	p := trace.NewProvider(trace.WithProcessor(text.New(os.Stderr)))
//	a.SetTracing(p)
//
// A line is printed when a span ends, indented by its depth in the tree. Run
// spans also print a line when they start; WithVerbose does so for every
// span. Content (prompts, tool arguments and results, outputs) is printed in
// full unless WithMaxLen is set.
package text
