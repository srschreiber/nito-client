// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package keys_test

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"testing"

	"github.com/srschreiber/nito-client/shellapp/keys"
)

// genKeyPair creates an in-memory RSA-2048 keypair.
// Returns the private key and the PKIX public key as a PEM string (the format
// the broker stores and EncryptDataWithPEM accepts).
func genKeyPair(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	pubBytes, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey: %v", err)
	}
	pubPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubBytes}))
	return priv, pubPEM
}

// ── E2E message encryption ────────────────────────────────────────────────────

// TestRoomMessageRoundTrip verifies that a message encrypted with the room key
// can be decrypted by a receiver that holds the same key.
func TestRoomMessageRoundTrip(t *testing.T) {
	roomKey, err := keys.GenerateRoomKey()
	if err != nil {
		t.Fatalf("GenerateRoomKey: %v", err)
	}

	sender := keys.NewRoomKeyChain(roomKey)
	receiver := keys.NewRoomKeyChain(roomKey)

	want := []byte("hello, E2EE world")
	count := 0
	ct, err := sender.EncryptMessageWithRoomKey(want, "alice", &count)
	if err != nil {
		t.Fatalf("EncryptMessageWithRoomKey: %v", err)
	}
	if string(ct) == string(want) {
		t.Fatal("ciphertext is identical to plaintext — encryption did nothing")
	}

	got, err := receiver.DecryptMessageWithRoomKey(ct, "alice-roundtrip", &count)
	if err != nil {
		t.Fatalf("DecryptMessageWithRoomKey: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("plaintext mismatch: got %q, want %q", got, want)
	}
}

// TestDecryptWithWrongKeyFails confirms that a ciphertext cannot be decrypted
// with a different room key.
func TestDecryptWithWrongKeyFails(t *testing.T) {
	correctKey, _ := keys.GenerateRoomKey()
	wrongKey, _ := keys.GenerateRoomKey()

	sender := keys.NewRoomKeyChain(correctKey)
	receiver := keys.NewRoomKeyChain(wrongKey)

	count := 0
	ct, err := sender.EncryptMessageWithRoomKey([]byte("secret"), "alice", &count)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if _, err := receiver.DecryptMessageWithRoomKey(ct, "alice-wrongkey", &count); err == nil {
		t.Fatal("expected decryption with wrong key to fail, got nil error")
	}
}

// ── Nonce replay ─────────────────────────────────────────────────────────────

// TestNonceReplayRejected confirms that replaying the same ciphertext (same
// nonce bytes) is rejected on the second attempt.
func TestNonceReplayRejected(t *testing.T) {
	roomKey, _ := keys.GenerateRoomKey()
	sender := keys.NewRoomKeyChain(roomKey)

	// Use a unique user ID so this test's nonce entries don't collide with others.
	userID := "nonce-replay-test-user"
	delete(keys.NonceMap, userID)
	t.Cleanup(func() { delete(keys.NonceMap, userID) })

	count := 0
	ct, err := sender.EncryptMessageWithRoomKey([]byte("replay me"), "alice", &count)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	receiver := keys.NewRoomKeyChain(roomKey)

	if _, err := receiver.DecryptMessageWithRoomKey(ct, userID, &count); err != nil {
		t.Fatalf("first decrypt failed unexpectedly: %v", err)
	}
	if _, err := receiver.DecryptMessageWithRoomKey(ct, userID, &count); err == nil {
		t.Fatal("second decrypt of same ciphertext (nonce replay) must be rejected, got nil error")
	}
}

// ── Login challenge signing ───────────────────────────────────────────────────

// TestLoginChallengeSignVerify confirms that signing "username:challenge" with
// the RSA private key produces a signature that verifies against the stored
// public key (replicating the broker's verification step).
func TestLoginChallengeSignVerify(t *testing.T) {
	priv, _ := genKeyPair(t)
	msg := "alice:abc123challenge"

	h := sha256.Sum256([]byte(msg))
	sig, err := rsa.SignPSS(rand.Reader, priv, crypto.SHA256, h[:], nil)
	if err != nil {
		t.Fatalf("SignPSS: %v", err)
	}
	if err := rsa.VerifyPSS(&priv.PublicKey, crypto.SHA256, h[:], sig, nil); err != nil {
		t.Errorf("valid signature rejected: %v", err)
	}
}

// TestLoginChallengeTamperedFails confirms that altering a single byte in the
// signed message makes the signature invalid.
func TestLoginChallengeTamperedFails(t *testing.T) {
	priv, _ := genKeyPair(t)
	original := "alice:abc123challenge"
	tampered := "alice:abc123CHALLENGE" // capitalised = different bytes

	h := sha256.Sum256([]byte(original))
	sig, err := rsa.SignPSS(rand.Reader, priv, crypto.SHA256, h[:], nil)
	if err != nil {
		t.Fatalf("SignPSS: %v", err)
	}

	hTampered := sha256.Sum256([]byte(tampered))
	if err := rsa.VerifyPSS(&priv.PublicKey, crypto.SHA256, hTampered[:], sig, nil); err == nil {
		t.Fatal("tampered challenge should not verify, got nil error")
	}
}

// ── Room key encryption (invite flow) ────────────────────────────────────────

// TestRoomKeyEncryptedForInvitee verifies the invite flow:
//   - room key encrypted with the invitee's public key can be decrypted by the
//     invitee's private key, and
//   - the sender's private key cannot decrypt it.
func TestRoomKeyEncryptedForInvitee(t *testing.T) {
	senderPriv, _ := genKeyPair(t)
	inviteePriv, inviteePubPEM := genKeyPair(t)

	roomKey, err := keys.GenerateRoomKey()
	if err != nil {
		t.Fatalf("GenerateRoomKey: %v", err)
	}

	encB64, err := keys.EncryptDataWithPEM(roomKey, inviteePubPEM)
	if err != nil {
		t.Fatalf("EncryptDataWithPEM: %v", err)
	}

	ct, err := base64.StdEncoding.DecodeString(encB64)
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}

	// Invitee can decrypt.
	got, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, inviteePriv, ct, nil)
	if err != nil {
		t.Fatalf("invitee decrypt failed: %v", err)
	}
	if string(got) != string(roomKey) {
		t.Errorf("decrypted room key mismatch: got %x, want %x", got, roomKey)
	}

	// Sender cannot decrypt (wrong private key).
	if _, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, senderPriv, ct, nil); err == nil {
		t.Fatal("sender should not be able to decrypt room key encrypted for invitee")
	}
}

// ── Public key / private key consistency ─────────────────────────────────────

// TestKeyPairConsistency confirms that the public key derived from the private
// key can verify signatures produced by that private key — i.e. the stored pair
// is internally consistent.
func TestKeyPairConsistency(t *testing.T) {
	priv, pubPEM := genKeyPair(t)

	// Parse the stored public key (the form the broker would hold).
	block, _ := pem.Decode([]byte(pubPEM))
	if block == nil {
		t.Fatal("PEM decode returned nil block")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		t.Fatalf("ParsePKIXPublicKey: %v", err)
	}
	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		t.Fatal("expected *rsa.PublicKey")
	}

	msg := "consistency-check"
	h := sha256.Sum256([]byte(msg))
	sig, err := rsa.SignPSS(rand.Reader, priv, crypto.SHA256, h[:], nil)
	if err != nil {
		t.Fatalf("SignPSS: %v", err)
	}
	if err := rsa.VerifyPSS(rsaPub, crypto.SHA256, h[:], sig, nil); err != nil {
		t.Errorf("stored public key does not match private key: %v", err)
	}
}
