// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package botcli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeScript creates a placeholder .sh in dir so the LoadConfig stat
// check passes. Content is irrelevant for config tests; ExecutorTest
// uses real script bodies.
func writeScript(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("write script: %v", err)
	}
}

func writeConfig(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, "bot.yml")
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// TestConfigLoadValid: a happy-path config with one defaulted command and
// one fully-overridden command loads cleanly. The defaults propagate; the
// overrides win where present; rate-limit window is derived from rps.
func TestConfigLoadValid(t *testing.T) {
	dir := t.TempDir()
	writeScript(t, dir, "hello.sh")
	writeScript(t, dir, "ask.sh")
	cfgPath := writeConfig(t, dir, `
defaults:
  rate_limit_rps: 1
  timeout_ms: 1000

commands:
  hello:
    script: hello.sh
    usage: "!hello"

  ask:
    script: ask.sh
    usage: "!ask <message>"
    args_regex: "^(.+)$"
    arg_names: [message]
    rate_limit_rps: 0.5
    timeout_ms: 2000
`)
	cfg, err := LoadConfig(cfgPath, dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(cfg.Commands) != 2 {
		t.Fatalf("want 2 commands, got %d", len(cfg.Commands))
	}
	hello := cfg.Commands["hello"]
	if hello.window != time.Second {
		t.Errorf("hello window: want 1s (default rps=1), got %s", hello.window)
	}
	if hello.timeout != time.Second {
		t.Errorf("hello timeout: want 1s (default), got %s", hello.timeout)
	}
	if hello.regex != nil {
		t.Errorf("hello: no regex configured but got non-nil")
	}
	ask := cfg.Commands["ask"]
	if ask.window != 2*time.Second {
		t.Errorf("ask window: want 2s (rps=0.5), got %s", ask.window)
	}
	if ask.timeout != 2*time.Second {
		t.Errorf("ask timeout: want 2s, got %s", ask.timeout)
	}
	if ask.regex == nil {
		t.Errorf("ask: regex should be compiled")
	}
}

// TestConfigRejectDotDot: any `..` segment in a script path is refused
// before the file stat happens — this is the path-traversal guard.
func TestConfigRejectDotDot(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeConfig(t, dir, `
commands:
  bad:
    script: ../etc/passwd.sh
`)
	_, err := LoadConfig(cfgPath, dir)
	if err == nil || !strings.Contains(err.Error(), "..") {
		t.Fatalf("want '..' rejection, got %v", err)
	}
}

// TestConfigRejectAbsolutePath: absolute script paths bypass the source
// dir scope, so they're refused.
func TestConfigRejectAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeConfig(t, dir, `
commands:
  bad:
    script: /tmp/escape.sh
`)
	_, err := LoadConfig(cfgPath, dir)
	if err == nil || !strings.Contains(err.Error(), "relative") {
		t.Fatalf("want absolute-path rejection, got %v", err)
	}
}

// TestConfigRejectNonSh: only .sh entry points are allowed. Scripts can
// of course exec python/node/etc internally — this gate is just to
// prevent the bot from invoking arbitrary binary types.
func TestConfigRejectNonSh(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "evil.py"), []byte("print()"), 0755); err != nil {
		t.Fatalf("write py: %v", err)
	}
	cfgPath := writeConfig(t, dir, `
commands:
  bad:
    script: evil.py
`)
	_, err := LoadConfig(cfgPath, dir)
	if err == nil || !strings.Contains(err.Error(), ".sh") {
		t.Fatalf("want .sh rejection, got %v", err)
	}
}

// TestConfigRejectInvalidRegex: a bad regex is caught at config load,
// not on first message — surfaces problems at startup where the operator
// will see them.
func TestConfigRejectInvalidRegex(t *testing.T) {
	dir := t.TempDir()
	writeScript(t, dir, "bad.sh")
	cfgPath := writeConfig(t, dir, `
commands:
  bad:
    script: bad.sh
    args_regex: "(unterminated"
`)
	_, err := LoadConfig(cfgPath, dir)
	if err == nil || !strings.Contains(err.Error(), "args_regex") {
		t.Fatalf("want regex compile error, got %v", err)
	}
}

// TestConfigArgNameCountMismatch: arg_names length must match the regex
// capture-group count exactly. Off-by-one would produce silently-empty
// env vars otherwise.
func TestConfigArgNameCountMismatch(t *testing.T) {
	dir := t.TempDir()
	writeScript(t, dir, "x.sh")
	cfgPath := writeConfig(t, dir, `
commands:
  ask:
    script: x.sh
    args_regex: "^(\\w+)$"
    arg_names: [first, second]
`)
	_, err := LoadConfig(cfgPath, dir)
	if err == nil || !strings.Contains(err.Error(), "capture groups") {
		t.Fatalf("want capture-count mismatch error, got %v", err)
	}
}

// TestConfigArgNamesWithoutRegex: arg_names is meaningless without an
// args_regex to populate them; refuse rather than silently ignore.
func TestConfigArgNamesWithoutRegex(t *testing.T) {
	dir := t.TempDir()
	writeScript(t, dir, "x.sh")
	cfgPath := writeConfig(t, dir, `
commands:
  ask:
    script: x.sh
    arg_names: [foo]
`)
	_, err := LoadConfig(cfgPath, dir)
	if err == nil || !strings.Contains(err.Error(), "args_regex") {
		t.Fatalf("want arg_names-without-regex rejection, got %v", err)
	}
}

// TestConfigEmptyCommands: a config with no commands defined is a
// loading-time error — running a bot that responds to nothing is almost
// always a misconfiguration.
func TestConfigEmptyCommands(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeConfig(t, dir, `defaults: {}`)
	_, err := LoadConfig(cfgPath, dir)
	if err == nil || !strings.Contains(err.Error(), "no commands") {
		t.Fatalf("want no-commands rejection, got %v", err)
	}
}
