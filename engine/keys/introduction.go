// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package keys

// Signed introductions are the web-of-trust primitive: after two users
// mutual-verify each other, each side publishes a short statement of the
// form "I saw username X use public key P, signed by me (my derived
// device id)". Other users who have already verified *me* can then
// upgrade their trust in X from broker-TOFU to "introduced" without
// running the full handshake themselves.
//
// The canonical signable form uses the same map-based attestation
// machinery as rooms.signature / room_members.signature: keys sorted
// alphabetically, values joined by ';'. The target's pubkey enters the
// signable form as its derived device_id (sha256(DER(pub_key)) hex)
// rather than the raw PEM — deriving rather than hashing the PEM string
// means PEM whitespace variants produce identical bytes, and an
// attacker can't swap the pubkey bytes without breaking the signature.

import (
	"fmt"

	apitypes "github.com/srschreiber/nito-client/shared/api_types"
)

// introductionAttestation builds the key/value pairs that form the
// canonical bytes signed/verified for intro. "device_id" is the target's
// derivation; "verified_by_device_id" is the signer's.
func introductionAttestation(intro *apitypes.SignedIntroduction) (map[string]string, error) {
	if intro.PublicKey == "" {
		return nil, fmt.Errorf("introduction: empty public key")
	}
	targetDevice, err := DeviceIDFromPublicKeyPEM(intro.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("introduction: derive target device id: %w", err)
	}
	return map[string]string{
		"device_id":             targetDevice,
		"username":              intro.Username,
		"verified_by_device_id": intro.VerifiedByDeviceID,
		"verified_by_username":  intro.VerifiedByUsername,
	}, nil
}

// SignIntroduction signs the canonical form of intro with signerUsername's
// private key. intro.VerifiedByUsername must equal signerUsername and
// intro.VerifiedByDeviceID must be the derivation of signerUsername's
// public key — we enforce both to prevent constructing a signed object
// attributed to someone other than the actual signer.
func SignIntroduction(intro *apitypes.SignedIntroduction, signerUsername string) (string, error) {
	if intro.VerifiedByUsername != signerUsername {
		return "", fmt.Errorf("introduction: VerifiedByUsername (%q) does not match signer (%q)", intro.VerifiedByUsername, signerUsername)
	}
	signerPub, err := LoadPublicKeyPEM(signerUsername)
	if err != nil {
		return "", fmt.Errorf("introduction: load signer public key: %w", err)
	}
	signerDevice, err := DeviceIDFromPublicKeyPEM(signerPub)
	if err != nil {
		return "", fmt.Errorf("introduction: derive signer device id: %w", err)
	}
	if intro.VerifiedByDeviceID != signerDevice {
		return "", fmt.Errorf("introduction: VerifiedByDeviceID (%q) does not match signer derivation (%q)", intro.VerifiedByDeviceID, signerDevice)
	}
	pairs, err := introductionAttestation(intro)
	if err != nil {
		return "", err
	}
	return SignAttestation(pairs, signerUsername)
}

// VerifyIntroduction verifies intro.VerifiedSignature against the supplied
// verifier pubkey PEM. Caller is responsible for obtaining verifierPubPEM
// from a trusted source (the caller's own verified pin for the introducer)
// — this function intentionally does not look up trust state, so it can
// be unit-tested in isolation.
//
// Additionally enforces that intro.VerifiedByDeviceID matches the
// derivation of verifierPubPEM: no point verifying a signature against a
// key whose id doesn't match what the introduction claims.
func VerifyIntroduction(intro *apitypes.SignedIntroduction, verifierPubPEM string) error {
	derivedVerifier, err := DeviceIDFromPublicKeyPEM(verifierPubPEM)
	if err != nil {
		return fmt.Errorf("verify introduction: derive verifier device id: %w", err)
	}
	if intro.VerifiedByDeviceID != derivedVerifier {
		return fmt.Errorf("verify introduction: device id mismatch (claimed=%s derived=%s)", intro.VerifiedByDeviceID, derivedVerifier)
	}
	pairs, err := introductionAttestation(intro)
	if err != nil {
		return err
	}
	return VerifyAttestation(pairs, intro.VerifiedSignature, verifierPubPEM)
}

// VerifyRoomAttestation verifies a rooms.signature over the canonical
// pairs {device_id, name, owner}. signerPubPEM must be the resolved
// public key for `owner` (obtained from ResolvePeerPublicKey; caller
// decides whether verified-only or introduced-also is acceptable).
//
// Enforces that signerDeviceID matches the derivation of signerPubPEM —
// same binding check as manifest / introduction verification — so the
// broker can't hand us a valid signature paired with a device_id it
// didn't actually come from.
func VerifyRoomAttestation(roomName, owner, signerDeviceID, signature, signerPubPEM string) error {
	if signature == "" {
		return fmt.Errorf("verify room attestation: empty signature")
	}
	derivedSigner, err := DeviceIDFromPublicKeyPEM(signerPubPEM)
	if err != nil {
		return fmt.Errorf("verify room attestation: derive signer device id: %w", err)
	}
	if signerDeviceID != derivedSigner {
		return fmt.Errorf("verify room attestation: device id mismatch (claimed=%s derived=%s)", signerDeviceID, derivedSigner)
	}
	pairs := map[string]string{
		"device_id": signerDeviceID,
		"name":      roomName,
		"owner":     owner,
	}
	return VerifyAttestation(pairs, signature, signerPubPEM)
}
