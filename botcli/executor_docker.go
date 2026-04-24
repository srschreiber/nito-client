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
	"os/exec"
	"strings"
	"sync"
	"time"
)

// dockerRunner sandboxes script execution inside a long-lived worker
// container. Lifecycle:
//
//   - newDockerRunner: validates docker is reachable and the image
//     exists locally; doesn't start the container yet.
//   - ensure (lazy): on first Run (or after a worker crash), starts the
//     worker with the source dir bind-mounted READ-ONLY at /scripts.
//     The container's entrypoint is overridden to `tail -f /dev/null`
//     so the user's image doesn't need a long-running default command.
//   - Run: docker exec into the container with the script's env vars.
//     One container, many execs — start-up cost amortised across a long
//     bot lifetime.
//   - Close: docker rm -f the container. Called from Main on shutdown.
//
// Crucially, /data (where the bot's keys live) is NOT mounted into the
// container. Scripts can read /scripts (their own source) and that's it.
// The bot's RSA private key, the .env password, and bot-state.yml are
// invisible to the worker.
type dockerRunner struct {
	image     string
	sourceDir string
	network   bool

	mu          sync.Mutex
	containerID string
}

func newDockerRunner(w BotWorker, sourceDir string) (*dockerRunner, error) {
	// Sanity checks at startup: fail loudly here rather than on the
	// first user message.
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, fmt.Errorf("docker CLI not found in PATH — install Docker Desktop (macOS/Windows) or your distro's docker package, then re-run: %w", err)
	}
	if err := exec.Command("docker", "version").Run(); err != nil {
		return nil, fmt.Errorf("docker daemon unreachable — is Docker running? (`docker version` must succeed): %w", err)
	}
	if err := exec.Command("docker", "image", "inspect", w.Image).Run(); err != nil {
		return nil, fmt.Errorf("worker image %q not present locally — `docker pull %s` (or `docker build -t %s .`) first", w.Image, w.Image, w.Image)
	}
	return &dockerRunner{
		image:     w.Image,
		sourceDir: sourceDir,
		network:   w.NetworkEnabled(),
	}, nil
}

// ensure starts the worker container if one isn't running. Idempotent
// and re-entrant: a crashed/stopped worker is detected on the next call
// and recreated. Returns the container id to docker exec into.
func (d *dockerRunner) ensure(ctx context.Context) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.containerID != "" {
		// Cheap probe: `docker inspect -f {{.State.Running}}` returns
		// "true\n" if the container is up. Anything else (gone, stopped,
		// errored) → recreate.
		out, err := exec.CommandContext(ctx, "docker", "inspect", "-f", "{{.State.Running}}", d.containerID).Output()
		if err == nil && strings.TrimSpace(string(out)) == "true" {
			return d.containerID, nil
		}
		log.Printf("worker: container %s not running; recreating", d.containerID)
		_ = exec.Command("docker", "rm", "-f", d.containerID).Run()
		d.containerID = ""
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
	name := "nito-worker-" + randHex(6)
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
	if !d.network {
		args = append(args, "--network", "none")
	}
	args = append(args, d.image, "-f", "/dev/null")

	startCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(startCtx, "docker", args...).Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && len(exitErr.Stderr) > 0 {
			return "", fmt.Errorf("start worker container: %v: %s", err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", fmt.Errorf("start worker container: %w", err)
	}
	cid := strings.TrimSpace(string(out))
	d.containerID = cid
	log.Printf("worker: started container %s from image %s (network=%v)", cid[:12], d.image, d.network)
	return cid, nil
}

func (d *dockerRunner) Run(ctx context.Context, cmd *BotCommand, env []string) ([]byte, error) {
	cid, err := d.ensure(ctx)
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
		// Best-effort: kill the script inside the container too. `docker
		// exec` will be killed when the bot's docker CLI process dies,
		// but the script itself keeps running until docker reaps it.
		// Easiest is to recycle the container — a runaway script can't
		// hold the worker hostage past the next command.
		go d.recycle(cid)
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

// recycle force-removes the worker container. Called after a script
// timeout to prevent a runaway script from blocking subsequent commands;
// the next ensure() will spin up a fresh container.
func (d *dockerRunner) recycle(cid string) {
	d.mu.Lock()
	if d.containerID == cid {
		d.containerID = ""
	}
	d.mu.Unlock()
	_ = exec.Command("docker", "rm", "-f", cid).Run()
}

func (d *dockerRunner) Close() error {
	d.mu.Lock()
	cid := d.containerID
	d.containerID = ""
	d.mu.Unlock()
	if cid == "" {
		return nil
	}
	if err := exec.Command("docker", "rm", "-f", cid).Run(); err != nil {
		return fmt.Errorf("docker rm worker %s: %w", cid, err)
	}
	return nil
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
