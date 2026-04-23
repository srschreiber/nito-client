// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package botcli

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"

	"github.com/srschreiber/nito-client/engine/keys"
)

// genPubPEM mirrors engine/keys/crypto_test.go:genKeyPair — inlined here
// because that helper is private to the keys_test package. Returns a fresh
// RSA-2048 public key encoded as PKIX PEM, ready for TrustedKey.PublicKey.
func genPubPEM(t *testing.T) string {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatalf("marshal pub: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

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

// TestRateLimiterPerSender verifies the per-sender sliding window: two
// different senders are both allowed at t=0, the same sender is denied
// inside the window, and is allowed again once the window has elapsed. A
// swappable clock keeps the test deterministic and fast.
func TestRateLimiterPerSender(t *testing.T) {
	var clock time.Time
	lim := NewLimiter(5 * time.Second)
	lim.now = func() time.Time { return clock }

	if !lim.Allow("alice") {
		t.Fatal("alice's first request should be allowed")
	}
	if !lim.Allow("bob") {
		t.Fatal("bob's first request should be allowed (different sender)")
	}
	// Same sender, 3 s later — still inside 5 s window.
	clock = clock.Add(3 * time.Second)
	if lim.Allow("alice") {
		t.Fatal("alice should be denied 3 s into the window")
	}
	// 6 s later — outside window.
	clock = clock.Add(3 * time.Second)
	if !lim.Allow("alice") {
		t.Fatal("alice should be allowed 6 s after first request")
	}
	// Sanity: attacker can't starve a legit future request by hammering.
	for i := 0; i < 50; i++ {
		lim.Allow("mallory")
	}
	clock = clock.Add(6 * time.Second)
	if !lim.Allow("mallory") {
		t.Fatal("mallory should be allowed once window elapses, regardless of prior spam")
	}
}

// TestAllowedSenderTrustGate covers every branch of senderAllowed:
// owner-is-self, introduced-by-owner, TOFU-only (denied), and contested
// (denied). Each case sets up a fresh on-disk pubkey cache via
// SetActiveBroker(t.TempDir()).
func TestAllowedSenderTrustGate(t *testing.T) {
	keys.SetActiveBroker(t.TempDir())
	t.Cleanup(func() { keys.SetActiveBroker("") })

	// Owner alice is always allowed — she's the bot's verified root.
	aliceP := genPubPEM(t)
	pinVerified(t, "alice", aliceP)
	if ok, why := senderAllowed("alice", "alice"); !ok {
		t.Fatalf("owner should be allowed, got denial: %s", why)
	}

	// Bob is pinned as TOFU: broker-asserted, not corroborated. Denied.
	bobP := genPubPEM(t)
	if err := keys.SavePeerPublicKey("bob", keys.TrustedKey{
		PublicKey: bobP,
		Method:    keys.TrustMethodTOFU,
	}); err != nil {
		t.Fatalf("tofu bob: %v", err)
	}
	if ok, _ := senderAllowed("bob", "alice"); ok {
		t.Fatal("TOFU-only bob must be denied")
	}

	// Alice introduces Bob → Bob's pin upgrades to Introduced and is
	// allowed. AddIntroduction requires alice to already have a verified
	// pin locally, which pinVerified provided above.
	if err := keys.AddIntroduction("bob", bobP, "alice"); err != nil {
		t.Fatalf("add intro: %v", err)
	}
	if ok, why := senderAllowed("bob", "alice"); !ok {
		t.Fatalf("owner-introduced bob should be allowed, denial: %s", why)
	}

	// Carol is contested: two verified introducers (alice + dave) vouch
	// for different pubkeys. Resolver surfaces Contested → refused.
	daveP := genPubPEM(t)
	pinVerified(t, "dave", daveP)
	carolKey1 := genPubPEM(t)
	carolKey2 := genPubPEM(t)
	if err := keys.AddIntroduction("carol", carolKey1, "alice"); err != nil {
		t.Fatalf("add intro carol/alice: %v", err)
	}
	if err := keys.AddIntroduction("carol", carolKey2, "dave"); err != nil {
		t.Fatalf("add intro carol/dave: %v", err)
	}
	if ok, why := senderAllowed("carol", "alice"); ok {
		t.Fatalf("contested carol must be refused; got allowed (%s)", why)
	}

	// Unknown sender (never seen) → no pin → refused.
	if ok, _ := senderAllowed("eve", "alice"); ok {
		t.Fatal("unknown sender must be refused")
	}

	// Self-verified peer whose pin has no owner introducer: refuse.
	// This mirrors the "bot only verifies the owner" invariant —
	// if a Verified pin exists for a non-owner, it's either a
	// misconfigured bot or an attacker-induced state; don't serve them.
	franklinP := genPubPEM(t)
	pinVerified(t, "franklin", franklinP)
	if ok, why := senderAllowed("franklin", "alice"); ok {
		t.Fatalf("self-verified non-owner must be refused; got allowed (%s)", why)
	}
}

// TestHelloRoundTrip pins dispatch behaviour: `!hello` returns the
// @-addressed greeting, unknown commands are dropped, and non-`!` chat is
// ignored entirely by the serve path (handleOne skips the dispatch).
func TestHelloRoundTrip(t *testing.T) {
	if got := dispatch("!hello", "alice"); got != "hello, @alice" {
		t.Fatalf("!hello dispatch: got %q", got)
	}
	if got := dispatch("!hello extra args ignored", "bob"); got != "hello, @bob" {
		t.Fatalf("extra args should not break dispatch: %q", got)
	}
	if got := dispatch("!unknown", "alice"); got != "" {
		t.Fatalf("unknown commands must yield no reply; got %q", got)
	}
}

// TestFirstToken checks the minimal parser: whitespace trim, first token,
// handles the no-space + single-word cases.
func TestFirstToken(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"!hello", "!hello"},
		{"  !hello  ", "!hello"},
		{"!hello world", "!hello"},
		{"!hello\tname", "!hello"},
		{"!hello\npayload", "!hello"},
	}
	for _, c := range cases {
		if got := firstToken(c.in); got != c.want {
			t.Errorf("firstToken(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
