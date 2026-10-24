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

//go:build !unix

package bash

import (
	"os"
	"os/exec"
)

// setProcessGroup does nothing where process groups are not available: only
// the command's own process is killed.
func setProcessGroup(*exec.Cmd) {}

// killProcessGroup kills proc.
func killProcessGroup(proc *os.Process) error {
	return proc.Kill()
}
