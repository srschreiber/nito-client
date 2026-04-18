// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package keys

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
	DeviceID  string      `json:"device_id"`  // reserved for multi-device; empty for now
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

// SavePeerPublicKey writes a TrustedKey record for the given peer.
func SavePeerPublicKey(username string, rec TrustedKey) error {
	if rec.AddedAt == 0 {
		rec.AddedAt = time.Now().Unix()
	}
	path, err := peerKeyPath(username)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal trusted key: %w", err)
	}
	return os.WriteFile(path, data, 0600)
}

// LoadPeerPublicKey returns the stored TrustedKey for a peer, or (zero, false) if absent.
func LoadPeerPublicKey(username string) (TrustedKey, bool) {
	path, err := peerKeyPath(username)
	if err != nil {
		return TrustedKey{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return TrustedKey{}, false
	}
	var rec TrustedKey
	if err := json.Unmarshal(data, &rec); err != nil {
		return TrustedKey{}, false
	}
	return rec, true
}

// HasPeerPublicKey reports whether a trusted key record exists for the peer.
func HasPeerPublicKey(username string) bool {
	_, ok := LoadPeerPublicKey(username)
	return ok
}
