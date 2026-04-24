// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package botcli

import (
	"context"
	"strings"
	"testing"
)

// dispatcherFromYAML is a tiny helper for dispatcher tests that also
// exercises LoadConfig, since the two are paired in production.
func dispatcherFromYAML(t *testing.T, yaml string) *Dispatcher {
	t.Helper()
	dir := t.TempDir()
	// Provide whatever scripts the YAML mentions; tests below use only
	// echo-style scripts so we ship a single one.
	writeExecScript(t, dir, "echo.sh", `#!/bin/sh
printf '{"reply":"%s says: %s"}' "$REQUESTER" "$NITO_ARGS"
`)
	writeExecScript(t, dir, "named.sh", `#!/bin/sh
printf '{"reply":"got message=%s"}' "$NITO_ARG_MESSAGE"
`)
	path := writeConfig(t, dir, yaml)
	cfg, err := LoadConfig(path, dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	return NewDispatcher(cfg)
}

// TestDispatchUnknownCommand: silently drops `!`-prefixed text whose
// command isn't in the table. This keeps unrelated bots in the same room
// from talking past each other.
func TestDispatchUnknownCommand(t *testing.T) {
	d := dispatcherFromYAML(t, `
commands:
  hello:
    script: echo.sh
`)
	reply, rl := d.Dispatch(context.Background(), "!unknown stuff", "alice")
	if reply != "" || rl {
		t.Fatalf("want silent drop, got reply=%q rateLimited=%v", reply, rl)
	}
}

// TestDispatchNonBangIgnored: chat that doesn't start with `!` is not
// our problem — return immediately.
func TestDispatchNonBangIgnored(t *testing.T) {
	d := dispatcherFromYAML(t, `
commands:
  hello:
    script: echo.sh
`)
	reply, rl := d.Dispatch(context.Background(), "just chatting", "alice")
	if reply != "" || rl {
		t.Fatalf("want silent ignore, got reply=%q rateLimited=%v", reply, rl)
	}
}

// TestDispatchExecutesScript: a known `!hello` command runs the script
// and returns its parsed reply. Args are everything after the command
// token.
func TestDispatchExecutesScript(t *testing.T) {
	d := dispatcherFromYAML(t, `
commands:
  hello:
    script: echo.sh
`)
	reply, rl := d.Dispatch(context.Background(), "!hello world", "alice")
	if rl {
		t.Fatalf("unexpected rate limit")
	}
	if reply != "alice says: world" {
		t.Fatalf("unexpected reply: %q", reply)
	}
}

// TestDispatchUsageOnArgsMismatch: when the args don't match
// args_regex, the bot replies with the configured `usage` string instead
// of running the script. This is the friendly "wrong syntax" UX.
func TestDispatchUsageOnArgsMismatch(t *testing.T) {
	d := dispatcherFromYAML(t, `
commands:
  ask:
    script: named.sh
    usage: "usage: !ask <message>"
    args_regex: "^(.+)$"
    arg_names: [message]
`)
	reply, rl := d.Dispatch(context.Background(), "!ask", "alice")
	if rl {
		t.Fatalf("unexpected rate limit")
	}
	if reply != "usage: !ask <message>" {
		t.Fatalf("want usage reply, got %q", reply)
	}
}

// TestDispatchNamedArgsPopulated: when args match the regex, the named
// captures land in the env as NITO_ARG_<NAME>.
func TestDispatchNamedArgsPopulated(t *testing.T) {
	d := dispatcherFromYAML(t, `
commands:
  ask:
    script: named.sh
    args_regex: "^(.+)$"
    arg_names: [message]
`)
	reply, rl := d.Dispatch(context.Background(), "!ask why is the sky blue", "alice")
	if rl {
		t.Fatalf("unexpected rate limit")
	}
	if !strings.Contains(reply, "why is the sky blue") {
		t.Fatalf("named arg not propagated; got %q", reply)
	}
}

// TestDispatchRateLimitPerCommand: each command's limiter is independent.
// Same sender hitting two different commands isn't gated, but hitting
// the same command twice in the window is.
func TestDispatchRateLimitPerCommand(t *testing.T) {
	d := dispatcherFromYAML(t, `
defaults:
  rate_limit_rps: 1000
commands:
  hello:
    script: echo.sh
    rate_limit_rps: 0.001
  ask:
    script: echo.sh
`)
	if _, rl := d.Dispatch(context.Background(), "!hello first", "alice"); rl {
		t.Fatalf("first !hello shouldn't be rate-limited")
	}
	if _, rl := d.Dispatch(context.Background(), "!hello second", "alice"); !rl {
		t.Fatalf("second !hello should be rate-limited (rps=0.001 → ~1000s window)")
	}
	// Different command: independent limiter, allowed.
	if _, rl := d.Dispatch(context.Background(), "!ask anything", "alice"); rl {
		t.Fatalf("!ask should not share !hello's limiter")
	}
}
