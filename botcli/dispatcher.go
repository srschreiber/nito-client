// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package botcli

import (
	"context"
	"log"
	"strings"
	"sync"
)

// Dispatcher turns a raw `!command args...` plaintext into either a reply,
// a "rate-limited" signal (so serve.go can answer "hold your horses"), or
// silence. One Dispatcher per process; rate-limit state lives here so the
// per-command sliding windows survive across messages.
type Dispatcher struct {
	cfg *BotConfig

	mu       sync.Mutex
	limiters map[string]*Limiter // command name -> per-sender limiter
}

func NewDispatcher(cfg *BotConfig) *Dispatcher {
	d := &Dispatcher{cfg: cfg, limiters: map[string]*Limiter{}}
	for name, c := range cfg.Commands {
		d.limiters[name] = NewLimiter(c.window)
	}
	return d
}

// Dispatch parses one room message and decides what to do with it.
//
// Returns:
//   - reply: text to post in the room, or "" for no reply
//   - rateLimited: true if the sender hit the per-command rate limit; the
//     caller posts "hold your horses" once per denied request
//
// Unknown commands and non-`!` messages produce ("", false). Argument-
// parsing failures produce the command's `usage` string as the reply.
func (d *Dispatcher) Dispatch(ctx context.Context, text, sender string) (reply string, rateLimited bool) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "!") {
		return "", false
	}
	cmdName := strings.TrimPrefix(firstToken(text), "!")
	cmd, ok := d.cfg.Commands[cmdName]
	if !ok {
		return "", false
	}

	d.mu.Lock()
	lim := d.limiters[cmdName]
	d.mu.Unlock()
	if !lim.Allow(sender) {
		return "", true
	}

	args := ""
	if i := strings.IndexAny(text, " \t"); i > 0 {
		args = strings.TrimSpace(text[i:])
	}

	named := map[string]string{}
	if cmd.regex != nil {
		m := cmd.regex.FindStringSubmatch(args)
		if m == nil {
			usage := cmd.Usage
			if usage == "" {
				usage = "usage: !" + cmdName
			}
			return usage, false
		}
		// m[0] is the full match; m[1..] are the capture groups.
		for i, n := range cmd.ArgNames {
			if i+1 < len(m) {
				named[n] = m[i+1]
			}
		}
	}

	out, err := d.cfg.Execute(ctx, cmdName, sender, args, named)
	if err != nil {
		log.Printf("dispatch: %s: %v", cmdName, err)
		return "", false
	}
	return out, false
}
