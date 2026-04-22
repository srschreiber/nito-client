// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package keys_test

import (
	"testing"

	"github.com/srschreiber/nito-client/engine/keys"
)

// pinVerified saves a verified pin for username at a temp-dir-scoped
// broker. Separate helper so individual resolver tests don't repeat the
// ritual of generating keys + marking them verified.
func pinVerified(t *testing.T, username, pub string) {
	t.Helper()
	if err := keys.SavePeerPublicKey(username, keys.TrustedKey{
		PublicKey: pub,
		Verified:  true,
		Method:    keys.TrustMethodVerified,
	}); err != nil {
		t.Fatalf("pinVerified %s: %v", username, err)
	}
}

// TestResolveNothingPinned: with no records, the resolver reports
// Found == false and nothing else.
func TestResolveNothingPinned(t *testing.T) {
	dir := t.TempDir()
	keys.SetActiveBroker(dir)
	t.Cleanup(func() { keys.SetActiveBroker("") })

	r := keys.ResolvePeerPublicKey("ghost")
	if r.Found {
		t.Fatalf("expected Found=false for unknown user, got %+v", r)
	}
}

// TestResolveTOFUFallback: a single broker-TOFU pin with no introducers
// resolves to TOFU.
func TestResolveTOFUFallback(t *testing.T) {
	dir := t.TempDir()
	keys.SetActiveBroker(dir)
	t.Cleanup(func() { keys.SetActiveBroker("") })

	_, bobPub := genKeyPair(t)
	if err := keys.SavePeerPublicKey("bob", keys.TrustedKey{
		PublicKey: bobPub,
		Method:    keys.TrustMethodTOFU,
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	r := keys.ResolvePeerPublicKey("bob")
	if !r.Found || r.Method != keys.TrustMethodTOFU || r.PublicKey != bobPub {
		t.Fatalf("expected TOFU resolution, got %+v", r)
	}
}

// TestResolveVerifiedWinsOverIntroductions: once a pin is verified, it
// must win regardless of conflicting introductions. This is the
// "verified is ground truth" invariant.
func TestResolveVerifiedWinsOverIntroductions(t *testing.T) {
	dir := t.TempDir()
	keys.SetActiveBroker(dir)
	t.Cleanup(func() { keys.SetActiveBroker("") })

	_, bobPub := genKeyPair(t)
	_, fakeBobPub := genKeyPair(t)
	_, alicePub := genKeyPair(t)
	_, carolPub := genKeyPair(t)

	// Alice and Carol need to be verified to count as legitimate
	// introducers.
	pinVerified(t, "alice", alicePub)
	pinVerified(t, "carol", carolPub)

	// Bob is mutual-verified as bobPub.
	pinVerified(t, "bob", bobPub)
	// Alice and Carol both introduce bob... as the fake key.
	if err := keys.AddIntroduction("bob", fakeBobPub, "alice"); err != nil {
		t.Fatalf("add intro alice: %v", err)
	}
	if err := keys.AddIntroduction("bob", fakeBobPub, "carol"); err != nil {
		t.Fatalf("add intro carol: %v", err)
	}

	r := keys.ResolvePeerPublicKey("bob")
	if r.Method != keys.TrustMethodVerified {
		t.Fatalf("verified pin should win, got method=%s", r.Method)
	}
	if r.PublicKey != bobPub {
		t.Fatal("resolver returned the fake pubkey instead of the verified one")
	}
	if !r.Contested {
		t.Fatal("expected Contested=true when introductions disagree with verified pin")
	}
}

// TestResolveIntroducedUpgradesTOFU: with no verified pin but a clear
// majority of introductions, resolution upgrades TOFU → Introduced and
// selects the majority pubkey.
func TestResolveIntroducedUpgradesTOFU(t *testing.T) {
	dir := t.TempDir()
	keys.SetActiveBroker(dir)
	t.Cleanup(func() { keys.SetActiveBroker("") })

	_, bobPub := genKeyPair(t)
	_, alicePub := genKeyPair(t)
	pinVerified(t, "alice", alicePub)

	// Broker-TOFU pin of bob first — should be replaceable.
	if err := keys.SavePeerPublicKey("bob", keys.TrustedKey{
		PublicKey: bobPub,
		Method:    keys.TrustMethodTOFU,
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	// Alice introduces bob with the same pubkey.
	if err := keys.AddIntroduction("bob", bobPub, "alice"); err != nil {
		t.Fatalf("add intro: %v", err)
	}

	r := keys.ResolvePeerPublicKey("bob")
	if r.Method != keys.TrustMethodIntroduced {
		t.Fatalf("expected Introduced, got %s", r.Method)
	}
	if r.PublicKey != bobPub {
		t.Fatal("wrong pubkey returned")
	}
	if len(r.Introducers) != 1 || r.Introducers[0] != "alice" {
		t.Fatalf("expected introducers=[alice], got %v", r.Introducers)
	}
}

// TestResolvePluralityPicksMajority: three introducers vouch for K1,
// one for K2 → K1 wins (no verified pin in play).
func TestResolvePluralityPicksMajority(t *testing.T) {
	dir := t.TempDir()
	keys.SetActiveBroker(dir)
	t.Cleanup(func() { keys.SetActiveBroker("") })

	_, realBobPub := genKeyPair(t)
	_, fakeBobPub := genKeyPair(t)
	_, alicePub := genKeyPair(t)
	_, carolPub := genKeyPair(t)
	_, davePub := genKeyPair(t)
	_, evePub := genKeyPair(t)

	for name, pub := range map[string]string{
		"alice": alicePub, "carol": carolPub, "dave": davePub, "eve": evePub,
	} {
		pinVerified(t, name, pub)
	}

	// 3 verifiers for the real key, 1 for the fake.
	if err := keys.AddIntroduction("bob", realBobPub, "alice"); err != nil {
		t.Fatal(err)
	}
	if err := keys.AddIntroduction("bob", realBobPub, "carol"); err != nil {
		t.Fatal(err)
	}
	if err := keys.AddIntroduction("bob", realBobPub, "dave"); err != nil {
		t.Fatal(err)
	}
	if err := keys.AddIntroduction("bob", fakeBobPub, "eve"); err != nil {
		t.Fatal(err)
	}

	r := keys.ResolvePeerPublicKey("bob")
	if r.Method != keys.TrustMethodIntroduced {
		t.Fatalf("expected Introduced, got %s", r.Method)
	}
	if r.PublicKey != realBobPub {
		t.Fatal("majority key not selected")
	}
	if len(r.Introducers) != 3 {
		t.Fatalf("expected 3 introducers on winner, got %d: %v", len(r.Introducers), r.Introducers)
	}
}

// TestResolveTieIsContested: an exact tie between two pubkeys produces
// a Contested result with no selected pubkey. Arbitrary tiebreaks on
// identity would be a footgun — surface the conflict to the UI.
func TestResolveTieIsContested(t *testing.T) {
	dir := t.TempDir()
	keys.SetActiveBroker(dir)
	t.Cleanup(func() { keys.SetActiveBroker("") })

	_, key1 := genKeyPair(t)
	_, key2 := genKeyPair(t)
	_, alicePub := genKeyPair(t)
	_, carolPub := genKeyPair(t)
	pinVerified(t, "alice", alicePub)
	pinVerified(t, "carol", carolPub)

	if err := keys.AddIntroduction("bob", key1, "alice"); err != nil {
		t.Fatal(err)
	}
	if err := keys.AddIntroduction("bob", key2, "carol"); err != nil {
		t.Fatal(err)
	}

	r := keys.ResolvePeerPublicKey("bob")
	if !r.Contested {
		t.Fatal("expected Contested=true for a tie")
	}
	if r.Found {
		t.Fatal("tied resolution must not return a selected pubkey")
	}
}

// TestResolveMultipleIntroductionsSamePubkey: two introducers for the
// same pubkey produce one TrustedKey record with both listed. The
// resolver surfaces both names on the winning record.
func TestResolveMultipleIntroductionsSamePubkey(t *testing.T) {
	dir := t.TempDir()
	keys.SetActiveBroker(dir)
	t.Cleanup(func() { keys.SetActiveBroker("") })

	_, bobPub := genKeyPair(t)
	_, alicePub := genKeyPair(t)
	_, carolPub := genKeyPair(t)
	pinVerified(t, "alice", alicePub)
	pinVerified(t, "carol", carolPub)

	if err := keys.AddIntroduction("bob", bobPub, "alice"); err != nil {
		t.Fatal(err)
	}
	if err := keys.AddIntroduction("bob", bobPub, "carol"); err != nil {
		t.Fatal(err)
	}

	r := keys.ResolvePeerPublicKey("bob")
	if r.Method != keys.TrustMethodIntroduced {
		t.Fatalf("expected Introduced, got %s", r.Method)
	}
	if len(r.Introducers) != 2 {
		t.Fatalf("expected 2 introducers, got %v", r.Introducers)
	}
}

// TestResolveDuplicateIntroducerDeduped: calling AddIntroduction twice
// with the same introducer must not double-count them in the stored
// list — idempotency guards against the broker returning duplicates.
func TestResolveDuplicateIntroducerDeduped(t *testing.T) {
	dir := t.TempDir()
	keys.SetActiveBroker(dir)
	t.Cleanup(func() { keys.SetActiveBroker("") })

	_, bobPub := genKeyPair(t)
	_, alicePub := genKeyPair(t)
	pinVerified(t, "alice", alicePub)

	if err := keys.AddIntroduction("bob", bobPub, "alice"); err != nil {
		t.Fatal(err)
	}
	if err := keys.AddIntroduction("bob", bobPub, "alice"); err != nil {
		t.Fatal(err)
	}

	r := keys.ResolvePeerPublicKey("bob")
	if len(r.Introducers) != 1 {
		t.Fatalf("expected deduped introducers=[alice], got %v", r.Introducers)
	}
}
