// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package botcli

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/srschreiber/nito-client/engine/connection"
	"github.com/srschreiber/nito-client/engine/keys"
	apitypes "github.com/srschreiber/nito-client/shared/api_types"
	wstypes "github.com/srschreiber/nito-client/shared/websocket_types"
)

// runFirstInviteWait blocks until the bot accepts its first invite from the
// verified owner, then transitions to StepReady. Subsequent invites are
// accepted by inviteAcceptLoop running alongside the serve loop.
//
// Cold-start sweep first (the owner may have invited us while offline), then
// listen on NotifChan for further UserAddedToRoom events and re-poll on each.
func runFirstInviteWait(ctx context.Context, rt *BotRuntime) error {
	owner := rt.Snapshot().OwnerUsername
	log.Printf("invite: waiting for first invite from owner %q", owner)

	// Reconnect watcher: WS drops during the wait shouldn't leave us
	// dangling on a closed NotifChan forever.
	go reconnectLoop(ctx, rt.Snapshot())

	if accepted := tryAcceptInvites(ctx, rt); accepted > 0 {
		return rt.SetStep(StepReady)
	}

	for ctx.Err() == nil {
		ch := connection.NotifChan()
		if ch == nil {
			if err := sleepCtx(ctx, 500*time.Millisecond); err != nil {
				return err
			}
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case data, ok := <-ch:
			if !ok {
				if err := sleepCtx(ctx, 500*time.Millisecond); err != nil {
					return err
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
			if accepted := tryAcceptInvites(ctx, rt); accepted > 0 {
				return rt.SetStep(StepReady)
			}
		}
	}
	return ctx.Err()
}

// inviteAcceptLoop runs in the background once the bot is in StepReady. It
// polls + listens for new owner invites and joins each accepted room into
// the runtime's RoomIDs. The serve loop discovers the new room on its next
// re-join sweep (after the next reconnect, or via the explicit join we do
// here).
func inviteAcceptLoop(ctx context.Context, rt *BotRuntime) {
	// Pull anything that landed while we were offline before listening for
	// pushes — otherwise a notification we missed would never get a retry.
	tryAcceptInvites(ctx, rt)

	for ctx.Err() == nil {
		ch := connection.NotifChan()
		if ch == nil {
			if err := sleepCtx(ctx, 500*time.Millisecond); err != nil {
				return
			}
			continue
		}
		select {
		case <-ctx.Done():
			return
		case data, ok := <-ch:
			if !ok {
				if err := sleepCtx(ctx, 500*time.Millisecond); err != nil {
					return
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
			tryAcceptInvites(ctx, rt)
		}
	}
}

// tryAcceptInvites pulls pending invites and accepts every one that passes
// the owner-only policy. Returns the count of newly-joined rooms.
func tryAcceptInvites(ctx context.Context, rt *BotRuntime) int {
	state := rt.Snapshot()
	invites, err := connection.ListPendingInvites()
	if err != nil {
		log.Printf("invite: list failed (will retry on next notification): %v", err)
		return 0
	}
	accepted := 0
	for _, inv := range invites {
		if err := checkInviteAuth(inv, state); err != nil {
			log.Printf("invite: refusing %q: %v", inv.RoomID, err)
			continue
		}
		if err := acceptAndJoin(inv, rt); err != nil {
			log.Printf("invite: accept %q failed: %v", inv.RoomID, err)
			continue
		}
		accepted++
	}
	return accepted
}

// checkInviteAuth layers the bot's owner-only policy on top of the
// shared signature verification in connection.VerifyInvite. The engine
// helper handles required-fields / device-id / signature checks; here
// we add the two bot-specific rules:
//
//  1. InviterUsername must equal the bot's pinned owner (the sole user
//     we mutual-verified at bootstrap). Anyone else's valid invite is
//     still refused.
//  2. The resolved trust method must be Verified. Introduced / TOFU
//     are fine for human users but the bot has no UI to surface an
//     "accept anyway?" prompt, and accepting on a weaker pin would
//     undermine the single-owner trust root.
//
// Also cross-checks the stored owner_device_id (captured at verify time)
// against the invite's claimed device id — catches the case where an
// attacker convinced the owner to run a second verify against a forged
// key, or where the bot's state file was tampered with.
func checkInviteAuth(inv apitypes.PendingInvite, state BotState) error {
	if inv.InviterUsername != state.OwnerUsername {
		return fmt.Errorf("inviter %q is not owner %q", inv.InviterUsername, state.OwnerUsername)
	}
	if state.OwnerDeviceID != "" && inv.InviterDeviceID != "" && state.OwnerDeviceID != inv.InviterDeviceID {
		return fmt.Errorf("inviter device %s does not match owner device pinned at verify time %s",
			inv.InviterDeviceID, state.OwnerDeviceID)
	}
	res, err := connection.VerifyInvite(inv, state.Username)
	if err != nil {
		return err
	}
	if res.Method != keys.TrustMethodVerified {
		return fmt.Errorf("owner pin method=%s; bot requires verified — re-run verify step", res.Method)
	}
	if res.Contested {
		return fmt.Errorf("owner pin is contested (%s); refusing", res.DisagreementDetails)
	}
	return nil
}

// acceptAndJoin accepts the broker-side invite, joins the room locally so
// the room key is decrypted and cached, sends room_enter so the broker
// fans out subsequent messages, and persists the new RoomID to disk.
func acceptAndJoin(inv apitypes.PendingInvite, rt *BotRuntime) error {
	if err := connection.AcceptInvite(inv.RoomID); err != nil {
		return err
	}
	if err := connection.SetSessionRoom(inv.RoomID); err != nil {
		// If the rotator isn't verified/introduced, the bot refuses the
		// join — same policy as the desktop client. Bail rather than
		// trust a broker-asserted identity for messaging.
		return err
	}
	added, err := rt.AddRoom(inv.RoomID)
	if err != nil {
		return fmt.Errorf("persist room id: %w", err)
	}
	if added {
		sendRoomEnter(inv.RoomID)
		log.Printf("invite: joined room %q (%s)", inv.RoomName, inv.RoomID)
	}
	return nil
}
