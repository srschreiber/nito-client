// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package keys_test

import (
	"strings"
	"testing"

	"github.com/srschreiber/nito-client/engine/keys"
)

// TestDeviceIDDeterministic confirms the same public key produces the same
// device id across independent calls. The whole security model rides on this.
func TestDeviceIDDeterministic(t *testing.T) {
	_, pubPEM := genKeyPair(t)
	a, err := keys.DeviceIDFromPublicKeyPEM(pubPEM)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	b, err := keys.DeviceIDFromPublicKeyPEM(pubPEM)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if a != b {
		t.Fatalf("device id not deterministic: %s vs %s", a, b)
	}
	if len(a) != 64 {
		t.Fatalf("expected 64-char hex id, got %d chars: %s", len(a), a)
	}
}

// TestDeviceIDDistinctPerKey confirms different keypairs produce different
// device ids — otherwise an attacker could coincidentally collide.
func TestDeviceIDDistinctPerKey(t *testing.T) {
	_, pubA := genKeyPair(t)
	_, pubB := genKeyPair(t)
	idA, err := keys.DeviceIDFromPublicKeyPEM(pubA)
	if err != nil {
		t.Fatalf("derive A: %v", err)
	}
	idB, err := keys.DeviceIDFromPublicKeyPEM(pubB)
	if err != nil {
		t.Fatalf("derive B: %v", err)
	}
	if idA == idB {
		t.Fatalf("distinct keys produced identical device id: %s", idA)
	}
}

// TestDeviceIDIgnoresPEMWhitespace confirms the derivation hashes the DER
// bytes, not the PEM envelope — trailing newlines and re-wrapped line breaks
// must produce the same id. A PEM-level hash would make the id brittle to
// cosmetic reformatting.
func TestDeviceIDIgnoresPEMWhitespace(t *testing.T) {
	_, pubPEM := genKeyPair(t)
	base, err := keys.DeviceIDFromPublicKeyPEM(pubPEM)
	if err != nil {
		t.Fatalf("derive base: %v", err)
	}
	withExtraNewlines := pubPEM + "\n\n"
	derived, err := keys.DeviceIDFromPublicKeyPEM(withExtraNewlines)
	if err != nil {
		t.Fatalf("derive with whitespace: %v", err)
	}
	if derived != base {
		t.Fatalf("PEM whitespace changed derived id: base=%s derived=%s", base, derived)
	}
}

func TestDeviceIDRejectsGarbage(t *testing.T) {
	if _, err := keys.DeviceIDFromPublicKeyPEM(""); err == nil {
		t.Fatal("expected error for empty PEM")
	}
	if _, err := keys.DeviceIDFromPublicKeyPEM("not a pem"); err == nil {
		t.Fatal("expected error for invalid PEM")
	}
}

// TestSavePeerPublicKeyDerivesDeviceID confirms SavePeerPublicKey ignores
// any caller-provided DeviceID and overwrites it with the derivation. This
// is the invariant that stops a bug elsewhere from persisting a forged
// (pub_key, device_id) pair.
func TestSavePeerPublicKeyDerivesDeviceID(t *testing.T) {
	dir := t.TempDir()
	keys.SetActiveBroker(dir)
	t.Cleanup(func() { keys.SetActiveBroker("") })

	_, pubPEM := genKeyPair(t)
	want, err := keys.DeviceIDFromPublicKeyPEM(pubPEM)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}

	// Pass a bogus DeviceID and confirm it is overwritten on save.
	if err := keys.SavePeerPublicKey("alice", keys.TrustedKey{
		DeviceID:  strings.Repeat("a", 64),
		PublicKey: pubPEM,
		Verified:  true,
		Method:    keys.TrustMethodVerified,
	}); err != nil {
		t.Fatalf("SavePeerPublicKey: %v", err)
	}

	loaded, ok := keys.LoadPeerPublicKeyByDevice("alice", want)
	if !ok {
		t.Fatalf("LoadPeerPublicKeyByDevice(%s) returned false — derivation not applied", want)
	}
	if loaded.DeviceID != want {
		t.Fatalf("stored DeviceID = %s, want %s", loaded.DeviceID, want)
	}
	if loaded.PublicKey != pubPEM {
		t.Fatal("stored PublicKey does not match input")
	}
}

func TestSavePeerPublicKeyRejectsEmptyKey(t *testing.T) {
	dir := t.TempDir()
	keys.SetActiveBroker(dir)
	t.Cleanup(func() { keys.SetActiveBroker("") })

	if err := keys.SavePeerPublicKey("alice", keys.TrustedKey{}); err == nil {
		t.Fatal("expected error when PublicKey is empty")
	}
}
