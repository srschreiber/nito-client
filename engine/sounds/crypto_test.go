// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package sounds

import (
	"bytes"
	"crypto/rand"
	"testing"
)

// ── Voice frame encryption (AES-256-GCM) ─────────────────────────────────────

func makeAEAD(t *testing.T) interface {
	Seal(dst, nonce, plaintext, additionalData []byte) []byte
	Open(dst, nonce, ciphertext, additionalData []byte) ([]byte, error)
	NonceSize() int
	Overhead() int
} {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	aead, err := newAEAD(key)
	if err != nil {
		t.Fatalf("newAEAD: %v", err)
	}
	return aead
}

// TestFrameRoundTrip verifies that a frame encrypted by encryptFrame can be
// recovered exactly by decryptFrame.
func TestFrameRoundTrip(t *testing.T) {
	aead := makeAEAD(t)
	want := []byte("opus-frame-payload")

	ct, err := encryptFrame(aead, want)
	if err != nil {
		t.Fatalf("encryptFrame: %v", err)
	}
	if bytes.Equal(ct, want) {
		t.Fatal("ciphertext is identical to plaintext — encryption did nothing")
	}

	got, err := decryptFrame(aead, ct)
	if err != nil {
		t.Fatalf("decryptFrame: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("plaintext mismatch: got %q, want %q", got, want)
	}
}

// TestFrameEncryptNonDeterministic confirms that two calls with the same
// plaintext produce different ciphertexts (random nonce per frame).
func TestFrameEncryptNonDeterministic(t *testing.T) {
	aead := makeAEAD(t)
	msg := []byte("same audio frame")

	ct1, err := encryptFrame(aead, msg)
	if err != nil {
		t.Fatal(err)
	}
	ct2, err := encryptFrame(aead, msg)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(ct1, ct2) {
		t.Error("two encryptions of the same frame produced identical ciphertexts — nonce not randomised")
	}
}

// TestFrameTamperedPayloadRejected confirms that flipping a bit in the
// ciphertext body causes decryption to fail (AEAD tag covers entire payload).
func TestFrameTamperedPayloadRejected(t *testing.T) {
	aead := makeAEAD(t)
	ct, err := encryptFrame(aead, []byte("audio-data"))
	if err != nil {
		t.Fatal(err)
	}

	tampered := make([]byte, len(ct))
	copy(tampered, ct)
	tampered[len(tampered)-1] ^= 0xFF

	if _, err := decryptFrame(aead, tampered); err == nil {
		t.Error("tampered payload was accepted — AEAD authentication not enforced")
	}
}

// TestFrameTamperedNonceRejected confirms that altering the nonce prefix also
// breaks decryption (the tag was computed over the original nonce).
func TestFrameTamperedNonceRejected(t *testing.T) {
	aead := makeAEAD(t)
	ct, err := encryptFrame(aead, []byte("audio-data"))
	if err != nil {
		t.Fatal(err)
	}

	tampered := make([]byte, len(ct))
	copy(tampered, ct)
	tampered[0] ^= 0xFF // flip a nonce byte

	if _, err := decryptFrame(aead, tampered); err == nil {
		t.Error("tampered nonce was accepted — decryption should fail with wrong nonce")
	}
}

// TestFrameTooShortRejected confirms that a payload shorter than the nonce size
// is rejected cleanly rather than panicking.
func TestFrameTooShortRejected(t *testing.T) {
	aead := makeAEAD(t)
	if _, err := decryptFrame(aead, []byte{0x00}); err == nil {
		t.Error("payload shorter than nonce size should be rejected")
	}
}

// TestFrameWrongKeyRejected confirms that a frame encrypted with one key cannot
// be decrypted with a different key.
func TestFrameWrongKeyRejected(t *testing.T) {
	aead1 := makeAEAD(t)
	aead2 := makeAEAD(t)

	ct, err := encryptFrame(aead1, []byte("secret audio"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decryptFrame(aead2, ct); err == nil {
		t.Error("frame decrypted with wrong key — key isolation broken")
	}
}

// ── Voice key derivation (HKDF) ───────────────────────────────────────────────

// TestDeriveVoiceKeyDeterministic confirms that the same room key always yields
// the same derived voice key.
func TestDeriveVoiceKeyDeterministic(t *testing.T) {
	roomKey := make([]byte, 32)
	if _, err := rand.Read(roomKey); err != nil {
		t.Fatal(err)
	}
	k1, err := deriveVoiceKey(roomKey)
	if err != nil {
		t.Fatalf("deriveVoiceKey: %v", err)
	}
	k2, err := deriveVoiceKey(roomKey)
	if err != nil {
		t.Fatalf("deriveVoiceKey: %v", err)
	}
	if !bytes.Equal(k1, k2) {
		t.Error("same room key produced different voice keys")
	}
}

// TestDeriveVoiceKeyDistinct confirms that different room keys produce different
// voice keys (no key collisions from the HKDF expansion).
func TestDeriveVoiceKeyDistinct(t *testing.T) {
	rk1 := make([]byte, 32)
	rk2 := make([]byte, 32)
	if _, err := rand.Read(rk1); err != nil {
		t.Fatal(err)
	}
	if _, err := rand.Read(rk2); err != nil {
		t.Fatal(err)
	}

	vk1, _ := deriveVoiceKey(rk1)
	vk2, _ := deriveVoiceKey(rk2)
	if bytes.Equal(vk1, vk2) {
		t.Error("different room keys produced identical voice keys")
	}
}

// TestDeriveVoiceKeyDifferentFromRoomKey confirms the derived key is not simply
// the room key passed through (HKDF actually transforms it).
func TestDeriveVoiceKeyDifferentFromRoomKey(t *testing.T) {
	roomKey := make([]byte, 32)
	if _, err := rand.Read(roomKey); err != nil {
		t.Fatal(err)
	}
	voiceKey, err := deriveVoiceKey(roomKey)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(roomKey, voiceKey) {
		t.Error("voice key is identical to room key — HKDF derivation is a no-op")
	}
}

// TestDeriveVoiceKeyFullPipeline is an end-to-end test: derive a voice key from
// a room key, build an AEAD from it, encrypt a frame, decrypt it, assert match.
func TestDeriveVoiceKeyFullPipeline(t *testing.T) {
	roomKey := make([]byte, 32)
	if _, err := rand.Read(roomKey); err != nil {
		t.Fatal(err)
	}

	voiceKey, err := deriveVoiceKey(roomKey)
	if err != nil {
		t.Fatalf("deriveVoiceKey: %v", err)
	}
	aead, err := newAEAD(voiceKey)
	if err != nil {
		t.Fatalf("newAEAD: %v", err)
	}

	want := []byte("end-to-end opus frame")
	ct, err := encryptFrame(aead, want)
	if err != nil {
		t.Fatalf("encryptFrame: %v", err)
	}
	got, err := decryptFrame(aead, ct)
	if err != nil {
		t.Fatalf("decryptFrame: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("pipeline round-trip failed: got %q, want %q", got, want)
	}
}
