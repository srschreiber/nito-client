// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package keys_test

import (
	"testing"
	"time"

	"github.com/srschreiber/nito-client/engine/keys"
	apitypes "github.com/srschreiber/nito-client/shared/api_types"
)

// newManifestFor returns a fresh, valid manifest whose DeviceID matches the
// derivation of signerPub. Callers tamper fields and re-check verification.
func newManifestFor(t *testing.T, signerPub string, rotator string) apitypes.RoomKeyManifest {
	t.Helper()
	dev, err := keys.DeviceIDFromPublicKeyPEM(signerPub)
	if err != nil {
		t.Fatalf("derive device id: %v", err)
	}
	nonce, err := keys.GenerateManifestNonce()
	if err != nil {
		t.Fatalf("generate nonce: %v", err)
	}
	return apitypes.RoomKeyManifest{
		CurKeyHash:     keys.HashRoomKey([]byte("room-key-bytes")),
		CurVersionNum:  1,
		DeviceID:       dev,
		Nonce:          nonce,
		PrevKeyHash:    "",
		PrevVersionNum: 0,
		RotatedBy:      rotator,
		Timestamp:      time.Now().Unix(),
	}
}

// TestManifestRoundTripWithDerivedDeviceID is the happy path: a manifest
// signed by alice carries DeviceID = sha256(alice pubkey DER), and verifies
// cleanly against alice's stored PEM.
func TestManifestRoundTripWithDerivedDeviceID(t *testing.T) {
	alicePub, _ := setupTwoParties(t)

	m := newManifestFor(t, alicePub, "alice")
	sig, err := keys.SignRoomKeyManifest(&m, "alice")
	if err != nil {
		t.Fatalf("SignRoomKeyManifest: %v", err)
	}
	if err := keys.VerifyRoomKeyManifest(&m, sig, alicePub); err != nil {
		t.Fatalf("verify failed for honest manifest: %v", err)
	}
}

// TestManifestDeviceIDTamperingFailsVerification confirms that a broker
// swapping the declared device id after signing invalidates the signature
// (because DeviceID is part of the signable bytes). This is the property
// that keeps device ids honest on the wire.
func TestManifestDeviceIDTamperingFailsVerification(t *testing.T) {
	alicePub, bobPub := setupTwoParties(t)

	m := newManifestFor(t, alicePub, "alice")
	sig, err := keys.SignRoomKeyManifest(&m, "alice")
	if err != nil {
		t.Fatalf("SignRoomKeyManifest: %v", err)
	}

	// Swap DeviceID to bob's derivation while keeping the signature.
	bobDev, err := keys.DeviceIDFromPublicKeyPEM(bobPub)
	if err != nil {
		t.Fatalf("derive bob: %v", err)
	}
	m.DeviceID = bobDev
	if err := keys.VerifyRoomKeyManifest(&m, sig, alicePub); err == nil {
		t.Fatal("verify accepted a manifest whose DeviceID was tampered after signing")
	}
}

// TestManifestVerifyRejectsWrongKey confirms a manifest signed by alice does
// not verify against bob's public key — the broker can't hand us a different
// user's key and have the signature coincidentally check out.
func TestManifestVerifyRejectsWrongKey(t *testing.T) {
	alicePub, bobPub := setupTwoParties(t)

	m := newManifestFor(t, alicePub, "alice")
	sig, err := keys.SignRoomKeyManifest(&m, "alice")
	if err != nil {
		t.Fatalf("SignRoomKeyManifest: %v", err)
	}
	if err := keys.VerifyRoomKeyManifest(&m, sig, bobPub); err == nil {
		t.Fatal("verify accepted alice's signature against bob's public key")
	}
}
