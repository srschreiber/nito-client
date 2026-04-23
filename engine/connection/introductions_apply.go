// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package connection

// applyIntroductions is the client-side verifier for a batch of signed
// introductions fetched from the broker. Every introduction is checked
// in isolation — signature must verify against the introducer's locally
// *verified* pin — before being merged into our trust state via
// keys.AddIntroduction. Introductions from unverified (or unknown)
// signers are silently dropped; they have no trust weight in our model.
//
// Returns the number of introductions that passed verification and were
// applied, for logging / observability.

import (
	apitypes "github.com/srschreiber/nito-client/shared/api_types"

	"github.com/srschreiber/nito-client/engine/keys"
	"github.com/srschreiber/nito-client/ui/clientlog"
)

// RefreshIntroductions pulls introductions for a room and merges any new
// ones into local trust state. Exposed for callers (like the headless
// bot) that need to poll for introductions published after they joined
// the room — desktop clients only need the room-join-time fetch that
// SetSessionRoom already performs, but a long-running bot misses every
// introduction its owner signs post-join without a periodic refresh.
//
// Returns the number of introductions applied (newly verified +
// merged). Errors from the broker propagate; apply-level failures are
// logged and counted out of the result.
func RefreshIntroductions(roomID string) (int, error) {
	intros, err := ListIntroductions(roomID)
	if err != nil {
		return 0, err
	}
	return applyIntroductions(intros), nil
}

func applyIntroductions(intros []apitypes.SignedIntroduction) int {
	applied := 0
	for _, intro := range intros {
		if intro.VerifiedByUsername == "" || intro.Username == "" || intro.PublicKey == "" {
			continue
		}
		// Skip introductions the broker returned that aren't from
		// someone we've mutual-verified. Trusting an unverified
		// introducer would collapse "introduced" back down to TOFU.
		rec, ok := keys.LoadPeerPublicKeyByDevice(intro.VerifiedByUsername, intro.VerifiedByDeviceID)
		if !ok || !rec.Verified {
			clientlog.Info("skipping introduction from unverified signer %s/%s", intro.VerifiedByUsername, intro.VerifiedByDeviceID)
			continue
		}
		if err := keys.VerifyIntroduction(&intro, rec.PublicKey); err != nil {
			clientlog.Warn("introduction signature invalid (target=%s, signer=%s): %v", intro.Username, intro.VerifiedByUsername, err)
			continue
		}
		if err := keys.AddIntroduction(intro.Username, intro.PublicKey, intro.VerifiedByUsername); err != nil {
			clientlog.Warn("add introduction for %s: %v", intro.Username, err)
			continue
		}
		applied++
	}
	return applied
}
