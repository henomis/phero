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

// client discovers NATS agents and starts an interactive chat session
// with the first one found.
//
// Usage:
//
//	go run ./examples/nats-agent/client
//	go run ./examples/nats-agent/client -agent=phero -owner=alice
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/nats-io/nats.go"

	natsagent "github.com/henomis/phero/nats"
)

func main() {
	natsURL := flag.String("nats-url", "", "NATS server URL (overrides NATS_URL env var)")
	agentFilter := flag.String("agent", "", "Filter by metadata.agent (e.g. \"phero\")")
	ownerFilter := flag.String("owner", "", "Filter by metadata.owner")
	nameFilter := flag.String("name", "", "Filter by instance name")
	flag.Parse()

	url := resolveNATSURL(*natsURL)

	nc, err := nats.Connect(url)
	if err != nil {
		log.Fatalf("NATS connect %s: %v", url, err)
	}
	defer nc.Drain() //nolint:errcheck

	c := natsagent.NewClient(nc,
		natsagent.WithDiscoveryTimeout(2*time.Second),
		natsagent.WithInactivityTimeout(60*time.Second),
	)

	var discoverOpts []natsagent.DiscoverOption
	if *agentFilter != "" {
		discoverOpts = append(discoverOpts, natsagent.FilterByAgent(*agentFilter))
	}
	if *ownerFilter != "" {
		discoverOpts = append(discoverOpts, natsagent.FilterByOwner(*ownerFilter))
	}
	if *nameFilter != "" {
		discoverOpts = append(discoverOpts, natsagent.FilterByName(*nameFilter))
	}

	fmt.Println("Discovering agents...")
	ctx := context.Background()

	agents, err := c.Discover(ctx, discoverOpts...)
	if err != nil {
		log.Fatalf("discover: %v", err)
	}

	fmt.Printf("Found %d agent(s):\n", len(agents))
	for i, a := range agents {
		fmt.Printf("  [%d] agent=%-12s owner=%-12s name=%-12s protocol=%s\n",
			i+1, a.Agent, a.Owner, a.Name, a.ProtocolVersion)
	}

	target := agents[0]
	fmt.Printf("\nConnected to: %s/%s/%s\n", target.Agent, target.Owner, target.Name)
	fmt.Println("Type a message and press Enter. /exit to quit.")
	fmt.Println()

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 512*1024)

	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if line == "/exit" || line == "/quit" || line == "/q" {
			break
		}

		turnCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		stream, err := target.Prompt(turnCtx, line)
		if err != nil {
			cancel()
			fmt.Printf("Error: %v\n\n", err)
			continue
		}

		text, err := stream.Text(turnCtx)
		cancel()
		if err != nil {
			fmt.Printf("Error: %v\n\n", err)
			continue
		}

		fmt.Printf("\n%s\n\n", strings.TrimSpace(text))
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "input error: %v\n", err)
	}

	fmt.Println("Goodbye!")
}

func resolveNATSURL(flag string) string {
	if flag != "" {
		return flag
	}
	if v := os.Getenv("NATS_URL"); v != "" {
		return v
	}
	return nats.DefaultURL
}
