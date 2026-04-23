// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package botcli

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/srschreiber/nito-client/engine/connection"
	apitypes "github.com/srschreiber/nito-client/shared/api_types"
	wstypes "github.com/srschreiber/nito-client/shared/websocket_types"
)

// runInviteWait blocks until the bot accepts its first invite from the
// verified owner. It:
//
//  1. Polls ListPendingInvites once on entry (cold start may have missed
//     the original user_added_to_room notification).
//  2. Listens on NotifChan() for subsequent UserAddedToRoom events and
//     re-polls on each.
//
// For each pending invite it checks InviterUsername == owner; anything else
// is logged and ignored. The first match is accepted + persisted.
//
// The reconnect loop runs alongside this; if the WS drops, our NotifChan
// returns a new channel after login re-establishes. The caller's ctx tying
// us to SIGTERM means a stop-the-bot during this phase exits promptly.
func runInviteWait(ctx context.Context, state BotState) (BotState, error) {
	log.Printf("invite: waiting for invite from owner %q", state.OwnerUsername)

	// Start reconnect watcher in background so WS drops during the wait
	// don't leave us dangling with a closed NotifChan forever.
	go reconnectLoop(ctx, state)

	// Initial sweep: handles the case where the owner invited us while the
	// bot was offline, so the push notification was never delivered.
	if st, ok := tryAcceptInvites(ctx, state); ok {
		return st, nil
	}

	for ctx.Err() == nil {
		ch := connection.NotifChan()
		if ch == nil {
			if err := sleepCtx(ctx, 500*time.Millisecond); err != nil {
				return state, err
			}
			continue
		}
		select {
		case <-ctx.Done():
			return state, ctx.Err()
		case data, ok := <-ch:
			if !ok {
				// Reconnecting — wait briefly and re-fetch channel.
				if err := sleepCtx(ctx, 500*time.Millisecond); err != nil {
					return state, err
				}
				continue
			}
			var notif wstypes.NotificationPayload
			if err := json.Unmarshal(data, &notif); err != nil {
				continue
			}
			if notif.Type != wstypes.NotificationTypeUserAddedToRoom {
				continue
			}
			if st, ok := tryAcceptInvites(ctx, state); ok {
				return st, nil
			}
		}
	}
	return state, ctx.Err()
}

// tryAcceptInvites pulls pending invites and accepts the first one from the
// verified owner. Returns (newState, true) on a successful accept, or
// (state, false) otherwise. Errors are logged and do not propagate — a
// transient broker issue shouldn't kill the bot.
func tryAcceptInvites(ctx context.Context, state BotState) (BotState, bool) {
	invites, err := connection.ListPendingInvites()
	if err != nil {
		log.Printf("invite: list failed (will retry on next notification): %v", err)
		return state, false
	}
	for _, inv := range invites {
		if inv.InviterUsername == "" {
			// Older broker doesn't populate the field. We can't enforce
			// the owner-only rule safely without it — refuse rather
			// than blindly accept.
			log.Printf("invite: ignoring invite to %q — broker did not supply inviter", inv.RoomID)
			continue
		}
		if inv.InviterUsername != state.OwnerUsername {
			log.Printf("invite: ignoring invite to %q from non-owner %q", inv.RoomID, inv.InviterUsername)
			continue
		}
		if err := acceptAndJoin(inv, &state); err != nil {
			log.Printf("invite: accept %q failed: %v", inv.RoomID, err)
			continue
		}
		return state, true
	}
	return state, false
}

// acceptAndJoin accepts the broker-side invite, then joins the room in the
// local session so the room key is decrypted and cached. Joining early lets
// the ready loop start receiving messages without a separate "select room"
// step.
//
// Owner's public key was pinned as Verified during the verify step, so the
// engine's SetSessionRoom can resolve the room creator's attestation without
// any extra work here — the standard trust path applies.
func acceptAndJoin(inv apitypes.PendingInvite, state *BotState) error {
	if err := connection.AcceptInvite(inv.RoomID); err != nil {
		return err
	}
	if err := connection.SetSessionRoom(inv.RoomID); err != nil {
		// If the room's rotator isn't verified/introduced, the bot
		// refuses to join — same policy as the desktop client. This is
		// the correct behaviour: a bot that trusts broker-asserted
		// identities bypasses the whole web-of-trust story. Log + bail.
		return err
	}
	state.RoomID = inv.RoomID
	state.Step = StepReady
	if err := SaveState(*state); err != nil {
		return err
	}
	log.Printf("invite: joined room %q (%s) — bootstrap complete", inv.RoomName, inv.RoomID)
	return nil
}
