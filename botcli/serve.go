// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package botcli

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/srschreiber/nito-client/engine/connection"
	wstypes "github.com/srschreiber/nito-client/shared/websocket_types"
)

// rateWindow is how long a sender must wait between bot requests. 5s is the
// user-chosen setting in the plan; exposed here rather than inline so the
// limit is one-place-to-change.
const rateWindow = 5 * time.Second

// introRefreshInterval is how often the bot re-pulls signed introductions
// from the broker. SetSessionRoom only fetches them at join time, so without
// this tick a bot in a long-lived room never learns that its owner has
// verified (and introduced) new members — those members would be silently
// refused by senderAllowed. 5 minutes is a compromise between freshness
// and broker load for a single-bot installation.
const introRefreshInterval = 5 * time.Minute

// runServe is the terminal step: the bot stays in the room forever, watches
// incoming room messages, and responds to `!`-prefixed commands from
// owner-introduced senders. Returns only on ctx cancellation.
//
// Reconnect is handled in parallel (reconnectLoop); this loop just follows
// the RoomMessageChan — on WS close the channel close cascades and we
// re-fetch after the reconnect completes.
func runServe(ctx context.Context, state BotState) error {
	log.Printf("serve: ready — room=%s owner=%q", state.RoomID, state.OwnerUsername)

	go reconnectLoop(ctx, state)
	go introRefreshLoop(ctx, state.RoomID)

	limiter := NewLimiter(rateWindow)

	for ctx.Err() == nil {
		ch := connection.RoomMessageChan()
		if ch == nil {
			if err := sleepCtx(ctx, 500*time.Millisecond); err != nil {
				return nil
			}
			continue
		}
		// After a reconnect we may need to re-join the room. Do it lazily
		// on every outer iteration — SetSessionRoom is cheap if the key
		// is already cached, and it's the single source of truth for
		// "we're ready to receive".
		if connection.GetSessionRoomID() == nil {
			if err := connection.SetSessionRoom(state.RoomID); err != nil {
				log.Printf("serve: re-join room %s failed: %v", state.RoomID, err)
				if err := sleepCtx(ctx, 2*time.Second); err != nil {
					return nil
				}
				continue
			}
		}
		handleMessages(ctx, ch, state, limiter)
	}
	return nil
}

// handleMessages decrypts and dispatches until the channel closes
// (disconnect) or ctx is done.
func handleMessages(ctx context.Context, ch <-chan []byte, state BotState, limiter *Limiter) {
	for {
		select {
		case <-ctx.Done():
			return
		case data, ok := <-ch:
			if !ok {
				// WS disconnected. Outer loop reopens after reconnect.
				return
			}
			handleOne(data, state, limiter)
		}
	}
}

func handleOne(data []byte, state BotState, limiter *Limiter) {
	var payload wstypes.RoomMessagePayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return
	}
	// Ignore our own echoes — the broker echoes every room_message back
	// to the sender, and the bot's hello reply would otherwise re-enter
	// the dispatcher as if someone said `!hello`.
	if payload.FromUsername == state.Username {
		return
	}
	chain, err := connection.GetOrCreateRoomKeyChain()
	if err != nil {
		log.Printf("serve: get key chain: %v", err)
		return
	}
	ct, err := base64.StdEncoding.DecodeString(payload.EncryptedText)
	if err != nil {
		return
	}
	plaintext, err := chain.DecryptMessageWithRoomKey(ct, payload.FromUsername, &payload.SenderMessageCount)
	if err != nil {
		log.Printf("serve: decrypt from %q failed: %v", payload.FromUsername, err)
		return
	}
	text := strings.TrimSpace(string(plaintext))
	if !strings.HasPrefix(text, "!") {
		return // normal chat; not a bot request
	}

	allowed, why := senderAllowed(payload.FromUsername, state.OwnerUsername)
	if !allowed {
		log.Printf("serve: refused %q from %q (%s)", firstToken(text), payload.FromUsername, why)
		return
	}
	if !limiter.Allow(payload.FromUsername) {
		log.Printf("serve: rate-limited %q from %q", firstToken(text), payload.FromUsername)
		return
	}

	reply := dispatch(text, payload.FromUsername)
	if reply == "" {
		return
	}
	if err := sendRoomReply(reply); err != nil {
		log.Printf("serve: reply send failed: %v", err)
	}
}

// dispatch is the whole command table. Everything takes the raw text and
// returns the reply text (or empty to send nothing). Kept inline: one
// handler today, and the call-site indirection would outweigh any
// abstraction gain.
func dispatch(text, sender string) string {
	cmd := firstToken(text)
	switch cmd {
	case "!hello":
		return fmt.Sprintf("hello, @%s", sender)
	default:
		return "" // unknown commands are silently ignored — keeps room chatter clean
	}
}

// introRefreshLoop polls signed introductions on introRefreshInterval and
// merges newly applied ones into the local trust cache. On disconnect
// the refresh fails transiently and retries on the next tick — reconnect
// logic lives in reconnectLoop, this loop doesn't need to care.
//
// Runs until ctx is cancelled. One tick is skipped at startup because
// SetSessionRoom inside runInviteWait already pulled introductions once;
// the first refresh five minutes later is the first genuinely new data.
func introRefreshLoop(ctx context.Context, roomID string) {
	tick := time.NewTicker(introRefreshInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if !connection.IsConnected() {
				continue
			}
			applied, err := connection.RefreshIntroductions(roomID)
			if err != nil {
				log.Printf("introductions: refresh failed (%v); will retry in %s", err, introRefreshInterval)
				continue
			}
			if applied > 0 {
				log.Printf("introductions: applied %d new", applied)
			}
		}
	}
}

func firstToken(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, " \t\n"); i > 0 {
		return s[:i]
	}
	return s
}

// sendRoomReply encrypts + sends an outbound room message using the same
// ratchet + WS envelope the desktop client uses. Reimplemented here rather
// than imported from engine/commands to keep the bot binary free of the
// CGo audio stack that engine/commands pulls in via voice.go.
//
// Every outbound message must carry a unique Nonce + current Timestamp
// (SECURITY.md §"Wire-message authentication"). We mirror the engine's
// choice of `%d-nanoseconds` — same monotonicity guarantee as the desktop
// client, so the broker's replay-detection logic treats bot and human
// traffic identically.
func sendRoomReply(text string) error {
	chain, err := connection.GetOrCreateRoomKeyChain()
	if err != nil {
		return fmt.Errorf("get key chain: %w", err)
	}
	username := connection.GetSessionUsername()
	roomID := connection.GetSessionRoomID()
	if username == "" || roomID == nil {
		return fmt.Errorf("no active session/room")
	}
	info := connection.GetSessionRoomInfo()
	if info == nil {
		return fmt.Errorf("no room info")
	}
	count := info.SentMessageCount
	ct, err := chain.EncryptMessageWithRoomKey([]byte(text), username, &count)
	if err != nil {
		return fmt.Errorf("encrypt: %w", err)
	}
	version := 0
	if v := connection.GetSessionRoomKeyVersion(); v != nil {
		version = *v
	}
	payloadBytes, err := json.Marshal(wstypes.RoomMessagePayload{
		RoomID:             *roomID,
		RoomKeyVersion:     version,
		SenderMessageCount: count,
		FromUsername:       username,
		EncryptedText:      base64.StdEncoding.EncodeToString(ct),
		MessageType:        wstypes.MessageTypeText,
	})
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	now := time.Now().UnixNano()
	raw, err := json.Marshal(wstypes.ToBrokerWsMessage{
		RPCName:   wstypes.RPCRoomMessage,
		RequestID: fmt.Sprintf("%d", now),
		UserID:    username,
		Nonce:     fmt.Sprintf("%d", now),
		Timestamp: time.Now().Unix(),
		Payload:   payloadBytes,
	})
	if err != nil {
		return fmt.Errorf("marshal envelope: %w", err)
	}
	if err := connection.Send(raw); err != nil {
		return fmt.Errorf("ws send: %w", err)
	}
	connection.IncrementSessionSentMessageCount()
	return nil
}
