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
	"strings"
)

// BubblewrapRunner is a Runner that runs each command in a bubblewrap
// (bwrap) sandbox, on Linux:
//   - of the host file system, only the system directories (SystemPaths) are
//     mounted, read-only, plus the workspace and the paths given with
//     WithWritablePaths (writable) and WithReadOnlyPaths;
//   - /tmp, /run and $HOME are private, empty tmpfs mounts, and /dev and /proc
//     are the sandbox's own;
//   - every namespace is unshared (no network unless WithNetwork(true), own
//     PID namespace, so killing the command kills everything it started);
//   - the environment is limited to DefaultEnvAllowlist unless the tool sets
//     one (WithEnvAllowlist, WithEnv).
//
// The rest of the host is not mounted, so host Unix sockets (docker.sock, the
// D-Bus buses, agent sockets under $HOME) cannot be reached: connect() on a
// socket ignores read-only mounts. Nor can files such as ~/.ssh be read.
//
// Tools installed or caching outside the system directories need those paths
// mounted, for example WithReadOnlyPaths for a toolchain under $HOME and
// WithWritablePaths for the Go build and module caches.
type BubblewrapRunner struct {
	bwrap     string
	workspace string
	home      string
	writable  []string
	readOnly  []string
	network   bool
}

// SystemPaths returns the host directories a BubblewrapRunner mounts
// read-only, when they exist. A path that is a symlink, such as /bin on
// merged-/usr systems, is recreated as the same symlink.
func SystemPaths() []string {
	return []string{"/usr", "/bin", "/sbin", "/lib", "/lib32", "/lib64", "/libx32", "/etc", "/opt"}
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

// WithReadOnlyPaths mounts more host paths read-only inside the sandbox, such
// as a toolchain installed under $HOME. Relative paths are made absolute.
// Unix sockets under them can be reached: mount no directory that holds one.
func WithReadOnlyPaths(paths ...string) BubblewrapOption {
	return func(r *BubblewrapRunner) { r.readOnly = append(r.readOnly, paths...) }
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

	for _, paths := range [][]string{r.writable, r.readOnly} {
		for i, p := range paths {
			if paths[i], err = filepath.Abs(p); err != nil {
				return nil, err
			}
		}
	}

	// The sandbox's $HOME is empty: tools write their caches there.
	if home, homeErr := os.UserHomeDir(); homeErr == nil && filepath.IsAbs(home) {
		r.home = filepath.Clean(home)
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

	args := systemMounts()
	args = append(args,
		"--dev", "/dev",
		"--proc", "/proc",
		"--tmpfs", "/tmp",
		"--tmpfs", "/run",
	)

	if r.home != "" {
		args = append(args, "--tmpfs", r.home)
	}

	if r.network {
		args = append(args, resolvConfMount()...)
	}

	// Later mounts go over earlier ones: the workspace and the extra paths may
	// lie under $HOME or /tmp.
	for _, p := range r.readOnly {
		args = append(args, "--ro-bind", p, p)
	}

	args = append(args, "--bind", r.workspace, r.workspace)

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

// systemMounts returns the bwrap arguments that mount SystemPaths read-only.
func systemMounts() []string {
	var args []string

	for _, p := range SystemPaths() {
		info, err := os.Lstat(p)
		if err != nil {
			continue
		}

		if info.Mode()&os.ModeSymlink != 0 {
			if target, linkErr := os.Readlink(p); linkErr == nil {
				args = append(args, "--symlink", target, p)
			}

			continue
		}

		args = append(args, "--ro-bind", p, p)
	}

	return args
}

// resolvConfMount returns the bwrap arguments that keep DNS working with the
// network on: /etc/resolv.conf is often a symlink into /run (systemd-resolved),
// which the sandbox replaces with an empty tmpfs, so its target file is mounted.
func resolvConfMount() []string {
	target, err := filepath.EvalSymlinks("/etc/resolv.conf")
	if err != nil || !strings.HasPrefix(target, "/run/") {
		return nil
	}

	return []string{"--ro-bind", target, target}
}
