// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package keys_test

import (
	"strings"
	"testing"

	"github.com/srschreiber/nito-client/engine/keys"
	apitypes "github.com/srschreiber/nito-client/shared/api_types"
)

// newIntroduction builds a filled-in SignedIntroduction that the alice
// fixture can sign. Caller invokes keys.SignIntroduction to populate
// VerifiedSignature.
func newIntroduction(t *testing.T, targetUsername, targetPub, signerUsername, signerPub string) apitypes.SignedIntroduction {
	t.Helper()
	signerDevice, err := keys.DeviceIDFromPublicKeyPEM(signerPub)
	if err != nil {
		t.Fatalf("derive signer device id: %v", err)
	}
	return apitypes.SignedIntroduction{
		Username:           targetUsername,
		PublicKey:          targetPub,
		VerifiedByUsername: signerUsername,
		VerifiedByDeviceID: signerDevice,
	}
}

// TestIntroductionRoundTrip: alice signs an introduction for bob's key,
// verifier uses alice's pubkey → should verify. The canonical signable
// form includes alice's derived device id, so this also confirms the
// self-reference produces identical bytes across sign and verify.
func TestIntroductionRoundTrip(t *testing.T) {
	alicePub, bobPub := setupTwoParties(t)
	intro := newIntroduction(t, "bob", bobPub, "alice", alicePub)
	sig, err := keys.SignIntroduction(&intro, "alice")
	if err != nil {
		t.Fatalf("SignIntroduction: %v", err)
	}
	intro.VerifiedSignature = sig

	if err := keys.VerifyIntroduction(&intro, alicePub); err != nil {
		t.Fatalf("honest introduction failed to verify: %v", err)
	}
}

// TestIntroductionTamperedPublicKeyFails: swapping out the target's
// PublicKey after signing must invalidate the signature. Because
// DeviceID is derived from PublicKey at verify time, replacing the key
// changes the canonical signable bytes even though the signature was
// produced over the original.
func TestIntroductionTamperedPublicKeyFails(t *testing.T) {
	alicePub, bobPub := setupTwoParties(t)
	intro := newIntroduction(t, "bob", bobPub, "alice", alicePub)
	sig, err := keys.SignIntroduction(&intro, "alice")
	if err != nil {
		t.Fatalf("SignIntroduction: %v", err)
	}
	intro.VerifiedSignature = sig

	// Swap in alice's pubkey as if attacker tried to relabel the
	// introduction's target.
	intro.PublicKey = alicePub
	if err := keys.VerifyIntroduction(&intro, alicePub); err == nil {
		t.Fatal("verifier accepted an introduction with tampered target PublicKey")
	}
}

// TestIntroductionTamperedTargetUsernameFails: swapping the Username
// after signing invalidates the signature since Username is in the
// canonical signable form.
func TestIntroductionTamperedTargetUsernameFails(t *testing.T) {
	alicePub, bobPub := setupTwoParties(t)
	intro := newIntroduction(t, "bob", bobPub, "alice", alicePub)
	sig, err := keys.SignIntroduction(&intro, "alice")
	if err != nil {
		t.Fatalf("SignIntroduction: %v", err)
	}
	intro.VerifiedSignature = sig

	intro.Username = "mallory"
	if err := keys.VerifyIntroduction(&intro, alicePub); err == nil {
		t.Fatal("verifier accepted an introduction with tampered Username")
	}
}

// TestIntroductionVerifierDeviceIDMismatchFails: if VerifiedByDeviceID
// doesn't match the derivation of verifierPubPEM, verification must
// fail before the signature is even checked. This stops the broker
// from handing a peer the wrong (signer_username, verifier_pubkey) pair.
func TestIntroductionVerifierDeviceIDMismatchFails(t *testing.T) {
	alicePub, bobPub := setupTwoParties(t)
	intro := newIntroduction(t, "bob", bobPub, "alice", alicePub)
	sig, err := keys.SignIntroduction(&intro, "alice")
	if err != nil {
		t.Fatalf("SignIntroduction: %v", err)
	}
	intro.VerifiedSignature = sig

	// Now verify with bob's pubkey. Device id in intro references
	// alice's derivation, not bob's — derivation mismatch should trip
	// before the RSA signature check.
	err = keys.VerifyIntroduction(&intro, bobPub)
	if err == nil {
		t.Fatal("verifier accepted an introduction against the wrong pubkey")
	}
	if !strings.Contains(err.Error(), "device id mismatch") {
		t.Fatalf("expected device id mismatch error, got: %v", err)
	}
}

// TestSignIntroductionRejectsImpersonation: SignIntroduction must
// refuse to produce a signature attributing the vouch to someone other
// than the actual signer. This is a safety rail so a buggy UI path
// can't emit introductions naming the wrong VerifiedByUsername.
func TestSignIntroductionRejectsImpersonation(t *testing.T) {
	alicePub, bobPub := setupTwoParties(t)
	// intro names alice as signer, but we'll ask "bob" to sign it.
	intro := newIntroduction(t, "bob", bobPub, "alice", alicePub)
	_, err := keys.SignIntroduction(&intro, "bob")
	if err == nil {
		t.Fatal("SignIntroduction produced a signature attributing alice's vouch to bob's key")
	}
}

// TestAddIntroductionRefusesUnverifiedIntroducer: defense-in-depth
// precondition — AddIntroduction should refuse to record a vouch from
// a username that isn't verified locally. An unverified introducer
// has no trust weight in the resolver.
func TestAddIntroductionRefusesUnverifiedIntroducer(t *testing.T) {
	dir := t.TempDir()
	keys.SetActiveBroker(dir)
	t.Cleanup(func() { keys.SetActiveBroker("") })

	_, bobPub := setupTwoPartiesInBroker(t, dir)
	// "alice" (the introducer) has never been verified locally.
	err := keys.AddIntroduction("bob", bobPub, "alice")
	if err == nil {
		t.Fatal("AddIntroduction accepted a vouch from a non-verified introducer")
	}
	if !strings.Contains(err.Error(), "not verified") {
		t.Fatalf("expected 'not verified' error, got: %v", err)
	}
}

// setupTwoPartiesInBroker is like setupTwoParties but takes an explicit
// broker path so a test can save a verified pin for one party before
// the other is generated. Used by AddIntroduction tests.
func setupTwoPartiesInBroker(t *testing.T, dir string) (alicePub, bobPub string) {
	t.Helper()
	keys.SetActiveBroker(dir)
	var err error
	if alicePub, err = keys.LoadOrGenerate("alice"); err != nil {
		t.Fatalf("LoadOrGenerate(alice): %v", err)
	}
	if bobPub, err = keys.LoadOrGenerate("bob"); err != nil {
		t.Fatalf("LoadOrGenerate(bob): %v", err)
	}
	return alicePub, bobPub
}

// TestVerifyRoomAttestationRoundTrip confirms a room-creation signature
// produced via SignAttestation verifies cleanly through the helper using
// the same canonical pairs.
func TestVerifyRoomAttestationRoundTrip(t *testing.T) {
	alicePub, _ := setupTwoParties(t)
	aliceDevice, err := keys.DeviceIDFromPublicKeyPEM(alicePub)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	sig, err := keys.SignAttestation(map[string]string{
		"device_id": aliceDevice,
		"name":      "general",
		"owner":     "alice",
	}, "alice")
	if err != nil {
		t.Fatalf("SignAttestation: %v", err)
	}
	if err := keys.VerifyRoomAttestation("general", "alice", aliceDevice, sig, alicePub); err != nil {
		t.Fatalf("honest room attestation failed to verify: %v", err)
	}
}

// TestVerifyRoomAttestationRejectsTamperedName confirms swapping the
// room name after signing invalidates the signature. This is the
// property that stops a broker relabeling rooms.
func TestVerifyRoomAttestationRejectsTamperedName(t *testing.T) {
	alicePub, _ := setupTwoParties(t)
	aliceDevice, err := keys.DeviceIDFromPublicKeyPEM(alicePub)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	sig, err := keys.SignAttestation(map[string]string{
		"device_id": aliceDevice,
		"name":      "general",
		"owner":     "alice",
	}, "alice")
	if err != nil {
		t.Fatalf("SignAttestation: %v", err)
	}
	if err := keys.VerifyRoomAttestation("private-channel", "alice", aliceDevice, sig, alicePub); err == nil {
		t.Fatal("verifier accepted a room attestation with tampered name")
	}
}

// TestVerifyRoomAttestationRejectsWrongSigner confirms that a signature
// made by alice does not verify under bob's pubkey.
func TestVerifyRoomAttestationRejectsWrongSigner(t *testing.T) {
	alicePub, bobPub := setupTwoParties(t)
	aliceDevice, err := keys.DeviceIDFromPublicKeyPEM(alicePub)
	if err != nil {
		t.Fatalf("derive alice: %v", err)
	}
	bobDevice, err := keys.DeviceIDFromPublicKeyPEM(bobPub)
	if err != nil {
		t.Fatalf("derive bob: %v", err)
	}
	sig, err := keys.SignAttestation(map[string]string{
		"device_id": aliceDevice,
		"name":      "general",
		"owner":     "alice",
	}, "alice")
	if err != nil {
		t.Fatalf("SignAttestation: %v", err)
	}
	// Verifier is given bob's pubkey but the signer is alice. The
	// device id mismatch check trips before the RSA verify.
	if err := keys.VerifyRoomAttestation("general", "alice", bobDevice, sig, bobPub); err == nil {
		t.Fatal("verifier accepted a room attestation against the wrong signer")
	}
}

// TestVerifyRoomAttestationDeviceIDMismatch confirms a broker-supplied
// device_id that doesn't match the resolved pubkey's derivation is
// rejected before the signature is checked.
func TestVerifyRoomAttestationDeviceIDMismatch(t *testing.T) {
	alicePub, _ := setupTwoParties(t)
	aliceDevice, err := keys.DeviceIDFromPublicKeyPEM(alicePub)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	sig, err := keys.SignAttestation(map[string]string{
		"device_id": aliceDevice,
		"name":      "general",
		"owner":     "alice",
	}, "alice")
	if err != nil {
		t.Fatalf("SignAttestation: %v", err)
	}
	// Claim the wrong device id while passing alice's actual pubkey.
	bogusDevice := "b" + aliceDevice[1:] // same length, different value
	err = keys.VerifyRoomAttestation("general", "alice", bogusDevice, sig, alicePub)
	if err == nil {
		t.Fatal("verifier accepted a mismatched device_id")
	}
}
