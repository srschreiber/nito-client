// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package botcli

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// dockerRunner sandboxes script execution inside per-command worker
// containers. Each command in bot.yml gets its own long-lived container
// started lazily on first invocation. This is what guarantees per-
// command env-var isolation: a host secret routed into command A's
// container via `env:` is invisible to command B's scripts because
// command B is a separate container.
//
// Lifecycle per command:
//
//   - newDockerRunner: validates docker is reachable and the image
//     exists locally; doesn't start any containers yet.
//   - ensure (lazy, per command): on first Run for that command (or
//     after a crash), starts the worker with the source dir bind-
//     mounted READ-ONLY at /scripts and the per-command env vars
//     baked in. The container's entrypoint is overridden to
//     `tail -f /dev/null` so the user's image doesn't need a long-
//     running default command.
//   - Run: docker exec into the command's container with per-call
//     env vars (NITO_*, REQUESTER). One container per command, many
//     execs per container — startup cost amortised.
//   - Close: docker rm -f every container we spawned. Called from
//     Main on shutdown.
//
// Crucially, /data (where the bot's keys live) is NEVER mounted into
// any container. Scripts can read /scripts (their own source) and
// that's it.
type dockerRunner struct {
	image     string
	sourceDir string

	mu         sync.Mutex
	containers map[string]string // command name -> container id
}

func newDockerRunner(image, sourceDir string) (*dockerRunner, error) {
	// Sanity checks at startup: fail loudly here rather than on the
	// first user message.
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, fmt.Errorf("docker CLI not found in PATH — install Docker Desktop (macOS/Windows) or your distro's docker package, then re-run: %w", err)
	}
	if err := exec.Command("docker", "version").Run(); err != nil {
		return nil, fmt.Errorf("docker daemon unreachable — is Docker running? (`docker version` must succeed): %w", err)
	}
	if err := exec.Command("docker", "image", "inspect", image).Run(); err != nil {
		return nil, fmt.Errorf("worker image %q not present locally — `docker pull %s` (or `docker build -t %s .`) first", image, image, image)
	}
	return &dockerRunner{
		image:      image,
		sourceDir:  sourceDir,
		containers: map[string]string{},
	}, nil
}

// ensure starts a worker container for cmd if one isn't running for it.
// Idempotent and re-entrant: a crashed/stopped worker is detected on
// the next call and recreated. Returns the container id to docker exec
// into.
//
// The per-command env vars (cmd.Env) are baked in at start time via
// `docker run -e NAME=<value-from-bot-process-env>`, so they live in
// the container's environment for every exec'd process. Vars not set
// in the bot's env are passed as the empty string (still scoped to
// only this command).
func (d *dockerRunner) ensure(ctx context.Context, cmd *BotCommand) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if cid, ok := d.containers[cmd.name]; ok {
		// Cheap probe: `docker inspect -f {{.State.Running}}` returns
		// "true\n" if the container is up. Anything else (gone, stopped,
		// errored) → recreate.
		out, err := exec.CommandContext(ctx, "docker", "inspect", "-f", "{{.State.Running}}", cid).Output()
		if err == nil && strings.TrimSpace(string(out)) == "true" {
			return cid, nil
		}
		log.Printf("worker[%s]: container %s not running; recreating", cmd.name, cid)
		_ = exec.Command("docker", "rm", "-f", cid).Run()
		delete(d.containers, cmd.name)
	}

	// Lockdown defaults (all hard-coded so an operator can't loosen them
	// without editing source):
	//   --read-only       root fs is RO; only /tmp is writable
	//   --tmpfs /tmp:...  64M scratch space, mode 1777 like a real /tmp
	//   -v ...:/scripts:ro  source dir is bind-mounted read-only
	//   --cap-drop ALL    no Linux capabilities — scripts run unprivileged
	//   --security-opt no-new-privileges  even if the script setuid's, it can't escalate
	//   --pids-limit 256  kills runaway forkbombs
	// /data is intentionally NOT mounted: bot keys + .env stay invisible.
	name := "nito-worker-" + cmd.name + "-" + randHex(4)
	args := []string{
		"run", "-d", "--rm",
		"--name", name,
		"-v", d.sourceDir + ":/scripts:ro",
		"-w", "/scripts",
		"--read-only",
		"--tmpfs", "/tmp:rw,nosuid,nodev,size=64m,mode=1777",
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		"--pids-limit", "256",
		"--entrypoint", "tail",
	}
	if !cmd.NetworkEnabled() {
		args = append(args, "--network", "none")
	}
	// Bake per-command env vars into the container's env at start
	// time. These are scoped to ONLY this container — other commands'
	// containers don't see them, so a stolen API key from a buggy
	// `ask` script can never leak into the `motto` script.
	//
	// LookupEnv gate: passing `-e VAR=` (with empty value) overrides
	// any ENV the worker image has baked in. We only want to override
	// when the operator explicitly set the var; otherwise let the
	// image's ENV provide the value. This is the principle of least
	// surprise — `env: ["MOTTO"]` plus `ENV MOTTO=...` in the
	// Dockerfile should leave MOTTO populated.
	for _, ev := range cmd.Env {
		if val, ok := os.LookupEnv(ev); ok {
			args = append(args, "-e", ev+"="+val)
		}
	}
	args = append(args, d.image, "-f", "/dev/null")

	startCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(startCtx, "docker", args...).Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && len(exitErr.Stderr) > 0 {
			return "", fmt.Errorf("start worker container for %s: %v: %s", cmd.name, err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", fmt.Errorf("start worker container for %s: %w", cmd.name, err)
	}
	cid := strings.TrimSpace(string(out))
	d.containers[cmd.name] = cid
	log.Printf("worker[%s]: started container %s from image %s (network=%v, env=%v)", cmd.name, cid[:12], d.image, cmd.NetworkEnabled(), cmd.Env)
	return cid, nil
}

func (d *dockerRunner) Run(ctx context.Context, cmd *BotCommand, env []string) ([]byte, error) {
	cid, err := d.ensure(ctx, cmd)
	if err != nil {
		return nil, err
	}

	args := []string{"exec", "-i"}
	for _, e := range env {
		args = append(args, "-e", e)
	}
	args = append(args, cid, "sh", "/scripts/"+cmd.scriptRel)

	c := exec.Command("docker", args...)
	setProcAttr(c)

	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr

	if err := c.Start(); err != nil {
		return nil, fmt.Errorf("docker exec start: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- c.Wait() }()

	select {
	case <-ctx.Done():
		killProcessGroup(c)
		<-done
		// Best-effort: `docker exec` will be killed when the bot's
		// docker CLI process dies, but the script itself keeps running
		// until docker reaps it. Recycle the container so a runaway
		// script can't hold this command's worker hostage past the
		// next call.
		go d.recycle(cmd.name, cid)
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

// recycle force-removes a single command's worker container. Called
// after a script timeout to prevent a runaway script from blocking
// subsequent calls to that same command; the next ensure() will spin
// up a fresh container.
func (d *dockerRunner) recycle(cmdName, cid string) {
	d.mu.Lock()
	if d.containers[cmdName] == cid {
		delete(d.containers, cmdName)
	}
	d.mu.Unlock()
	_ = exec.Command("docker", "rm", "-f", cid).Run()
}

// Close removes every per-command worker container. Best-effort: any
// individual remove failure is logged into the returned error but we
// keep going so a single dead container doesn't leave the rest
// dangling.
func (d *dockerRunner) Close() error {
	d.mu.Lock()
	cids := make(map[string]string, len(d.containers))
	for k, v := range d.containers {
		cids[k] = v
	}
	d.containers = map[string]string{}
	d.mu.Unlock()

	var firstErr error
	for cmdName, cid := range cids {
		if err := exec.Command("docker", "rm", "-f", cid).Run(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("docker rm worker[%s] %s: %w", cmdName, cid, err)
		}
	}
	return firstErr
}

func randHex(nBytes int) string {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		// Fall back to a timestamp; collision is unlikely given the
		// short bot lifetime + container --name uniqueness check.
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
