// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package keys

// ResolvePeerPublicKey implements the web-of-trust resolution described in
// SECURITY.md. It consults every TrustedKey record we have for a user and
// returns a single ResolvedPeer capturing both the selected pubkey and the
// confidence we have in that selection.
//
// Precedence (highest-trust first):
//
//  1. Any record with Verified == true (mutual-verify is ground truth).
//     Introductions disagreeing with a verified pin are flagged in the
//     result but never override the verified key.
//
//  2. Introduction consensus: among records whose Introducers is
//     non-empty, group by pubkey. If a strict plurality exists
//     (one pubkey has strictly more introducers than any other),
//     return that pubkey with method=Introduced. If multiple pubkeys
//     tie at the top, the identity is contested — return Contested
//     with no selected pubkey so callers can fall back / warn the user.
//
//  3. Broker-TOFU fallback: a single unvouched pin (no introducers,
//     not verified). Method = TOFU.
//
//  4. Nothing on record → Found == false.

// ResolvedPeer is the output of ResolvePeerPublicKey. PublicKey is the
// winning pubkey PEM; Method describes confidence; Contested and
// DisagreementDetails surface edge cases the UI must warn about even
// when a pubkey was successfully selected.
type ResolvedPeer struct {
	Found       bool
	PublicKey   string
	DeviceID    string
	Method      TrustMethod
	Introducers []string // usernames that introduced the selected pubkey

	// Contested is true when multiple introduced pubkeys tied at the top,
	// OR when an introduction disagrees with a verified pin. The UI must
	// surface this loudly — the pubkey we return in contested cases is
	// the safest fallback, not a consensus.
	Contested bool
	// DisagreementDetails contains a human-readable summary of what
	// conflicting evidence exists (e.g. "2 introducers each for 2
	// different pubkeys"). Safe to show to users.
	DisagreementDetails string
}

// ResolvePeerPublicKey returns the resolved trust state for username.
func ResolvePeerPublicKey(username string) ResolvedPeer {
	recs, _ := loadPeerPublicKeys(username)
	if len(recs) == 0 {
		return ResolvedPeer{}
	}

	// 1. Verified wins outright.
	var verified *TrustedKey
	for i := range recs {
		if recs[i].Verified {
			verified = &recs[i]
			break
		}
	}
	if verified != nil {
		contested, detail := detectConflictAgainstVerified(*verified, recs)
		return ResolvedPeer{
			Found:               true,
			PublicKey:           verified.PublicKey,
			DeviceID:            verified.DeviceID,
			Method:              TrustMethodVerified,
			Introducers:         verified.Introducers,
			Contested:           contested,
			DisagreementDetails: detail,
		}
	}

	// 2. Introduction consensus.
	introduced := filterIntroduced(recs)
	if len(introduced) > 0 {
		winner, tied := plurality(introduced)
		if tied {
			return ResolvedPeer{
				Found:               false,
				Contested:           true,
				DisagreementDetails: "introduced pubkeys tied; no consensus",
			}
		}
		return ResolvedPeer{
			Found:       true,
			PublicKey:   winner.PublicKey,
			DeviceID:    winner.DeviceID,
			Method:      TrustMethodIntroduced,
			Introducers: winner.Introducers,
		}
	}

	// 3. Broker-TOFU: single record, no corroboration.
	r := recs[0]
	return ResolvedPeer{
		Found:     true,
		PublicKey: r.PublicKey,
		DeviceID:  r.DeviceID,
		Method:    TrustMethodTOFU,
	}
}

// detectConflictAgainstVerified reports whether any *introduction* exists
// for a pubkey other than the one we mutual-verified. That's a signal
// someone in our web-of-trust disagrees with us — not automatically an
// attack (they may have verified a newer rotation we haven't picked up),
// but worth surfacing in the UI.
func detectConflictAgainstVerified(verified TrustedKey, all []TrustedKey) (bool, string) {
	var dissenters []string
	for _, r := range all {
		if r.DeviceID == verified.DeviceID {
			continue
		}
		for _, u := range r.Introducers {
			dissenters = append(dissenters, u)
		}
	}
	if len(dissenters) == 0 {
		return false, ""
	}
	return true, "introduction(s) disagree with your verified pin (verified wins)"
}

// filterIntroduced returns only records with at least one introducer.
func filterIntroduced(recs []TrustedKey) []TrustedKey {
	out := make([]TrustedKey, 0, len(recs))
	for _, r := range recs {
		if len(r.Introducers) > 0 {
			out = append(out, r)
		}
	}
	return out
}

// plurality picks the introduced record with the strictly highest number
// of introducers. Returns tied=true if two or more records tie at the top,
// in which case the caller should treat the identity as contested.
func plurality(introduced []TrustedKey) (winner TrustedKey, tied bool) {
	best := -1
	var bestIdx int
	secondBest := -1
	for i, r := range introduced {
		n := len(r.Introducers)
		if n > best {
			secondBest = best
			best = n
			bestIdx = i
		} else if n > secondBest {
			secondBest = n
		}
	}
	if best == secondBest {
		return TrustedKey{}, true
	}
	return introduced[bestIdx], false
}
