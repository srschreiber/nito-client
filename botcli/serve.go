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

// introRefreshInterval is how often the bot re-pulls signed introductions
// from the broker. SetSessionRoom only fetches them at join time, so without
// this tick a bot in a long-lived room never learns that its owner has
// verified (and introduced) new members — those members would be silently
// refused by senderAllowed. 5 minutes is a compromise between freshness
// and broker load.
const introRefreshInterval = 5 * time.Minute

// runServe is the terminal step: the bot stays online forever, watches
// inbound room messages across every joined room, and routes !-prefixed
// commands through the YAML-defined dispatcher. Returns only on ctx cancel.
//
// Reconnect, intro refresh, and background invite acceptance run in
// parallel goroutines; this loop just follows RoomMessageChan.
func runServe(ctx context.Context, rt *BotRuntime, dispatcher *Dispatcher) error {
	state := rt.Snapshot()
	log.Printf("serve: ready — rooms=%v owner=%q commands=%d", state.RoomIDs, state.OwnerUsername, len(dispatcher.cfg.Commands))

	go reconnectLoop(ctx, state)
	go inviteAcceptLoop(ctx, rt)
	go introRefreshLoop(ctx, rt)

	for ctx.Err() == nil {
		ch := connection.RoomMessageChan()
		if ch == nil {
			if err := sleepCtx(ctx, 500*time.Millisecond); err != nil {
				return nil
			}
			continue
		}
		// After a (re)connect, re-join every room so the broker hands
		// us its room key and we land on the fan-out gate. Cheap if
		// the keys are still cached; idempotent on repeat.
		for _, rid := range rt.Snapshot().RoomIDs {
			if err := connection.SetSessionRoom(rid); err != nil {
				log.Printf("serve: re-join room %s failed: %v", rid, err)
				continue
			}
			sendRoomEnter(rid)
		}
		handleMessages(ctx, ch, rt, dispatcher)
	}
	return nil
}

// handleMessages decrypts and dispatches until the channel closes (WS
// disconnect) or ctx is done. Caller re-enters runServe's outer loop on
// channel close to pick up the new channel after reconnect.
func handleMessages(ctx context.Context, ch <-chan []byte, rt *BotRuntime, dispatcher *Dispatcher) {
	for {
		select {
		case <-ctx.Done():
			return
		case data, ok := <-ch:
			if !ok {
				return
			}
			handleOne(ctx, data, rt, dispatcher)
		}
	}
}

func handleOne(ctx context.Context, data []byte, rt *BotRuntime, dispatcher *Dispatcher) {
	var payload wstypes.RoomMessagePayload
	if err := json.Unmarshal(data, &payload); err != nil {
		log.Printf("serve: unmarshal room message: %v", err)
		return
	}
	state := rt.Snapshot()
	if !roomKnown(state.RoomIDs, payload.RoomID) {
		// Stale frame for a room we've left, or one we haven't joined yet.
		return
	}
	// Drop our own echoes — the broker echoes every room_message back
	// to the sender.
	if payload.FromUsername == state.Username {
		return
	}
	chain, err := connection.GetOrCreateRoomKeyChain(payload.RoomID)
	if err != nil {
		log.Printf("serve: get key chain for %s: %v", payload.RoomID, err)
		return
	}
	ct, err := base64.StdEncoding.DecodeString(payload.EncryptedText)
	if err != nil {
		log.Printf("serve: base64 decode from %q: %v", payload.FromUsername, err)
		return
	}
	plaintext, err := chain.DecryptMessageWithRoomKey(ct, payload.FromUsername, &payload.SenderMessageCount)
	if err != nil {
		log.Printf("serve: decrypt from %q failed: %v", payload.FromUsername, err)
		return
	}
	text := strings.TrimSpace(string(plaintext))
	if !strings.HasPrefix(text, "!") {
		return
	}

	allowed, why := senderAllowed(payload.FromUsername, state.OwnerUsername)
	if !allowed {
		log.Printf("serve: refused %q from %q (%s)", firstToken(text), payload.FromUsername, why)
		return
	}

	reply, rateLimited := dispatcher.Dispatch(ctx, text, payload.FromUsername)
	if rateLimited {
		log.Printf("serve: rate-limited %q from %q", firstToken(text), payload.FromUsername)
		if err := sendRoomReply("hold your horses", payload.RoomID); err != nil {
			log.Printf("serve: rate-limit reply failed: %v", err)
		}
		return
	}
	if reply == "" {
		return
	}
	if err := sendRoomReply(reply, payload.RoomID); err != nil {
		log.Printf("serve: reply send failed: %v", err)
	}
}

func roomKnown(rooms []string, rid string) bool {
	for _, r := range rooms {
		if r == rid {
			return true
		}
	}
	return false
}

// introRefreshLoop polls signed introductions across every joined room
// on introRefreshInterval and merges newly-applied ones into the local
// trust cache. On disconnect the refresh fails transiently and retries
// on the next tick — reconnect logic lives in reconnectLoop, this loop
// doesn't need to care.
func introRefreshLoop(ctx context.Context, rt *BotRuntime) {
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
			for _, rid := range rt.Snapshot().RoomIDs {
				applied, err := connection.RefreshIntroductions(rid)
				if err != nil {
					log.Printf("introductions: refresh %s failed (%v); will retry in %s", rid, err, introRefreshInterval)
					continue
				}
				if applied > 0 {
					log.Printf("introductions: room %s applied %d new", rid, applied)
				}
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
func sendRoomReply(text, roomID string) error {
	chain, err := connection.GetOrCreateRoomKeyChain(roomID)
	if err != nil {
		return fmt.Errorf("get key chain: %w", err)
	}
	username := connection.GetSessionUsername()
	if username == "" || roomID == "" {
		return fmt.Errorf("no active session/room")
	}
	info := connection.GetRoomInfo(roomID)
	if info == nil {
		return fmt.Errorf("no room info for %s", roomID)
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
		RoomID:             roomID,
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
	connection.IncrementSentMessageCount(roomID)
	return nil
}

// sendRoomEnter tells the broker the bot is actively viewing the given room,
// which opens the broker's fan-out gate for room_message delivery. Called
// after each (re)connect for every joined room. Best-effort: a failed send
// is logged but not fatal.
func sendRoomEnter(roomID string) {
	username := connection.GetSessionUsername()
	if username == "" {
		return
	}
	payloadBytes, err := json.Marshal(wstypes.RoomEnterPayload{RoomID: roomID})
	if err != nil {
		log.Printf("serve: marshal room_enter: %v", err)
		return
	}
	now := time.Now().UnixNano()
	raw, err := json.Marshal(wstypes.ToBrokerWsMessage{
		RPCName:   wstypes.RPCRoomEnter,
		RequestID: fmt.Sprintf("%d", now),
		UserID:    username,
		Nonce:     fmt.Sprintf("%d", now),
		Timestamp: time.Now().Unix(),
		Payload:   payloadBytes,
	})
	if err != nil {
		log.Printf("serve: marshal room_enter envelope: %v", err)
		return
	}
	if err := connection.Send(raw); err != nil {
		log.Printf("serve: room_enter send: %v", err)
	}
}
