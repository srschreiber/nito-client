// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package botcli

import (
	"context"
	"errors"
	"log"
	"os"
	"time"

	"github.com/srschreiber/nito-client/engine/connection"
)

// reconnectInterval is the fixed backoff between reconnect attempts. The user
// explicitly asked for "every 10 seconds" — not exponential, not jittered —
// so we keep it simple. If the broker is down for hours the bot will still
// probe politely once every 10s; no thundering-herd risk because the bot is
// a singleton per room.
const reconnectInterval = 10 * time.Second

// reconnectLoop watches the connection health and reconnects on failure. It
// runs in the background for the invite-wait and ready phases — the wizard
// and verify phases don't need it because they each only make a single
// round-trip before the normal flow resumes.
//
// Detection: we piggyback on connection.IsConnected + a periodic ping. The
// engine's readLoop also signals disconnect by closing RoomMessageChan /
// NotifChan, but those channels aren't universally watched, and the bot may
// be between messages for long stretches — polling is cheaper in code and
// catches half-open sockets that never deliver another frame.
func reconnectLoop(ctx context.Context, state BotState) {
	// Give the initial connect from Main() a moment to settle before the
	// first health probe fires; otherwise the first tick would race with
	// the dial.
	if err := sleepCtx(ctx, reconnectInterval); err != nil {
		return
	}
	tick := time.NewTicker(reconnectInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if connection.IsConnected() {
				continue
			}
			log.Printf("reconnect: session down; attempting reconnect")
			if err := reconnectOnce(ctx, state); err != nil {
				log.Printf("reconnect: failed (%v) — will retry in %s", err, reconnectInterval)
				continue
			}
			log.Printf("reconnect: session restored")
		}
	}
}

// reconnectOnce performs a single reconnect attempt. Uses the engine's
// stored credentials from the initial Connect if they're still good
// (fast path, no password round-trip); on 401/403 it falls back to a full
// re-login so an expired JWT doesn't permanently brick a long-running bot.
func reconnectOnce(ctx context.Context, state BotState) error {
	// Give each attempt a generous budget but don't let it hang forever;
	// the outer ticker will fire again anyway.
	attemptCtx, cancel := context.WithTimeout(ctx, reconnectInterval)
	defer cancel()

	if err := connection.Reconnect(attemptCtx); err == nil {
		return nil
	} else if !isAuthError(err) && !errors.Is(err, errNoCreds(err)) {
		// Unrecoverable? Retry via full login regardless — the bot's
		// correctness doesn't depend on distinguishing "JWT expired"
		// from "broker restarted and lost our session".
		log.Printf("reconnect: fast path failed (%v); trying full login", err)
	}

	password := os.Getenv("NITO_BOT_PASSWORD")
	if password == "" {
		return errors.New("reconnect: NITO_BOT_PASSWORD is not set")
	}
	token, err := connection.Login(attemptCtx, state.Broker, state.Username, password)
	if err != nil {
		return err
	}
	return connection.Connect(attemptCtx, state.Broker, state.Username, token)
}

// errNoCreds is a thin predicate that avoids importing the engine's internal
// sentinel. connection.Reconnect returns a plain errors.New("no prior
// connection credentials") on a cold path; we match by substring rather
// than identity because the engine doesn't export the value.
func errNoCreds(err error) error {
	if err != nil && contains(err.Error(), "no prior connection credentials") {
		return err
	}
	return nil
}
