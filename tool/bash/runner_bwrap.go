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
	"context"
	"os"
	"os/exec"
	"path/filepath"
)

// BubblewrapRunner is a Runner that runs each command in a bubblewrap
// (bwrap) sandbox, on Linux:
//   - the host file system is read-only, except the workspace and the paths
//     given with WithWritablePaths;
//   - /tmp is a private, empty tmpfs, and /dev and /proc are the sandbox's own;
//   - every namespace is unshared (no network unless WithNetwork(true), own
//     PID namespace, so killing the command kills everything it started);
//   - the environment is limited to DefaultEnvAllowlist unless the tool sets
//     one (WithEnvAllowlist, WithEnv).
//
// Tools that write outside the workspace need those paths made writable, for
// example the Go build and module caches.
type BubblewrapRunner struct {
	bwrap     string
	workspace string
	writable  []string
	network   bool
}

// BubblewrapOption configures a BubblewrapRunner.
type BubblewrapOption func(*BubblewrapRunner)

// WithNetwork lets sandboxed commands use the host network. It is off by
// default.
func WithNetwork(enabled bool) BubblewrapOption {
	return func(r *BubblewrapRunner) { r.network = enabled }
}

// WithWritablePaths makes more host paths writable inside the sandbox, such
// as a build cache. Relative paths are made absolute.
func WithWritablePaths(paths ...string) BubblewrapOption {
	return func(r *BubblewrapRunner) { r.writable = append(r.writable, paths...) }
}

// NewBubblewrapRunner returns a BubblewrapRunner whose commands may write in
// workspace and run there by default. It returns ErrBubblewrapNotFound when
// bwrap is not installed.
func NewBubblewrapRunner(workspace string, opts ...BubblewrapOption) (*BubblewrapRunner, error) {
	bwrap, err := exec.LookPath("bwrap")
	if err != nil {
		return nil, ErrBubblewrapNotFound
	}

	r := &BubblewrapRunner{bwrap: bwrap}
	for _, opt := range opts {
		opt(r)
	}

	if r.workspace, err = filepath.Abs(workspace); err != nil {
		return nil, err
	}

	for i, p := range r.writable {
		if r.writable[i], err = filepath.Abs(p); err != nil {
			return nil, err
		}
	}

	return r, nil
}

// Command returns the bwrap invocation that runs `bash -c command` in the
// sandbox, in opts.Dir (the workspace when empty).
func (r *BubblewrapRunner) Command(ctx context.Context, command string, opts RunOptions) (*exec.Cmd, error) {
	dir := opts.Dir
	if dir == "" {
		dir = r.workspace
	}

	args := []string{
		"--ro-bind", "/", "/",
		"--dev", "/dev",
		"--proc", "/proc",
		"--tmpfs", "/tmp",
		"--bind", r.workspace, r.workspace,
	}

	for _, p := range r.writable {
		args = append(args, "--bind", p, p)
	}

	args = append(args, "--unshare-all")
	if r.network {
		args = append(args, "--share-net")
	}

	args = append(args, "--die-with-parent", "--chdir", dir, "--", "bash", "-c", command)

	env := opts.Env
	if env == nil {
		env = filterEnv(os.Environ(), DefaultEnvAllowlist)
	}

	cmd := exec.CommandContext(ctx, r.bwrap, args...) //nolint:gosec // the tool runs arbitrary commands by design
	cmd.Env = env

	return cmd, nil
}
