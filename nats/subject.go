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

import (
	"fmt"
	"regexp"
)

// Labels for the identifiers that become subject tokens, so an error names the
// thing the caller configured rather than the position it occupies. The owner
// and session labels are the protocol's own metadata keys.
const (
	fieldAgentID = "agent id"
	fieldName    = "name"
)

// maxSubjectToken bounds a single token. NATS imposes no per-token limit, but
// the protocol's identifiers are names, and an unbounded one only makes the
// subject harder to read and log.
const maxSubjectToken = 64

// subjectTokenPattern is the shape required of every identifier phero
// interpolates into a subject: the agent id, the owner and the instance name,
// which become tokens 3, 4 and 5 of agents.{verb}.{agent}.{owner}.{name}.
//
// It excludes the three characters that would change the subject's shape or
// reach rather than merely name something within it — "." (a token separator),
// "*" (a single-token wildcard) and ">" (a tail wildcard) — and everything else
// outside a conservative name alphabet, since NATS tokens are compared byte for
// byte and whitespace or control characters in one are a debugging trap.
var subjectTokenPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// validateSubjectIdentity checks every identifier a [Server] interpolates into
// its subjects. The instance name is checked as the composed value, because
// that is what reaches the wire: a name and a session that are each legal can
// still exceed the length bound once joined.
//
// The session is reported on its own first, so a bad one is named as a session
// rather than as the composed instance name the user never typed.
func validateSubjectIdentity(agentID, owner, instanceName, session string) error {
	if session != "" {
		if err := ValidateSubjectToken(session); err != nil {
			return fmt.Errorf("%s: %w", metaSession, err)
		}
	}

	for _, f := range []struct {
		field string
		value string
	}{
		{fieldAgentID, agentID},
		{metaOwner, owner},
		{fieldName, instanceName},
	} {
		if err := ValidateSubjectToken(f.value); err != nil {
			return fmt.Errorf("%s: %w", f.field, err)
		}
	}

	return nil
}

// ValidateSubjectToken reports whether token is usable as one token of the
// agent protocol's subject hierarchy.
//
// It is exported because the constraint is not phero's private business: any
// embedder that reads these identifiers from a config file wants to reject a
// bad one where the user can still fix it, rather than at [New] — and there is
// no reason for it to guess at the rule.
//
// The value of enforcing it at all is [Server]: the owner and name are
// interpolated verbatim into the prompt subject, which is subscribed in the
// shared "agents" queue group. An owner of "*" therefore produces a *wildcard
// subscription inside that queue group*, so the NATS server load-balances other
// tenants' prompts to it — the subscriber reads prompts addressed to agents it
// does not own, and its replies are accepted as those agents' answers. A "."
// is quieter and still wrong: it shifts every later token, so the agent
// registers on a subject nobody addresses and discovery parses the wrong name
// out of it.
func ValidateSubjectToken(token string) error {
	if token == "" {
		return fmt.Errorf("%w: must not be empty", ErrInvalidSubjectToken)
	}

	if len(token) > maxSubjectToken {
		return fmt.Errorf("%w: %q is %d characters, over the %d-character limit",
			ErrInvalidSubjectToken, token, len(token), maxSubjectToken)
	}

	if !subjectTokenPattern.MatchString(token) {
		return fmt.Errorf(
			"%w: %q must match [A-Za-z0-9_-] — it becomes one token of a NATS subject, "+
				"so \".\", \"*\" and \">\" would change which subjects it covers",
			ErrInvalidSubjectToken, token)
	}

	return nil
}

// reconcileMaxPayload checks the advertised prompt-endpoint cap against what the
// connection will actually carry, and tightens the default down to it.
//
// The advertised value is a constant otherwise — its 1MB default happens to
// equal NATS's own default max_payload, so it looks reconciled and is not. On a
// server configured lower, the cap admits envelopes the transport then rejects
// with `nats: maximum payload exceeded`: exactly the opaque failure the setting
// exists to replace, produced at the moment an agent returns a large reply
// rather than at construction.
//
// A configured cap that is too large is an error, because two configured numbers
// that contradict each other are a configuration mistake and the layer holding
// both should say so. The default is not the embedder's statement of intent, so
// it is quietly lowered instead.
func reconcileMaxPayload(cfg *serverConfig, connLimit int64) error {
	advertised, err := parseMaxPayload(cfg.maxPayload)
	if err != nil {
		// An unparseable value is worse than a wrong one: every client's parse
		// fails too, leaving MaxPayloadBytes zero, which disables the client-side
		// guard entirely rather than setting it badly.
		return fmt.Errorf("%w: %s", ErrInvalidMaxPayload, err.Error())
	}

	if advertised <= 0 {
		return fmt.Errorf("%w: %q is not a positive size", ErrInvalidMaxPayload, cfg.maxPayload)
	}

	// A connection that has not yet learned the server's limit reports 0.
	if connLimit <= 0 || advertised <= connLimit {
		return nil
	}

	if cfg.maxPayloadSet {
		return fmt.Errorf(
			"%w: %q (%d bytes) exceeds this NATS server's max_payload (%d bytes); lower it to %d or less",
			ErrInvalidMaxPayload, cfg.maxPayload, advertised, connLimit, connLimit)
	}

	cfg.maxPayload = fmt.Sprintf("%dB", connLimit)

	return nil
}
