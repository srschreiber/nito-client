// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package botcli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// scriptResponse is the JSON shape every command script must produce on
// stdout. Empty (or missing) reply means "say nothing in the room" — the
// script ran successfully but chose not to respond. Non-JSON output is a
// script bug; the dispatcher logs and drops it.
type scriptResponse struct {
	Reply string `json:"reply"`
}

// runner is the strategy by which a single script invocation produces
// stdout. Two implementations:
//
//   - directRunner: exec `sh script.sh` in the bot's process namespace.
//     Quick and dependency-free, but the script can read everything the
//     bot can. Used for tests + dev.
//   - dockerRunner: docker exec into a long-lived worker container that
//     was started at bot launch from worker.image. The container only
//     sees the bind-mounted (read-only) source dir; the bot's keys/
//     state are not mounted. Used for production.
type runner interface {
	Run(ctx context.Context, cmd *BotCommand, env []string) ([]byte, error)
}

// Execute runs cmdName end to end: builds the env, invokes the runner
// (direct or docker depending on config), and parses the JSON reply.
// Returns the reply text (empty = no reply) or an error suitable for
// logging.
//
// Env discipline: NEVER inherit the bot process's env. The bot holds
// NITO_BOT_PASSWORD and other host secrets; passing them through to a
// script (sandboxed or not) defeats the whole isolation story. Scripts
// receive only the explicit NITO_* vars + REQUESTER + a baseline PATH.
func (cfg *BotConfig) Execute(ctx context.Context, cmdName, requester, args string, named map[string]string) (string, error) {
	cmd, ok := cfg.Commands[cmdName]
	if !ok {
		return "", fmt.Errorf("unknown command %q", cmdName)
	}
	env := []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"NITO_COMMAND=" + cmd.name,
		"NITO_ARGS=" + args,
		"REQUESTER=" + requester,
	}
	for name, val := range named {
		env = append(env, "NITO_ARG_"+strings.ToUpper(name)+"="+val)
	}

	runCtx, cancel := context.WithTimeout(ctx, cmd.timeout)
	defer cancel()

	out, err := cmd.runner.Run(runCtx, cmd, env)
	if err != nil {
		return "", err
	}
	out = trimUTF8BOM(out)
	if len(strings.TrimSpace(string(out))) == 0 {
		return "", nil
	}
	var resp scriptResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		return "", fmt.Errorf("script %q output not JSON {\"reply\":...}: %w (raw: %q)", cmd.name, err, string(out))
	}
	return resp.Reply, nil
}

func trimUTF8BOM(b []byte) []byte {
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		return b[3:]
	}
	return b
}
