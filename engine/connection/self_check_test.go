// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package connection

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/srschreiber/nito-client/engine/keys"
)

// brokerServing stands up a tiny httptest server that serves the given
// pubkey PEM for GET /api/v0/users/public-key?username=*. Returns the
// brokerURL string in the "host:port" form AssertBrokerServesOurPublicKey
// expects.
func brokerServing(t *testing.T, pubKeyPEM string) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v0/users/public-key", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"publicKey": pubKeyPEM})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "http://")
}

// TestAssertBrokerServesOurPublicKeyHappyPath: broker returns the same
// pubkey we hold locally → no error.
func TestAssertBrokerServesOurPublicKeyHappyPath(t *testing.T) {
	dir := t.TempDir()
	keys.SetActiveBroker(dir)
	t.Cleanup(func() { keys.SetActiveBroker("") })

	pubPEM, err := keys.LoadOrGenerate("alice")
	if err != nil {
		t.Fatalf("LoadOrGenerate: %v", err)
	}
	brokerURL := brokerServing(t, pubPEM)

	if err := AssertBrokerServesOurPublicKey(brokerURL, "alice"); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}

// TestAssertBrokerServesOurPublicKeyRejectsForgery: broker returns a
// different pubkey than we hold locally → error. This is the
// broker-compromise detection property.
func TestAssertBrokerServesOurPublicKeyRejectsForgery(t *testing.T) {
	dir := t.TempDir()
	keys.SetActiveBroker(dir)
	t.Cleanup(func() { keys.SetActiveBroker("") })

	// Alice's real key.
	if _, err := keys.LoadOrGenerate("alice"); err != nil {
		t.Fatalf("LoadOrGenerate alice: %v", err)
	}
	// Separate keypair to play the "forged alice" — reuse the generator by
	// creating keys for a different username then reading the PEM.
	forgedPEM, err := keys.LoadOrGenerate("evil")
	if err != nil {
		t.Fatalf("LoadOrGenerate evil: %v", err)
	}

	brokerURL := brokerServing(t, forgedPEM)

	err = AssertBrokerServesOurPublicKey(brokerURL, "alice")
	if err == nil {
		t.Fatal("expected error — broker served a different pubkey for alice and we accepted it")
	}
	if !strings.Contains(err.Error(), "different public key") {
		t.Fatalf("expected 'different public key' in error, got: %v", err)
	}
}

// TestAssertBrokerServesOurPublicKeyIgnoresPEMWhitespace: extra trailing
// newlines or line-wrap variants from the broker must still match —
// comparison is by derived device id, not byte-for-byte PEM.
func TestAssertBrokerServesOurPublicKeyIgnoresPEMWhitespace(t *testing.T) {
	dir := t.TempDir()
	keys.SetActiveBroker(dir)
	t.Cleanup(func() { keys.SetActiveBroker("") })

	pubPEM, err := keys.LoadOrGenerate("alice")
	if err != nil {
		t.Fatalf("LoadOrGenerate: %v", err)
	}
	// Broker returns the same key with some extra whitespace.
	brokerURL := brokerServing(t, pubPEM+"\n\n")

	if err := AssertBrokerServesOurPublicKey(brokerURL, "alice"); err != nil {
		t.Fatalf("whitespace difference should not fail the check: %v", err)
	}
}
