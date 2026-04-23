// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package botcli

import (
	"context"
	"log"
	"time"
)

// drainLoop / drainStringLoop exist to consume channels the bot has no
// interest in, so connection.readLoop never blocks on a full channel and
// drop real messages it does care about. The engine exposes several
// side-channels (key-verify challenges inbound, DMs, etc.) that the bot
// intentionally ignores — we still have to empty them.
//
// Each drain handles channel close transparently: the underlying accessor
// returns a fresh channel after reconnect, so we re-fetch on each outer
// iteration.

func drainLoop(ctx context.Context, accessor func() <-chan []byte, name string) {
	for ctx.Err() == nil {
		ch := accessor()
		if ch == nil {
			if err := sleepCtx(ctx, 500*time.Millisecond); err != nil {
				return
			}
			continue
		}
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-ch:
				if !ok {
					// Channel closed on disconnect; outer loop re-fetches.
					break
				}
				continue
			}
			break
		}
		// Sleep before re-fetching; without this the loop spins at full
		// speed returning the same closed channel until reconnect creates
		// a new one (up to 10 s), producing hundreds of log lines.
		log.Printf("drain %s: channel closed; awaiting reconnect", name)
		if err := sleepCtx(ctx, 500*time.Millisecond); err != nil {
			return
		}
	}
}

func drainStringLoop(ctx context.Context, accessor func() <-chan string, name string) {
	for ctx.Err() == nil {
		ch := accessor()
		if ch == nil {
			if err := sleepCtx(ctx, 500*time.Millisecond); err != nil {
				return
			}
			continue
		}
		for {
			select {
			case <-ctx.Done():
				return
			case v, ok := <-ch:
				if !ok {
					break
				}
				log.Printf("drain %s: late verify response from %q (ignored)", name, v)
				continue
			}
			break
		}
		if err := sleepCtx(ctx, 500*time.Millisecond); err != nil {
			return
		}
	}
}
