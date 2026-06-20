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

import "time"

const (
	defaultHeartbeatInterval = 30 * time.Second
	defaultKeepaliveInterval = 30 * time.Second
	defaultInactivityTimeout = 60 * time.Second
	defaultDiscoveryTimeout  = 2 * time.Second
	defaultStallTimeout      = 750 * time.Millisecond
)

// — Server options ——————————————————————————————————————————————————————————

// ServerOption configures a [Server].
type ServerOption func(*serverConfig)

type serverConfig struct {
	// agentID is the metadata.agent value advertised in the service (§3.2).
	agentID string
	// session is the optional metadata.session value (§3.2).
	session string
	// version is the harness implementation version (not the protocol version).
	version string
	// maxPayload is the human-readable max payload size for the prompt endpoint (§2.1).
	maxPayload string
	// attachmentsOk controls the attachments_ok endpoint metadata flag (§2.1).
	attachmentsOk bool
	// heartbeatInterval is the cadence of heartbeat publication (§8.2).
	heartbeatInterval time.Duration
	// keepaliveInterval is the cadence of mid-stream ack chunks (§6.4).
	keepaliveInterval time.Duration
}

func defaultServerConfig() *serverConfig {
	return &serverConfig{
		agentID:           "phero",
		version:           "0.1.0",
		maxPayload:        "1MB",
		attachmentsOk:     false,
		heartbeatInterval: defaultHeartbeatInterval,
		keepaliveInterval: defaultKeepaliveInterval,
	}
}

// WithAgentID overrides the metadata.agent value (default "phero").
func WithAgentID(id string) ServerOption {
	return func(c *serverConfig) { c.agentID = id }
}

// WithSession sets the optional metadata.session value (§3.2).
func WithSession(session string) ServerOption {
	return func(c *serverConfig) { c.session = session }
}

// WithVersion sets the harness implementation version reported in the service
// registration. Defaults to "0.1.0".
func WithVersion(v string) ServerOption {
	return func(c *serverConfig) { c.version = v }
}

// WithMaxPayload sets the max_payload endpoint metadata value (§2.1).
// Format: a positive integer followed by B, KB, MB, or GB. Default "1MB".
func WithMaxPayload(s string) ServerOption {
	return func(c *serverConfig) { c.maxPayload = s }
}

// WithAttachmentsOk controls whether the prompt endpoint advertises
// attachments_ok=true (§2.1). Default false.
func WithAttachmentsOk(ok bool) ServerOption {
	return func(c *serverConfig) { c.attachmentsOk = ok }
}

// WithHeartbeatInterval sets the heartbeat publication cadence (§8.2).
// Default 30 seconds. Values below 1 second should not be used on shared
// infrastructure.
func WithHeartbeatInterval(d time.Duration) ServerOption {
	return func(c *serverConfig) { c.heartbeatInterval = d }
}

// WithKeepaliveInterval sets the cadence of mid-stream ack status chunks
// emitted during long-running agent work (§6.4). Default 30 seconds.
func WithKeepaliveInterval(d time.Duration) ServerOption {
	return func(c *serverConfig) { c.keepaliveInterval = d }
}

// — Client options ——————————————————————————————————————————————————————————

// ClientOption configures a [Client].
type ClientOption func(*clientConfig)

type clientConfig struct {
	// inactivityTimeout is the per-stream timeout (§6.6). Default 60 s.
	inactivityTimeout time.Duration
	// discoveryTimeout is the absolute ceiling for the discovery fan-out. Default 2 s.
	discoveryTimeout time.Duration
	// stallTimeout is the "quiet period" that ends discovery early. Default 750 ms.
	stallTimeout time.Duration
}

func defaultClientConfig() *clientConfig {
	return &clientConfig{
		inactivityTimeout: defaultInactivityTimeout,
		discoveryTimeout:  defaultDiscoveryTimeout,
		stallTimeout:      defaultStallTimeout,
	}
}

// WithInactivityTimeout overrides the per-stream inactivity timeout (§6.6).
// Default 60 seconds.
func WithInactivityTimeout(d time.Duration) ClientOption {
	return func(c *clientConfig) { c.inactivityTimeout = d }
}

// WithDiscoveryTimeout overrides the absolute discovery timeout. Default 2 s.
func WithDiscoveryTimeout(d time.Duration) ClientOption {
	return func(c *clientConfig) { c.discoveryTimeout = d }
}

// — Discover filters ————————————————————————————————————————————————————————

// DiscoverOption filters the agents returned by [Client.Discover].
type DiscoverOption func(*discoverFilter)

type discoverFilter struct {
	agent string
	owner string
	name  string
}

// FilterByAgent restricts discovery to agents with the given metadata.agent
// value (e.g. "phero", "claude-code").
func FilterByAgent(agent string) DiscoverOption {
	return func(f *discoverFilter) { f.agent = agent }
}

// FilterByOwner restricts discovery to agents with the given metadata.owner value.
func FilterByOwner(owner string) DiscoverOption {
	return func(f *discoverFilter) { f.owner = owner }
}

// FilterByName restricts discovery to agents with the given instance name
// (the 5th token of the prompt endpoint subject).
func FilterByName(name string) DiscoverOption {
	return func(f *discoverFilter) { f.name = name }
}
