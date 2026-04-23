// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package botcli

import (
	"github.com/srschreiber/nito-client/engine/keys"
)

// senderAllowed reports whether the bot should serve a request from `sender`
// given `owner` as the verified operator. Rules (matching BOTS.md):
//
//  1. The owner themselves is always allowed.
//  2. Anyone the owner has introduced (their verified signature on a
//     SignedIntroduction ties sender's pubkey to sender's username) is allowed.
//     Introductions from other parties don't count — the bot's trust root is
//     its single owner.
//  3. Contested pins (different verifiers disagree about sender's pubkey) are
//     refused. The web-of-trust resolver flags these and we treat them as
//     "don't serve" rather than pick a winner the resolver refused to pick.
//  4. TOFU-only pins are refused — the bot trusts the owner, not the broker.
//
// Returns an explanation string alongside the bool so the caller can log the
// decision without re-deriving it.
func senderAllowed(sender, owner string) (bool, string) {
	if sender == "" || owner == "" {
		return false, "empty sender or owner"
	}
	if sender == owner {
		return true, "sender is owner"
	}
	resolved := keys.ResolvePeerPublicKey(sender)
	if !resolved.Found {
		return false, "no pin for sender"
	}
	if resolved.Contested {
		return false, "contested pin: " + resolved.DisagreementDetails
	}
	if resolved.Method != keys.TrustMethodIntroduced && resolved.Method != keys.TrustMethodVerified {
		return false, "sender pin is " + string(resolved.Method) + "; need owner introduction"
	}
	for _, u := range resolved.Introducers {
		if u == owner {
			return true, "owner introduced sender"
		}
	}
	// Method==Verified without owner in introducers means *we* verified
	// this sender ourselves — the bot only verifies the owner, so this
	// shouldn't happen in normal operation. Refuse rather than guess.
	return false, "sender pin not introduced by owner"
}
