// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package botcli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeExecScript writes an executable .sh into dir and returns its path.
func writeExecScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	return p
}

// TestExecutorEnvVars: the script receives NITO_COMMAND, NITO_ARGS,
// NITO_ARG_<NAME>, and REQUESTER. We have the script echo them back as
// JSON so we can verify the exact wire format.
func TestExecutorEnvVars(t *testing.T) {
	dir := t.TempDir()
	body := `#!/bin/sh
printf '{"reply":"cmd=%s args=%s req=%s msg=%s"}' "$NITO_COMMAND" "$NITO_ARGS" "$REQUESTER" "$NITO_ARG_MESSAGE"
`
	writeExecScript(t, dir, "echo.sh", body)
	cfgPath := writeConfig(t, dir, `
commands:
  echo:
    script: echo.sh
    args_regex: "^(.+)$"
    arg_names: [message]
`)
	cfg, err := LoadConfig(cfgPath, dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	reply, err := cfg.Execute(context.Background(), "echo", "alice", "hello world", map[string]string{"message": "hello world"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := "cmd=echo args=hello world req=alice msg=hello world"
	if reply != want {
		t.Fatalf("reply mismatch:\n  got  %q\n  want %q", reply, want)
	}
}

// TestExecutorTimeout: a script that sleeps past its timeout is killed
// and surfaces a timeout error rather than blocking forever.
func TestExecutorTimeout(t *testing.T) {
	dir := t.TempDir()
	writeExecScript(t, dir, "slow.sh", "#!/bin/sh\nsleep 2\n")
	cfgPath := writeConfig(t, dir, `
defaults:
  timeout_ms: 50
commands:
  slow:
    script: slow.sh
`)
	cfg, err := LoadConfig(cfgPath, dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	start := time.Now()
	_, err = cfg.Execute(context.Background(), "slow", "alice", "", nil)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("want timeout error, got %v", err)
	}
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Fatalf("timeout took too long: %s", d)
	}
}

// TestExecutorEmptyReply: a script that prints empty stdout returns no
// reply (silent success), distinct from JSON parse failure.
func TestExecutorEmptyReply(t *testing.T) {
	dir := t.TempDir()
	writeExecScript(t, dir, "silent.sh", "#!/bin/sh\nexit 0\n")
	cfgPath := writeConfig(t, dir, `
commands:
  silent:
    script: silent.sh
`)
	cfg, err := LoadConfig(cfgPath, dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	reply, err := cfg.Execute(context.Background(), "silent", "alice", "", nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if reply != "" {
		t.Fatalf("want empty reply, got %q", reply)
	}
}

// TestExecutorBadJSON: a script that prints non-JSON returns a clear
// error rather than silently dropping the output.
func TestExecutorBadJSON(t *testing.T) {
	dir := t.TempDir()
	writeExecScript(t, dir, "broken.sh", "#!/bin/sh\necho not json\n")
	cfgPath := writeConfig(t, dir, `
commands:
  broken:
    script: broken.sh
`)
	cfg, err := LoadConfig(cfgPath, dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	_, err = cfg.Execute(context.Background(), "broken", "alice", "", nil)
	if err == nil || !strings.Contains(err.Error(), "JSON") {
		t.Fatalf("want JSON parse error, got %v", err)
	}
}

// TestExecutorEnvIsolated: the host's env is NOT inherited. We set a
// secret-ish var in the parent process and verify the script can't see
// it. This is the defense against scripts accidentally exfiltrating
// NITO_BOT_PASSWORD etc.
func TestExecutorEnvIsolated(t *testing.T) {
	t.Setenv("NITO_BOT_PASSWORD", "supersecret")
	dir := t.TempDir()
	body := `#!/bin/sh
printf '{"reply":"%s"}' "${NITO_BOT_PASSWORD:-EMPTY}"
`
	writeExecScript(t, dir, "leak.sh", body)
	cfgPath := writeConfig(t, dir, `
commands:
  leak:
    script: leak.sh
`)
	cfg, err := LoadConfig(cfgPath, dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	reply, err := cfg.Execute(context.Background(), "leak", "alice", "", nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if reply != "EMPTY" {
		t.Fatalf("script saw NITO_BOT_PASSWORD — env should be isolated; got %q", reply)
	}
}
