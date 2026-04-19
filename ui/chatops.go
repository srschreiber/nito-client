// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package main

// Chat ops are slash-prefixed commands typed into the chat input (e.g.
// `/giphy fist bump`). The input panel delegates command detection, name
// completion, and execution here so new ops can be added in one place.
//
// Each op implements Exec: given the raw argument string and a ChatOpHost it
// does whatever work it needs (HTTP calls, etc.) and calls back through the
// host to send a chat message or surface an error to the user. The host
// abstraction keeps ops free of Fyne / window / thread concerns.

import (
	"strings"
	"unicode"

	"github.com/srschreiber/nito-client/engine/connection"
	apitypes "github.com/srschreiber/nito-client/shared/api_types"
)

// ChatOpHost is the surface a chatop uses to talk back to the UI. The
// implementation is responsible for marshalling onto the Fyne thread; ops
// freely call these from background goroutines.
type ChatOpHost interface {
	// SendMessage routes text through the chat input's normal submit path so
	// it's encrypted, sent to the broker, and rendered to every peer. Used by
	// ops that post content (e.g. /giphy posts the GIF URL).
	SendMessage(text string)
	// ShowError surfaces a human-readable error (e.g. as a toast).
	ShowError(msg string)
}

type ChatOp struct {
	Name string // without the leading '/'
	Desc string // shown in autocomplete list
	Exec func(args string, host ChatOpHost)
}

// chatOps is the ordered, lowercase registry of supported commands.
// Autocomplete preserves this ordering.
var chatOps = []ChatOp{
	{Name: "giphy", Desc: "post a GIF from GIPHY for <search text>", Exec: giphyChatOp},
}

// IsChatOp returns true if text (ignoring leading whitespace) starts with '/'.
func IsChatOp(text string) bool {
	s := strings.TrimLeftFunc(text, unicode.IsSpace)
	return strings.HasPrefix(s, "/")
}

// ChatOpCompletions returns ops whose names begin with prefix. Prefix should
// be the command name without the leading '/'. An empty prefix returns all
// registered ops (i.e. user typed only '/').
func ChatOpCompletions(prefix string) []ChatOp {
	p := strings.ToLower(prefix)
	out := make([]ChatOp, 0, len(chatOps))
	for _, op := range chatOps {
		if strings.HasPrefix(op.Name, p) {
			out = append(out, op)
		}
	}
	return out
}

// ExecChatOp parses text like "/giphy hello world" into (name, args) and runs
// the matching op. If the op name isn't recognised, surfaces an error through
// host. Ops may run work in goroutines — this function returns immediately.
func ExecChatOp(text string, host ChatOpHost) {
	s := strings.TrimSpace(text)
	if !strings.HasPrefix(s, "/") {
		host.ShowError("not a command")
		return
	}
	rest := s[1:]
	name, args, _ := strings.Cut(rest, " ")
	name = strings.ToLower(name)
	for _, op := range chatOps {
		if op.Name == name {
			op.Exec(strings.TrimSpace(args), host)
			return
		}
	}
	host.ShowError("unknown command: /" + name)
}

// ── Individual ops ────────────────────────────────────────────────────────────

// giphyChatOp implements /giphy. Calls the broker-proxied GIPHY /translate
// endpoint with the user's phrase; on success posts the original-rendition
// URL to chat so every peer's client auto-embeds it via isGifURL detection.
func giphyChatOp(args string, host ChatOpHost) {
	if args == "" {
		host.ShowError("/giphy: missing search term (usage: /giphy <text>)")
		return
	}
	go func() {
		resp, err := connection.GiphyTranslate(apitypes.GiphyTranslateRequest{Phrase: args})
		if err != nil {
			host.ShowError("giphy: " + err.Error())
			return
		}
		url := resp.Data.Images.Original.URL
		if url == "" {
			// Fall back to a smaller rendition if the original didn't come back.
			url = resp.Data.Images.Downsized.URL
		}
		if url == "" {
			host.ShowError("giphy: no GIF returned for " + args)
			return
		}
		host.SendMessage(url)
	}()
}
