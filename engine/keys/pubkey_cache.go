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
	"time"
)

// TrustMethod describes how a peer's public key was trusted.
type TrustMethod string

const (
	TrustMethodTOFU     TrustMethod = "tofu"
	TrustMethodVerified TrustMethod = "verified"
)

// TrustedKey is the on-disk record for a peer's trusted public key.
type TrustedKey struct {
	DeviceID  string      `json:"device_id"`  // empty string = root device
	PublicKey string      `json:"public_key"` // PEM-encoded RSA public key
	Verified  bool        `json:"verified"`
	AddedAt   int64       `json:"added_at"` // unix timestamp
	Method    TrustMethod `json:"method"`
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

// SavePeerPublicKey upserts rec into the user's key file, matching on
// DeviceID: an existing entry for the same device is replaced, otherwise
// the record is appended. Legacy v1 files are migrated to the array form
// the first time they're written back.
func SavePeerPublicKey(username string, rec TrustedKey) error {
	if rec.AddedAt == 0 {
		rec.AddedAt = time.Now().Unix()
	}
	existing, err := loadPeerPublicKeys(username)
	if err != nil {
		// File exists but is unreadable — start fresh rather than fail silently.
		existing = nil
	}
	replaced := false
	for i := range existing {
		if existing[i].DeviceID == rec.DeviceID {
			existing[i] = rec
			replaced = true
			break
		}
	}
	if !replaced {
		existing = append(existing, rec)
	}
	return savePeerPublicKeysList(username, existing)
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
