// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package botcli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/srschreiber/nito-client/engine/connection"
	"github.com/srschreiber/nito-client/engine/keys"
)

// Main is the bot's process entry point. configPath is the YAML command
// table (-f); sourceDir is the directory those scripts live under (-s).
// The state file is still authoritative for "what step are we in"; the
// config controls "what commands do we serve once we're ready."
//
// We load and validate the config eagerly — a malformed config or a
// missing script aborts before we touch the broker, which keeps the
// failure message visible at the terminal instead of buried in serve-loop
// logs.
func Main(configPath, sourceDir string) int {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.SetPrefix("[nito-bot] ")

	cfg, err := LoadConfig(configPath, sourceDir)
	if err != nil {
		log.Printf("config load failed: %v", err)
		return 1
	}
	defer func() {
		if err := cfg.Close(); err != nil {
			log.Printf("worker shutdown: %v", err)
		}
	}()
	if cfg.HasSandbox() {
		log.Printf("loaded %d commands from %s (source=%s, all sandboxed)", len(cfg.Commands), configPath, sourceDir)
	} else {
		log.Printf("loaded %d commands from %s (source=%s)", len(cfg.Commands), configPath, sourceDir)
		if !confirmUnsandboxed(cfg.UnsandboxedCommands()) {
			return 1
		}
	}

	// Pull values persisted by the wizard (NITO_BOT_PASSWORD) into the
	// process env before any step that needs them. Process env always
	// wins over the file, so a docker run -e flag still takes precedence.
	if err := LoadEnvFile(); err != nil {
		log.Printf("warn: could not read bot .env (%v); continuing", err)
	}
	// Also pull any `.env` sitting next to bot.yml — this is where
	// operators stash per-command secrets (OPENAI_API_KEY, MOTTO, etc.)
	// without having to export them on the host. Same precedence: any
	// var already in the process env wins.
	configEnv := filepath.Join(filepath.Dir(configPath), ".env")
	if err := LoadEnvFileFrom(configEnv); err != nil {
		log.Printf("warn: could not read %s (%v); continuing", configEnv, err)
	}

	// Root ctx tied to SIGINT/SIGTERM so the Docker stop signal tears down
	// WebSocket + pending RPCs cleanly.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	state, err := LoadState()
	if err != nil {
		log.Printf("state load failed: %v", err)
		return 1
	}

	// Wizard step: interactive only. If we somehow reach this without a
	// TTY (e.g. docker run -d on first launch), fail loudly rather than
	// hang waiting for input that will never arrive.
	if state.Step == StepFresh {
		if !stdinIsTTY() {
			log.Printf("first-run setup needs an interactive terminal — re-run with `docker run -it` (or attach stdin)")
			return 1
		}
		newState, err := runWizard(ctx)
		if err != nil {
			log.Printf("wizard failed: %v", err)
			return 1
		}
		state = newState
	}

	// From registered onward every step eventually needs a WS session, so
	// log in + connect once and let the reconnect loop own the lifecycle
	// for the rest of the process.
	keys.SetActiveBroker(state.Broker)
	if err := loginAndConnect(ctx, state); err != nil {
		// A bad password on startup is terminal — no point retrying on a
		// loop because the user must fix the env var. Connection errors
		// (network blip, broker down) are not terminal; loop handles them.
		if errors.Is(err, errBadCredentials) {
			log.Printf("login refused by broker: %v", err)
			return 1
		}
		log.Printf("initial connect failed: %v (will retry)", err)
	}

	// Drain the channels the bot never consumes so readLoop never blocks
	// on a full channel and drops real messages. Started once — they
	// survive reconnects because the underlying channel accessors return
	// a fresh channel each time.
	go drainLoop(ctx, connection.KeyVerifyChallengeChan, "key-verify-challenge")
	go drainLoop(ctx, connection.KeyVerifyConfirmChan, "key-verify-confirm")
	go drainStringLoop(ctx, connection.LateVerifyResponseChan, "late-verify-response")
	go drainLoop(ctx, connection.DMChan, "direct-message")

	// Verify step: bot must have exactly one owner. The prompt picks who;
	// the handshake runs headlessly after the model quits.
	if state.Step == StepRegistered {
		if !stdinIsTTY() {
			log.Printf("verify step needs an interactive terminal to pick the owner — attach a TTY")
			return 1
		}
		newState, err := runVerifyStep(ctx, state)
		if err != nil {
			log.Printf("verify failed: %v", err)
			return 1
		}
		state = newState
	}

	rt := NewRuntime(state)

	// Invite step: headless loop that accepts the first invite from the
	// verified owner and transitions to StepReady. Subsequent invites are
	// accepted in the background by inviteAcceptLoop inside runServe.
	if state.Step == StepVerified {
		if err := runFirstInviteWait(ctx, rt); err != nil {
			log.Printf("invite wait ended: %v", err)
			return 1
		}
	}

	dispatcher := NewDispatcher(cfg)

	// Ready: serve forever. Only returns on ctx cancel.
	if err := runServe(ctx, rt, dispatcher); err != nil {
		log.Printf("serve ended: %v", err)
		return 1
	}
	return 0
}

// errBadCredentials distinguishes "your password is wrong" from "the broker
// is unreachable" at startup. Only the former is terminal.
var errBadCredentials = errors.New("bad credentials")

// loginAndConnect performs the HTTP challenge-response dance then opens the
// WebSocket. The private key was generated during the wizard; here we only
// load the password from env.
func loginAndConnect(ctx context.Context, state BotState) error {
	password := os.Getenv("NITO_BOT_PASSWORD")
	if password == "" {
		return fmt.Errorf("NITO_BOT_PASSWORD is not set — populate %s or pass --env-file", mustEnvPath())
	}
	token, err := connection.Login(ctx, state.Broker, state.Username, password)
	if err != nil {
		// The broker returns 401 on bad password; the error text contains
		// the status code. Treat anything containing 401 as terminal.
		if isAuthError(err) {
			return fmt.Errorf("%w: %v", errBadCredentials, err)
		}
		return fmt.Errorf("login: %w", err)
	}
	if err := connection.Connect(ctx, state.Broker, state.Username, token); err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	log.Printf("connected to %s as %q", state.Broker, state.Username)
	return nil
}

func isAuthError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return contains(s, "401") || contains(s, "403")
}

func contains(hay, needle string) bool {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if hay[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

func mustEnvPath() string {
	p, err := EnvFilePath()
	if err != nil {
		return "$NITO_BOT_DATA/.env"
	}
	return p
}

// Short-wait helper so the reconnect loop doesn't spin when ctx dies mid-sleep.
func sleepCtx(ctx context.Context, d time.Duration) error {
	select {
	case <-time.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// confirmUnsandboxed prints a loud warning + a y/N prompt when one or
// more commands have no resolved image. Returns true if the operator
// explicitly typed y (or yes). No TTY means no human to ACK, so we
// refuse to start — this is what blocks someone from silently shipping
// an unsandboxed bot to prod under `docker run -d`.
func confirmUnsandboxed(unsandboxed []string) bool {
	bar := strings.Repeat("━", 70)
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, bar)
	fmt.Fprintln(os.Stderr, "  SECURITY WARNING: some commands have no image configured")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "  Unsandboxed commands: %s\n", strings.Join(unsandboxed, ", "))
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "  These scripts will run in the bot's own process namespace.")
	fmt.Fprintln(os.Stderr, "  They CAN read the bot's RSA private key, .env password, and")
	fmt.Fprintln(os.Stderr, "  bot-state.yml. A malicious or buggy script can impersonate")
	fmt.Fprintln(os.Stderr, "  the bot to the broker.")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "  Fix: set defaults.image in bot.yml (applies to all commands)")
	fmt.Fprintln(os.Stderr, "  or per-command image: on each affected command. See BOTS.md")
	fmt.Fprintln(os.Stderr, "  §Sandbox model.")
	fmt.Fprintln(os.Stderr, bar)
	fmt.Fprintln(os.Stderr)

	if !stdinIsTTY() {
		log.Printf("aborting: no TTY to confirm unsandboxed mode. Set worker.image (or re-run with a TTY attached).")
		return false
	}
	fmt.Fprint(os.Stderr, "continue without sandbox? [y/N]: ")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		log.Printf("aborting: could not read confirmation (%v)", err)
		return false
	}
	ans := strings.ToLower(strings.TrimSpace(line))
	if ans != "y" && ans != "yes" {
		log.Printf("aborted by operator (answered %q)", ans)
		return false
	}
	log.Printf("proceeding without sandbox (operator confirmed)")
	return true
}
