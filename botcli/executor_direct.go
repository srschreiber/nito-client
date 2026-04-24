// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package botcli

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// directRunner exec's `sh <abs_script_path>` directly on the host. Fast
// (no docker round-trip) but offers no isolation from the bot's own
// process — a malicious script can read the bot's keys and state. For
// tests and quick-iteration dev only; production should use dockerRunner.
type directRunner struct{}

func (directRunner) Run(ctx context.Context, cmd *BotCommand, env []string) ([]byte, error) {
	if cmd.scriptPath == "" {
		return nil, fmt.Errorf("command %q not loaded", cmd.name)
	}
	// Don't use exec.CommandContext: it only kills the direct child
	// (`sh`), leaving descendants (sleep/python/etc.) holding our
	// stdout pipe open — Wait() then blocks until the orphan exits.
	// setProcAttr puts the child in its own pgid; killProcessGroup
	// nukes the whole tree on timeout.
	c := exec.Command("sh", cmd.scriptPath)
	setProcAttr(c)
	c.Env = env

	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr

	if err := c.Start(); err != nil {
		return nil, fmt.Errorf("script %q start: %w", cmd.name, err)
	}
	done := make(chan error, 1)
	go func() { done <- c.Wait() }()

	select {
	case <-ctx.Done():
		killProcessGroup(c)
		<-done
		return nil, fmt.Errorf("script %q timed out after %s", cmd.name, cmd.timeout)
	case err := <-done:
		if err != nil {
			es := strings.TrimSpace(stderr.String())
			if es != "" {
				return nil, fmt.Errorf("script %q failed: %v: %s", cmd.name, err, es)
			}
			return nil, fmt.Errorf("script %q failed: %w", cmd.name, err)
		}
	}
	return stdout.Bytes(), nil
}
