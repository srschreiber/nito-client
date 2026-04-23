// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package botcli

import (
	"testing"

	"github.com/srschreiber/nito-client/engine/keys"
	apitypes "github.com/srschreiber/nito-client/shared/api_types"
)

// signMembershipAs signs the canonical invite attestation bytes as
// username. Delegates to keys.SignAttestation so if the engine's
// canonical form ever shifts, the test will drift with it — not against
// it, which is the pathological case.
func signMembershipAs(t *testing.T, username, invitedUsername, roomID, deviceID string) string {
	t.Helper()
	sig, err := keys.SignAttestation(map[string]string{
		"device_id":        deviceID,
		"invited_username": invitedUsername,
		"room_id":          roomID,
	}, username)
	if err != nil {
		t.Fatalf("sign membership: %v", err)
	}
	return sig
}

// TestCheckInviteAuth walks every rejection branch and a happy path. The
// "broker compromise" scenarios are the load-bearing ones: a malicious
// broker substituting inviter, pubkey, or signature must not cause an
// accepted invite for the bot.
func TestCheckInviteAuth(t *testing.T) {
	keys.SetActiveBroker(t.TempDir())
	t.Cleanup(func() { keys.SetActiveBroker("") })

	// Alice (owner) and Mallory (attacker) each have a real on-disk
	// keypair so keys.SignAttestation can sign with them.
	alicePub, err := keys.LoadOrGenerate("alice")
	if err != nil {
		t.Fatalf("gen alice: %v", err)
	}
	aliceDevice, err := keys.DeviceIDFromPublicKeyPEM(alicePub)
	if err != nil {
		t.Fatalf("derive alice device: %v", err)
	}
	if err := keys.SavePeerPublicKey("alice", keys.TrustedKey{
		PublicKey: alicePub, Verified: true, Method: keys.TrustMethodVerified,
	}); err != nil {
		t.Fatalf("pin alice: %v", err)
	}
	malPub, err := keys.LoadOrGenerate("mallory")
	if err != nil {
		t.Fatalf("gen mallory: %v", err)
	}
	mallDevice, err := keys.DeviceIDFromPublicKeyPEM(malPub)
	if err != nil {
		t.Fatalf("derive mallory device: %v", err)
	}

	state := BotState{
		Username:      "bot",
		OwnerUsername: "alice",
		OwnerDeviceID: aliceDevice,
	}
	const roomID = "room-xyz"
	goodSig := signMembershipAs(t, "alice", "bot", roomID, aliceDevice)

	// Happy path: alice invites bot with a correct signature.
	if err := checkInviteAuth(apitypes.PendingInvite{
		RoomID:              roomID,
		InviterUsername:     "alice",
		InviterDeviceID:     aliceDevice,
		MembershipSignature: goodSig,
	}, state); err != nil {
		t.Fatalf("happy path rejected: %v", err)
	}

	// Missing required fields → refused (broker did not upgrade).
	if err := checkInviteAuth(apitypes.PendingInvite{
		RoomID: roomID, InviterUsername: "alice", InviterDeviceID: aliceDevice,
		// MembershipSignature empty
	}, state); err == nil {
		t.Fatal("missing signature must be refused")
	}

	// Non-owner inviter (broker honest but some random user invited us).
	if err := checkInviteAuth(apitypes.PendingInvite{
		RoomID:              roomID,
		InviterUsername:     "mallory",
		InviterDeviceID:     mallDevice,
		MembershipSignature: signMembershipAs(t, "mallory", "bot", roomID, mallDevice),
	}, state); err == nil {
		t.Fatal("non-owner inviter must be refused")
	}

	// Broker forgery: claims inviter=alice but signature is under
	// Mallory's key. Device id mismatch catches it even before the sig
	// check runs.
	if err := checkInviteAuth(apitypes.PendingInvite{
		RoomID:              roomID,
		InviterUsername:     "alice",
		InviterDeviceID:     mallDevice,
		MembershipSignature: signMembershipAs(t, "mallory", "bot", roomID, mallDevice),
	}, state); err == nil {
		t.Fatal("device id mismatch must be refused")
	}

	// Tampered signature (claims alice + alice's device id, but sig
	// bytes are corrupted).
	tampered := goodSig[:len(goodSig)-4] + "AAAA"
	if err := checkInviteAuth(apitypes.PendingInvite{
		RoomID:              roomID,
		InviterUsername:     "alice",
		InviterDeviceID:     aliceDevice,
		MembershipSignature: tampered,
	}, state); err == nil {
		t.Fatal("tampered signature must be refused")
	}

	// Broker swaps room_id on an otherwise-valid signature — canonical
	// bytes differ, verify fails.
	if err := checkInviteAuth(apitypes.PendingInvite{
		RoomID:              "different-room",
		InviterUsername:     "alice",
		InviterDeviceID:     aliceDevice,
		MembershipSignature: goodSig,
	}, state); err == nil {
		t.Fatal("invite retargeted to different room must be refused")
	}

	// Signature for a different invitee (attacker steals a valid invite
	// addressed to someone else, re-targets it to the bot).
	wrongInvitee := signMembershipAs(t, "alice", "someone-else", roomID, aliceDevice)
	if err := checkInviteAuth(apitypes.PendingInvite{
		RoomID:              roomID,
		InviterUsername:     "alice",
		InviterDeviceID:     aliceDevice,
		MembershipSignature: wrongInvitee,
	}, state); err == nil {
		t.Fatal("invite signed for a different invitee must be refused")
	}
}
