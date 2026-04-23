// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package connection

import (
	"errors"
	"fmt"

	"github.com/srschreiber/nito-client/engine/keys"
	apitypes "github.com/srschreiber/nito-client/shared/api_types"
)

// InviteVerifyResult describes what VerifyInvite learned about an invite
// after the signature check passed. Callers use the Method field to
// decide whether to accept silently (Verified / Introduced) or show the
// user a warning and require explicit consent (TOFU).
type InviteVerifyResult struct {
	// InviterPubPEM is the pubkey the signature was verified with —
	// derived from local trust state or, on first contact, a broker
	// TOFU fetch. Callers that want to display "invited by alice
	// (device=abcd, verified)" already have the pieces here.
	InviterPubPEM string
	// Method is the effective trust level of the inviter's pin at the
	// moment this check ran. TOFU means the broker is the only source;
	// bots must refuse TOFU but human users may accept with a warning.
	Method keys.TrustMethod
	// Contested is true when ResolvePeerPublicKey surfaced conflicting
	// evidence (e.g. two verified peers introduced different pubkeys).
	// Callers must treat this as an amber flag even when Method is set
	// — the resolver picked the safest option, not a consensus.
	Contested bool
	// DisagreementDetails mirrors ResolvedPeer.DisagreementDetails for
	// UI display when Contested is true.
	DisagreementDetails string
}

// VerifyInvite cryptographically verifies that a PendingInvite was really
// issued by the username the broker claims. Shared entry point for
// everyone who needs to act on an invite — the headless bot and the
// desktop UI both route through here to avoid two divergent
// implementations of the same invariant.
//
// Layered checks (each must pass):
//
//  1. Required fields present — an older broker that omits any of them
//     cannot support verified invites. Callers should treat this as a
//     hard failure and tell the user to upgrade the broker.
//  2. Inviter's pubkey resolves via the web-of-trust hierarchy
//     (Verified > Introduced > TOFU). On first contact — no prior pin —
//     we fall through to GetOrStoreUserPublicKey, which pins as TOFU
//     and gives the caller something to verify against. A broker that
//     lies here can only lie consistently about a user the client has
//     *never* seen, which is the standard TOFU caveat.
//  3. DeviceID derived from the resolved pubkey matches
//     inv.InviterDeviceID — closes the broker-swap vector that lets a
//     compromised broker pair a sig with an unrelated key.
//  4. RSA-PSS signature over the canonical
//     "device_id;invited_username;room_id" bytes verifies. Matches the
//     inviter-side construction in engine/commands/room.go.
//
// selfUsername is the authenticated caller's username — used to
// reconstruct the canonical bytes (the invitee slot). Pass
// connection.GetSessionUsername() in practice.
func VerifyInvite(inv apitypes.PendingInvite, selfUsername string) (InviteVerifyResult, error) {
	if selfUsername == "" {
		return InviteVerifyResult{}, errors.New("verify invite: no active session username")
	}
	if inv.InviterUsername == "" || inv.InviterDeviceID == "" || inv.MembershipSignature == "" {
		return InviteVerifyResult{}, errors.New("verify invite: broker did not supply inviterUsername / inviterDeviceId / membershipSignature (upgrade broker)")
	}

	// Resolve inviter's pubkey, preferring Verified > Introduced > TOFU.
	// If we've never seen this user, fall through to a broker TOFU fetch
	// which also pins for future calls.
	resolved := keys.ResolvePeerPublicKey(inv.InviterUsername)
	pubPEM := ""
	switch {
	case resolved.Found:
		pubPEM = resolved.PublicKey
	default:
		fetched, err := GetOrStoreUserPublicKey(inv.InviterUsername)
		if err != nil {
			return InviteVerifyResult{}, fmt.Errorf("verify invite: fetch inviter pubkey: %w", err)
		}
		pubPEM = fetched
		// Refresh the resolved view so Method reflects the new TOFU pin.
		resolved = keys.ResolvePeerPublicKey(inv.InviterUsername)
	}

	derivedDevice, err := keys.DeviceIDFromPublicKeyPEM(pubPEM)
	if err != nil {
		return InviteVerifyResult{}, fmt.Errorf("verify invite: derive inviter device id: %w", err)
	}
	if derivedDevice != inv.InviterDeviceID {
		return InviteVerifyResult{}, fmt.Errorf("verify invite: inviter device id mismatch (claimed=%s derived=%s) — possible broker substitution",
			inv.InviterDeviceID, derivedDevice)
	}

	if err := keys.VerifyAttestation(map[string]string{
		"device_id":        inv.InviterDeviceID,
		"invited_username": selfUsername,
		"room_id":          inv.RoomID,
	}, inv.MembershipSignature, pubPEM); err != nil {
		return InviteVerifyResult{}, fmt.Errorf("verify invite: membership signature invalid: %w", err)
	}

	return InviteVerifyResult{
		InviterPubPEM:       pubPEM,
		Method:              resolved.Method,
		Contested:           resolved.Contested,
		DisagreementDetails: resolved.DisagreementDetails,
	}, nil
}
