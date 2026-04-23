// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package keys

// On-disk peer public-key store. Each username's file holds an array of
// TrustedKey records so we can pin one key per device. The root device is the
// entry with empty DeviceID.
//
// Format evolution:
//   v1 (legacy): a single JSON object — one key, no multi-device awareness.
//   v2 (current): a JSON array of key records. LoadPeerPublicKeys transparently
//                 unwraps legacy files and re-writes them as arrays on the
//                 next SavePeerPublicKey call.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// TrustMethod describes the effective trust level on a peer's public key,
// computed from the provenance signals recorded on TrustedKey. The ordering
// matters at resolution time:
//
//	tofu < introduced < verified
//
// Verified is ground truth (set by the mutual-handshake flow) and is never
// overridden by later introductions — an introduction disagreeing with a
// verified pin surfaces as evidence of an attack, not as a reason to switch.
type TrustMethod string

const (
	TrustMethodTOFU       TrustMethod = "tofu"
	TrustMethodIntroduced TrustMethod = "introduced"
	TrustMethodVerified   TrustMethod = "verified"
)

// TrustedKey is the on-disk record for a peer's trusted public key. DeviceID
// is a function of PublicKey — SavePeerPublicKey derives it on write, callers
// must not set it manually. Under the web-of-trust model a single user may
// have multiple TrustedKey rows (one per distinct pubkey) when different
// verifiers introduce different keys — the resolver flags that as contested.
type TrustedKey struct {
	DeviceID  string      `json:"device_id"`  // sha256(DER(PublicKey)), hex
	PublicKey string      `json:"public_key"` // PEM-encoded RSA public key
	Verified  bool        `json:"verified"`   // true iff we mutual-handshake-verified this pubkey ourselves
	AddedAt   int64       `json:"added_at"`   // unix timestamp
	Method    TrustMethod `json:"method"`     // cached effective trust level; resolver is source of truth

	// Introducers lists the usernames of peers who have cryptographically
	// introduced us to this pubkey (published a SignedIntroduction we verified
	// against their verified-pinned key). Empty for broker-TOFU and
	// mutual-verify records that haven't been corroborated by anyone else.
	Introducers []string `json:"introducers,omitempty"`

	// IsBot mirrors the broker's claim at pin-creation time. Advisory —
	// the UI uses it to surface a badge; trust decisions still flow
	// through Method. A compromised broker lying about IsBot does not
	// grant any capability; it only affects how we *label* the peer.
	IsBot bool `json:"is_bot,omitempty"`
}

func peerKeyPath(username string) (string, error) {
	activeBrokerMu.RLock()
	broker := activeBroker
	activeBrokerMu.RUnlock()
	if broker == "" {
		broker = "default"
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	dir := filepath.Join(home, keyDir, broker, "publickeys")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("create publickeys dir: %w", err)
	}
	return filepath.Join(dir, username+".json"), nil
}

// loadPeerPublicKeys reads the peer file and returns all pinned records.
// Transparently handles the legacy single-object format: if the top-level
// JSON is an object rather than an array, it's wrapped in a one-element slice
// *and* the file is rewritten on disk in the array form so we migrate once
// and never parse legacy again for that user.
// Returns (nil, nil) when the file doesn't exist.
func loadPeerPublicKeys(username string) ([]TrustedKey, error) {
	path, err := peerKeyPath(username)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	// Try the array (v2) format first.
	var arr []TrustedKey
	if err := json.Unmarshal(data, &arr); err == nil {
		return arr, nil
	}
	// Fall back to the single-object (v1) format and migrate.
	var single TrustedKey
	if err := json.Unmarshal(data, &single); err != nil {
		return nil, fmt.Errorf("unmarshal trusted key(s): %w", err)
	}
	migrated := []TrustedKey{single}
	// Best-effort migration — if the rewrite fails we still return the
	// parsed entry so the caller isn't blocked; the next SavePeerPublicKey
	// will try again.
	_ = savePeerPublicKeysList(username, migrated)
	return migrated, nil
}

func savePeerPublicKeysList(username string, records []TrustedKey) error {
	path, err := peerKeyPath(username)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal trusted keys: %w", err)
	}
	return os.WriteFile(path, data, 0600)
}

// SavePeerPublicKey upserts rec into the user's key file, matching on the
// device id derived from rec.PublicKey. Preserves provenance we may already
// have recorded for this device:
//   - Verified sticks once set (mutual-verify is ground truth).
//   - Introducers are unioned — an incoming save doesn't drop earlier
//     introductions we already trust.
//
// rec.DeviceID is always overwritten with the derivation so callers can't
// forge the binding. Legacy v1 files are migrated to the array form on
// first write back.
func SavePeerPublicKey(username string, rec TrustedKey) error {
	if rec.PublicKey == "" {
		return fmt.Errorf("save peer key: public key required")
	}
	derivedID, err := DeviceIDFromPublicKeyPEM(rec.PublicKey)
	if err != nil {
		return fmt.Errorf("save peer key: derive device id: %w", err)
	}
	rec.DeviceID = derivedID
	if rec.AddedAt == 0 {
		rec.AddedAt = time.Now().Unix()
	}
	existing, err := loadPeerPublicKeys(username)
	if err != nil {
		existing = nil
	}
	replaced := false
	for i := range existing {
		if existing[i].DeviceID == rec.DeviceID {
			// Merge provenance: incoming record might be weaker (e.g. a
			// broker TOFU fetch arriving after a verified pin). Never
			// demote.
			if existing[i].Verified {
				rec.Verified = true
			}
			if existing[i].IsBot {
				rec.IsBot = true
			}
			rec.Introducers = mergeUsernames(existing[i].Introducers, rec.Introducers)
			rec.Method = effectiveMethod(rec)
			if rec.AddedAt == 0 || existing[i].AddedAt < rec.AddedAt {
				rec.AddedAt = existing[i].AddedAt
			}
			existing[i] = rec
			replaced = true
			break
		}
	}
	if !replaced {
		rec.Method = effectiveMethod(rec)
		existing = append(existing, rec)
	}
	return savePeerPublicKeysList(username, existing)
}

// AddIntroduction records that `introducer` has vouched for (username,
// publicKeyPEM). Callers are expected to have already verified the
// introduction's signature against the introducer's verified pinned key;
// this function enforces a defense-in-depth precondition that `introducer`
// actually has a verified pin on disk before any of their vouches enter
// local trust state — an unverified introducer's "introduction" has no
// weight in the web-of-trust, so we refuse to record it.
//
// Creates a TrustedKey row if no prior record exists for this
// (username, device_id) pair; otherwise appends to the Introducers list
// (deduplicated).
// AddIntroduction records that introducer has vouched for (username, publicKeyPEM).
// Returns (true, nil) when the trust state actually changed (new pin or new
// introducer appended), (false, nil) when the record was already present, and
// (false, err) on any error. Callers that only care about errors may discard
// the bool; callers that want to count genuinely new introductions (e.g.
// applyIntroductions) use it to avoid false positives on repeat polls.
func AddIntroduction(username, publicKeyPEM, introducer string) (bool, error) {
	if introducer == "" {
		return false, fmt.Errorf("add introduction: introducer required")
	}
	if !hasVerifiedPin(introducer) {
		return false, fmt.Errorf("add introduction: introducer %q is not verified locally — refusing to record their vouch", introducer)
	}
	derivedID, err := DeviceIDFromPublicKeyPEM(publicKeyPEM)
	if err != nil {
		return false, fmt.Errorf("add introduction: derive device id: %w", err)
	}
	existing, err := loadPeerPublicKeys(username)
	if err != nil {
		existing = nil
	}
	for i := range existing {
		if existing[i].DeviceID == derivedID {
			merged := mergeUsernames(existing[i].Introducers, []string{introducer})
			if len(merged) == len(existing[i].Introducers) {
				return false, nil // introducer already recorded — no disk write needed
			}
			existing[i].Introducers = merged
			existing[i].Method = effectiveMethod(existing[i])
			return true, savePeerPublicKeysList(username, existing)
		}
	}
	rec := TrustedKey{
		DeviceID:    derivedID,
		PublicKey:   publicKeyPEM,
		AddedAt:     time.Now().Unix(),
		Introducers: []string{introducer},
	}
	rec.Method = effectiveMethod(rec)
	existing = append(existing, rec)
	return true, savePeerPublicKeysList(username, existing)
}

// mergeUsernames returns a sorted de-duplicated union of a and b.
func mergeUsernames(a, b []string) []string {
	set := make(map[string]struct{}, len(a)+len(b))
	for _, u := range a {
		if u != "" {
			set[u] = struct{}{}
		}
	}
	for _, u := range b {
		if u != "" {
			set[u] = struct{}{}
		}
	}
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for u := range set {
		out = append(out, u)
	}
	sort.Strings(out)
	return out
}

// hasVerifiedPin reports whether any on-disk record for username is
// marked Verified — i.e. whether we've ever completed mutual-verify
// against this user. Used as a precondition gate for functions that
// treat a username as authoritative (like AddIntroduction).
func hasVerifiedPin(username string) bool {
	recs, err := loadPeerPublicKeys(username)
	if err != nil {
		return false
	}
	for _, r := range recs {
		if r.Verified {
			return true
		}
	}
	return false
}

// effectiveMethod returns the trust level a record should advertise,
// given its current Verified/Introducers state.
func effectiveMethod(rec TrustedKey) TrustMethod {
	switch {
	case rec.Verified:
		return TrustMethodVerified
	case len(rec.Introducers) > 0:
		return TrustMethodIntroduced
	default:
		return TrustMethodTOFU
	}
}

// LoadPeerPublicKey returns the user's primary pinned key — preferring the
// root device (empty DeviceID) and falling back to whichever entry exists
// if none is marked as root. Use this when you don't care which specific
// device signed something.
func LoadPeerPublicKey(username string) (TrustedKey, bool) {
	recs, err := loadPeerPublicKeys(username)
	if err != nil || len(recs) == 0 {
		return TrustedKey{}, false
	}
	for _, r := range recs {
		if r.DeviceID == "" {
			return r, true
		}
	}
	return recs[0], true
}

// LoadPeerPublicKeyByDevice returns the pinned key for a specific
// (username, deviceID). Empty deviceID selects the root device. Used by
// the room-key manifest verifier since the manifest names which device
// signed it.
func LoadPeerPublicKeyByDevice(username, deviceID string) (TrustedKey, bool) {
	recs, err := loadPeerPublicKeys(username)
	if err != nil {
		return TrustedKey{}, false
	}
	for _, r := range recs {
		if r.DeviceID == deviceID {
			return r, true
		}
	}
	return TrustedKey{}, false
}

// LoadAllPeerPublicKeys returns every pinned key for a user, one per device.
func LoadAllPeerPublicKeys(username string) []TrustedKey {
	recs, _ := loadPeerPublicKeys(username)
	return recs
}

// HasPeerPublicKey reports whether any trusted key record exists for the peer.
func HasPeerPublicKey(username string) bool {
	_, ok := LoadPeerPublicKey(username)
	return ok
}
