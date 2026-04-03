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
	"fmt"
	"strings"
	"sync"

	"github.com/henomis/phero/agent"
)

// FeedEntry is a single post written by a persona agent during a simulation round.
type FeedEntry struct {
	Round  int
	Author string
	Post   string
}

// WorldFeed is the shared, append-only transcript of all simulation posts.
// It is safe for concurrent use.
type WorldFeed struct {
	mu      sync.Mutex
	entries []FeedEntry
}

// Append adds a new entry to the feed.
func (f *WorldFeed) Append(e FeedEntry) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries = append(f.entries, e)
}

// TopK returns the last k entries in the feed.
// If fewer than k entries exist, all entries are returned.
func (f *WorldFeed) TopK(k int) []FeedEntry {
	f.mu.Lock()
	defer f.mu.Unlock()

	if k <= 0 || len(f.entries) == 0 {
		return nil
	}

	start := len(f.entries) - k
	if start < 0 {
		start = 0
	}

	result := make([]FeedEntry, len(f.entries)-start)
	copy(result, f.entries[start:])

	return result
}

// Transcript returns the full simulation feed as a readable text block.
func (f *WorldFeed) Transcript() string {
	f.mu.Lock()
	defer f.mu.Unlock()

	if len(f.entries) == 0 {
		return "(empty)"
	}

	b := &strings.Builder{}
	for _, e := range f.entries {
		fmt.Fprintf(b, "[Round %d] %s: %s\n\n", e.Round, e.Author, e.Post)
	}

	return strings.TrimRight(b.String(), "\n")
}

// personaAgent pairs a persona's display name with its running agent instance.
type personaAgent struct {
	name  string
	agent *agent.Agent
}

// Simulation orchestrates a set of persona agents over multiple rounds,
// collecting their posts into a shared WorldFeed.
type Simulation struct {
	agents []*personaAgent
	feed   *WorldFeed
	topk   int
}

// newSimulation creates a Simulation from a slice of personaAgents.
func newSimulation(agents []*personaAgent, topk int) *Simulation {
	return &Simulation{
		agents: agents,
		feed:   &WorldFeed{},
		topk:   topk,
	}
}

// RunRound executes one simulation round, running all persona agents concurrently.
// Each agent observes the same feed snapshot from before the round starts, so
// goroutines do not race on WorldFeed reads.
// onPost is an optional callback invoked (in deterministic agent order) after
// each agent post is collected.
func (s *Simulation) RunRound(ctx context.Context, round, totalRounds int, onPost func(FeedEntry)) error {
	// Snapshot the feed before fanout so every agent sees the same state.
	snapshot := s.feed.TopK(s.topk)

	type roundResult struct {
		entry FeedEntry
		err   error
	}

	results := make([]roundResult, len(s.agents))

	var wg sync.WaitGroup

	for i, pa := range s.agents {
		wg.Add(1)

		go func(idx int, pa *personaAgent) {
			defer wg.Done()

			prompt := buildRoundPrompt(round, totalRounds, snapshot)

			out, err := pa.agent.Run(ctx, prompt)
			if err != nil {
				results[idx] = roundResult{err: fmt.Errorf("agent %q: %w", pa.name, err)}
				return
			}

			results[idx] = roundResult{
				entry: FeedEntry{
					Round:  round,
					Author: pa.name,
					Post:   strings.TrimSpace(out.Content),
				},
			}
		}(i, pa)
	}

	wg.Wait()

	// Collect in deterministic (agent-list) order.
	for _, r := range results {
		if r.err != nil {
			return r.err
		}

		s.feed.Append(r.entry)

		if onPost != nil {
			onPost(r.entry)
		}
	}

	return nil
}

// Transcript returns the full world-feed formatted as a readable text block.
func (s *Simulation) Transcript() string {
	return s.feed.Transcript()
}

// buildRoundPrompt formats the prompt sent to each persona agent during a round.
func buildRoundPrompt(round, totalRounds int, snapshot []FeedEntry) string {
	b := &strings.Builder{}

	fmt.Fprintf(b, "Round %d of %d.\n\n", round, totalRounds)

	if len(snapshot) == 0 {
		b.WriteString("No posts yet — you are the first to speak.\n\n")
	} else {
		b.WriteString("Recent simulation feed:\n\n")

		for _, e := range snapshot {
			fmt.Fprintf(b, "[Round %d] %s: %s\n\n", e.Round, e.Author, e.Post)
		}
	}

	b.WriteString("Write your response as a short social-media post (3-5 sentences). " +
		"Stay true to your persona and react to what others have said.")

	return b.String()
}
