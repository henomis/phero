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

// Package web provides a Phero tool that fetches a web page for an agent and
// returns it as text: documentation, an API reference, an issue.
//
// HTML is turned into readable text, with links kept as "text [url]"; script
// and style are dropped. Other text and JSON responses are returned as they
// are; binary content is refused. Responses are capped in size and time.
//
// The URL comes from the model, so by default the tool refuses addresses on
// loopback, private, link-local, carrier-grade NAT and "this network"
// (0.0.0.0/8) addresses (including cloud metadata endpoints), and IPv6
// addresses that carry such an IPv4 address (NAT64, 6to4, Teredo), checked
// when connecting, so redirects and DNS answers cannot get
// around it. WithPrivateNetworks(true) allows them, for example to read a
// local dev server; WithAllowedHosts limits the hosts that can be fetched.
//
//	fetcher := web.New(web.WithAllowedHosts("*.go.dev", "pkg.go.dev"))
//	tool, _ := fetcher.Tool()
//	_ = myAgent.AddTool(tool)
package web
