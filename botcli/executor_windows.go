// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

//go:build windows

package botcli

import "os/exec"

// setProcAttr is a no-op on Windows; the bot's command-script protocol
// targets POSIX sh and isn't supported on Windows in v1. The build still
// produces a binary so register/verify can be done from a Windows host.
func setProcAttr(cmd *exec.Cmd) {}

func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
