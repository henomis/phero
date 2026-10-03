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

// defaultMaxPayload is the advertised prompt-endpoint cap when none is set. It
// matches NATS's own default max_payload, and [New] tightens it when the
// connection turns out to allow less.
const defaultMaxPayload = "1MB"

// defaultAgentID is the metadata.agent value when none is set.
const defaultAgentID = "phero"

const (
	defaultHeartbeatInterval = 30 * time.Second
	defaultKeepaliveInterval = 30 * time.Second
	defaultDrainTimeout      = 30 * time.Second
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
	// maxPayloadSet distinguishes a configured cap from the default, so New can
	// reject the first for exceeding the connection's limit while quietly
	// tightening the second.
	maxPayloadSet bool
	// attachmentsOk controls the attachments_ok endpoint metadata flag (§2.1).
	attachmentsOk bool
	// heartbeatInterval is the cadence of heartbeat publication (§8.2).
	heartbeatInterval time.Duration
	// keepaliveInterval is the cadence of mid-stream ack chunks (§6.4).
	keepaliveInterval time.Duration
	// drainTimeout bounds how long a shutdown waits for in-flight prompt
	// handlers before cancelling them.
	drainTimeout time.Duration
	// errorMapper, when set, chooses the error sent for a failed run before
	// the defaults do.
	errorMapper func(error) *CodedError
}

func defaultServerConfig() *serverConfig {
	return &serverConfig{
		agentID:           defaultAgentID,
		version:           "0.1.0",
		maxPayload:        defaultMaxPayload,
		attachmentsOk:     false,
		heartbeatInterval: defaultHeartbeatInterval,
		keepaliveInterval: defaultKeepaliveInterval,
		drainTimeout:      defaultDrainTimeout,
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
// A value above what the connection will carry is rejected by [New]: it would
// disable the guard it configures, and the entry that slipped past it would fail
// later as an opaque transport error instead.
func WithMaxPayload(s string) ServerOption {
	return func(c *serverConfig) {
		c.maxPayload = s
		c.maxPayloadSet = true
	}
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

// WithDrainTimeout bounds a shutdown: how long [Server.Drain] (and so
// [Server.Start], once its context is cancelled) spends waiting for the prompt
// handlers already running, cancelling them, and letting them unwind. Default
// 30 seconds.
//
// It is a total, so it can be sized against an enclosing shutdown budget
// directly — there is no second timeout underneath it to reconcile.
//
// Size it against the work an agent actually does. The handlers being waited for
// are LLM calls that have already been paid for, so a budget shorter than a
// typical completion throws that money away; one longer than the orchestrator's
// own shutdown budget is never reached.
//
// A zero or negative value cancels in-flight handlers immediately, which is the
// behaviour of releases before this option existed. Choose it deliberately: it
// means a restart abandons every call in flight.
func WithDrainTimeout(d time.Duration) ServerOption {
	return func(c *serverConfig) { c.drainTimeout = d }
}

// WithErrorMapper sets fn to choose the error a caller receives when the
// handler fails (§9). It sees the error first: return a [CodedError] to send
// it, or nil to fall back to the defaults — a CodedError in the error's chain,
// then the mapping of LLM provider errors, then 500 "internal_error".
//
// It is also the place to keep internal detail away from callers: by default
// the error's text is sent as the message, and an LLM provider's error text
// can carry account, quota or endpoint details.
func WithErrorMapper(fn func(error) *CodedError) ServerOption {
	return func(c *serverConfig) { c.errorMapper = fn }
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
	agent   string
	owner   string
	name    string
	session string
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
//
// For an agent served with [WithSession] that name is the suffixed one — see
// [InstanceName], or use [FilterBySession] and avoid composing it.
func FilterByName(name string) DiscoverOption {
	return func(f *discoverFilter) { f.name = name }
}

// FilterBySession restricts discovery to agents with the given metadata.session
// value (§3.2) — the session passed to [WithSession], not the instance name it
// produces.
//
// Combined with [FilterByOwner] this addresses one deployment's agents without
// knowing how a session is joined to a name.
func FilterBySession(session string) DiscoverOption {
	return func(f *discoverFilter) { f.session = session }
}
