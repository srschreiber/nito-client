// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

//go:build !windows

package botcli

import (
	"os/exec"
	"syscall"
)

// setProcAttr puts the script into its own process group so killProcessGroup
// can take down `sh` plus any descendants (sleep, python, etc.). Without
// this, killing `sh` orphans the descendants and they keep our stdout pipe
// open — Execute hangs until the descendant naturally exits.
func setProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup signals SIGKILL to the negative pid (entire process group).
// Best-effort: a process that already exited is fine.
func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
