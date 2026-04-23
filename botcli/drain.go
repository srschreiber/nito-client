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
				// Silently drop — these payloads would only matter to a
				// richer client. Logging at info level once per reconnect
				// is enough to spot stuck flows.
				continue
			}
			break
		}
		log.Printf("drain %s: channel closed; awaiting reconnect", name)
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
				// Late verify-response is load-bearing to log: it means
				// someone answered a challenge the bot's session thought
				// expired. Surfacing it makes misconfigured clocks /
				// laggy peers diagnosable after the fact.
				log.Printf("drain %s: late verify response from %q (ignored)", name, v)
				continue
			}
			break
		}
	}
}
