// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package keys

// Device IDs are deterministic: they are SHA-256 hex digests over the DER
// (PKIX-encoded) bytes of a device's RSA public key. Clients derive them
// locally from any public key they hold — a peer's pinned PEM or their own
// on-disk copy — so the broker can never swap a device id to one we've
// already pinned. Every signature path that carries a device id
// (manifest.device_id, rooms.signed_by_device_id, room_members.signature's
// device_id) is cross-checked against this derivation before the signature
// itself is trusted: a mismatch means the key we're verifying with is not
// the key that was claimed to have signed, and we reject.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/pem"
	"fmt"
)

// DeviceIDFromPublicKeyPEM returns the canonical device id for the given
// PEM-encoded RSA public key. Hashing the inner DER (PKIX) bytes — not the
// PEM envelope — means whitespace or line-wrapping variants of the same key
// produce identical ids.
func DeviceIDFromPublicKeyPEM(publicKeyPEM string) (string, error) {
	block, _ := pem.Decode([]byte(publicKeyPEM))
	if block == nil {
		return "", fmt.Errorf("device id: decode public key PEM: no block found")
	}
	if len(block.Bytes) == 0 {
		return "", fmt.Errorf("device id: empty DER bytes")
	}
	sum := sha256.Sum256(block.Bytes)
	return hex.EncodeToString(sum[:]), nil
}
