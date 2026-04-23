// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package botcli

import (
	"sync"
	"time"
)

// Limiter is a per-sender sliding-window gate. Each sender is allowed one
// request per Window; subsequent requests within the window are denied
// without advancing the clock, so a flood of requests doesn't starve the
// first-legit request after the window elapses.
//
// Not a token bucket — the bot only needs to reject obvious abuse, and a
// single-entry per-sender timestamp is simpler + has no burst concept to
// configure wrong.
type Limiter struct {
	mu     sync.Mutex
	last   map[string]time.Time
	window time.Duration
	// now is swappable for tests; nil means time.Now.
	now func() time.Time
}

// NewLimiter returns a limiter that allows one request per sender per window.
func NewLimiter(window time.Duration) *Limiter {
	return &Limiter{
		last:   make(map[string]time.Time),
		window: window,
	}
}

// Allow reports whether sender may fire a request right now. Side-effect: on
// allow, records the time as the sender's latest fire. On deny, does not
// advance the clock — so an attacker hammering the gate can't extend their
// own cooldown past the legitimate window.
func (l *Limiter) Allow(sender string) bool {
	if sender == "" {
		return false
	}
	nowFn := l.now
	if nowFn == nil {
		nowFn = time.Now
	}
	now := nowFn()
	l.mu.Lock()
	defer l.mu.Unlock()
	if last, ok := l.last[sender]; ok {
		if now.Sub(last) < l.window {
			return false
		}
	}
	l.last[sender] = now
	return true
}
