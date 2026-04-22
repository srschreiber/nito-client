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
// Canonical signable form:
//
//	<Username>:<DeviceID>;INTRODUCED_BY:<VerifiedByUsername>:<VerifiedByDeviceID>
//
// DeviceID is re-derived from PublicKey at sign *and* verify time —
// deriving rather than reading a struct field means an attacker can't
// swap the PublicKey bytes without breaking the signature. Both device
// ids are sha256(DER(pub_key)) hex, same derivation used everywhere.

import (
	"fmt"

	apitypes "github.com/srschreiber/nito-client/shared/api_types"
)

// introductionSignable builds the canonical bytes described above for
// intro. Returns an error if the target PublicKey is unparseable.
func introductionSignable(intro *apitypes.SignedIntroduction) (string, error) {
	if intro.PublicKey == "" {
		return "", fmt.Errorf("introduction: empty public key")
	}
	targetDevice, err := DeviceIDFromPublicKeyPEM(intro.PublicKey)
	if err != nil {
		return "", fmt.Errorf("introduction: derive target device id: %w", err)
	}
	return fmt.Sprintf("%s:%s;INTRODUCED_BY:%s:%s",
		intro.Username, targetDevice,
		intro.VerifiedByUsername, intro.VerifiedByDeviceID,
	), nil
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
	msg, err := introductionSignable(intro)
	if err != nil {
		return "", err
	}
	return Sign(msg, signerUsername)
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
	msg, err := introductionSignable(intro)
	if err != nil {
		return err
	}
	return VerifyWithPublicKey(msg, intro.VerifiedSignature, verifierPubPEM)
}
