// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package keys

// Room-key manifest signing + verification. The manifest is the signed
// metadata accompanying every room key version; it lets every member detect
// if the broker silently substitutes a different key.
//
// Canonical signable form: fields concatenated alphabetically (by JSON key)
// with ';' separators. Numeric fields are stringified via strconv; missing
// prev_* fields (first key) and nil device_id (root device) serialise as 0
// or empty string.
//
//   cur_key_hash;cur_version_num;device_id;nonce;prev_key_hash;prev_version_num;rotated_by;timestamp

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"sort"
	"strconv"
	"strings"

	apitypes "github.com/srschreiber/nito-client/shared/api_types"
)

// HashRoomKey returns the hex-encoded SHA-256 digest of a raw room key.
// The manifest stores these as {prev,cur}_key_hash so members can check
// they have the same key bytes the rotator signed.
func HashRoomKey(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// GenerateManifestNonce returns 16 random bytes base64-encoded, suitable for
// the manifest's nonce field. Prevents the broker (or an attacker replaying a
// stale manifest) from reusing an old signature for a new version.
func GenerateManifestNonce() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("manifest nonce: %w", err)
	}
	return base64.RawStdEncoding.EncodeToString(b), nil
}

// manifestSignable builds the canonical bytes described above. A nil DeviceID
// (root device) is serialised as an empty string so both signer and verifier
// produce identical bytes regardless of whether the pointer is set.
func manifestSignable(m *apitypes.RoomKeyManifest) string {
	deviceID := ""
	if m.DeviceID != nil {
		deviceID = *m.DeviceID
	}
	return strings.Join([]string{
		m.CurKeyHash,
		strconv.Itoa(m.CurVersionNum),
		deviceID,
		m.Nonce,
		m.PrevKeyHash,
		strconv.Itoa(m.PrevVersionNum),
		m.RotatedBy,
		strconv.FormatInt(m.Timestamp, 10),
	}, ";")
}

// SignRoomKeyManifest signs the canonical form of m with the local user's
// private key. Returns a base64 RSA-PSS-SHA256 signature suitable for the
// VersionManifestSignature field on the CreateRoom / GetRoomKey wire types.
func SignRoomKeyManifest(m *apitypes.RoomKeyManifest, username string) (string, error) {
	return Sign(manifestSignable(m), username)
}

// VerifyRoomKeyManifest checks sigB64 over manifestSignable(m) using
// publicKeyPEM (typically the rotator's key, fetched via the users endpoint).
// Returns nil if the signature is valid.
func VerifyRoomKeyManifest(m *apitypes.RoomKeyManifest, sigB64, publicKeyPEM string) error {
	return VerifyWithPublicKey(manifestSignable(m), sigB64, publicKeyPEM)
}

// SignAttestation signs a canonical "field1;field2;...;fieldN" string where
// the provided pairs have been sorted alphabetically by key and joined by ';'.
// Shared by row-level attestations (rooms.signature, room_members.signature,
// user_room_roles.signature) that need deterministic bytes independent of
// JSON key ordering. Values must not themselves contain ';'.
func SignAttestation(pairs map[string]string, username string) (string, error) {
	keysSorted := make([]string, 0, len(pairs))
	for k := range pairs {
		keysSorted = append(keysSorted, k)
	}
	sort.Strings(keysSorted)
	parts := make([]string, len(keysSorted))
	for i, k := range keysSorted {
		parts[i] = pairs[k]
	}
	return Sign(strings.Join(parts, ";"), username)
}

// VerifyWithPublicKey verifies sigB64 (base64 RSA-PSS-SHA256) over message
// using a PKIX-encoded PEM public key. Generic counterpart to Sign — callers
// that build their own signable bytes can verify with any peer's key.
func VerifyWithPublicKey(message, sigB64, publicKeyPEM string) error {
	block, _ := pem.Decode([]byte(publicKeyPEM))
	if block == nil {
		return fmt.Errorf("decode public key PEM: no block found")
	}
	pubIface, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return fmt.Errorf("parse public key: %w", err)
	}
	rsaPub, ok := pubIface.(*rsa.PublicKey)
	if !ok {
		return fmt.Errorf("expected RSA public key")
	}
	sig, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}
	h := sha256.Sum256([]byte(message))
	if err := rsa.VerifyPSS(rsaPub, crypto.SHA256, h[:], sig, nil); err != nil {
		return fmt.Errorf("signature verification failed: %w", err)
	}
	return nil
}
