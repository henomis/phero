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

package bash

import (
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// volatileEnv are variables bash sets for itself; they are not carried from
// one command to the next.
var volatileEnv = map[string]bool{"PWD": true, "OLDPWD": true, "SHLVL": true, "_": true}

// shellState is what a persistent shell carries from one command to the
// next: the working directory and the environment changes.
type shellState struct {
	mu    sync.Mutex
	cwd   string
	set   map[string]string
	unset map[string]bool
}

// dir returns the directory the next command starts in: the last command's
// final directory, or fallback before the first command.
func (s *shellState) dir(fallback string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cwd != "" {
		return s.cwd
	}

	return fallback
}

// apply returns env with the carried changes applied.
func (s *shellState) apply(env []string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.set) == 0 && len(s.unset) == 0 {
		return env
	}

	out := make([]string, 0, len(env)+len(s.set))

	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if _, changed := s.set[name]; !changed && !s.unset[name] {
			out = append(out, kv)
		}
	}

	for name, value := range s.set {
		out = append(out, name+"="+value)
	}

	return out
}

// update records the final directory and environment a command reported,
// against the environment it started with.
func (s *shellState) update(cwd string, before, after []string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.set == nil {
		s.set, s.unset = map[string]string{}, map[string]bool{}
	}

	s.cwd = cwd

	old := envMap(before)
	now := envMap(after)

	for name, value := range now {
		if !volatileEnv[name] && old[name] != value {
			s.set[name] = value
			delete(s.unset, name)
		}
	}

	for name := range old {
		if _, kept := now[name]; !kept && !volatileEnv[name] {
			s.unset[name] = true
			delete(s.set, name)
		}
	}
}

// envMap turns KEY=VALUE entries into a map.
func envMap(env []string) map[string]string {
	m := make(map[string]string, len(env))

	for _, kv := range env {
		if name, value, ok := strings.Cut(kv, "="); ok {
			m[name] = value
		}
	}

	return m
}

// persistentScript wraps command so that, after it, the shell reports its
// final directory and environment on file descriptor 3. The command itself
// runs with descriptor 3 closed, so a process it leaves in the background
// cannot hold the report open. A command that calls exit reports nothing.
func persistentScript(command string) string {
	return "{\n" + command + "\n} 3>&-\n" +
		"__phero_rc=$?\n" +
		"{ printf '%s\\0' \"$PWD\"; env -0; } >&3 2>/dev/null\n" +
		"exit $__phero_rc\n"
}

// stateReport reads the report a persistent script writes on descriptor 3.
type stateReport struct {
	w    *os.File
	done chan []byte
}

// attachStateReport gives cmd a descriptor 3 to report its state on.
func attachStateReport(cmd *exec.Cmd) (*stateReport, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}

	cmd.ExtraFiles = append(cmd.ExtraFiles, w)

	report := &stateReport{w: w, done: make(chan []byte, 1)}

	go func() {
		defer func() { _ = r.Close() }()

		data, _ := io.ReadAll(r)
		report.done <- data
	}()

	return report, nil
}

// read returns the reported directory and environment once the command has
// exited, and false when there is no complete report.
func (s *stateReport) read() (string, []string, bool) {
	_ = s.w.Close()

	var data []byte

	select {
	case data = <-s.done:
	case <-time.After(waitDelay):
		return "", nil, false
	}

	records := strings.Split(string(data), "\x00")
	if len(records) < 2 || records[0] == "" { //nolint:mnd // the directory, then the environment
		return "", nil, false
	}

	env := records[1:]
	if env[len(env)-1] == "" {
		env = env[:len(env)-1]
	}

	return records[0], env, true
}
