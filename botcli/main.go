// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package botcli

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/srschreiber/nito-client/engine/connection"
	"github.com/srschreiber/nito-client/engine/keys"
)

// Main is the bot's process entry point. It selects the next action from the
// persisted state, drives interactive prompts through bubbletea when needed,
// and enters the headless serve loop once bootstrap is complete.
//
// The same binary handles every phase: the caller doesn't pass flags
// describing "what to do" — the state file is authoritative. Users who want
// to start over delete ~/.nito-bot (or the mounted /data volume) and re-run.
func Main() int {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.SetPrefix("[nito-bot] ")

	// Pull values persisted by the wizard (NITO_BOT_PASSWORD) into the
	// process env before any step that needs them. Process env always
	// wins over the file, so a docker run -e flag still takes precedence.
	if err := LoadEnvFile(); err != nil {
		log.Printf("warn: could not read bot .env (%v); continuing", err)
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

	// Invite step: headless loop that accepts the first invite from the
	// verified owner and persists the room ID.
	if state.Step == StepVerified {
		newState, err := runInviteWait(ctx, state)
		if err != nil {
			log.Printf("invite wait ended: %v", err)
			return 1
		}
		state = newState
	}

	// Ready: serve forever. Only returns on ctx cancel.
	if err := runServe(ctx, state); err != nil {
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
