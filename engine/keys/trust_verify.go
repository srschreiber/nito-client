// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package keys

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"sync"
	"time"
)

const verifyCodeMax = 1_000_000

// GenerateVerificationChallenge returns a zero-padded 6-digit code and a random
// session ID. The code is displayed to the local user and shared out-of-band
// with the peer; the session ID is sent through the broker.
func GenerateVerificationChallenge() (code, sessionID string, err error) {
	n, err := rand.Int(rand.Reader, big.NewInt(verifyCodeMax))
	if err != nil {
		return "", "", fmt.Errorf("generate code: %w", err)
	}
	sidBytes := make([]byte, 16)
	if _, err := rand.Read(sidBytes); err != nil {
		return "", "", fmt.Errorf("generate session id: %w", err)
	}
	return fmt.Sprintf("%06d", n.Int64()), base64.RawURLEncoding.EncodeToString(sidBytes), nil
}

// verifyHash returns SHA-256(code | sessionID | initiatorPubPEM | responderPubPEM | role).
// role is "A" (initiator) or "B" (responder). The role tag binds a signature
// to one side of the handshake so B's response cannot be reflected back as A's
// confirm, and vice versa.
func verifyHash(code, sessionID, initiatorPubPEM, responderPubPEM, role string) []byte {
	h := sha256.Sum256([]byte(code + "|" + sessionID + "|" + initiatorPubPEM + "|" + responderPubPEM + "|" + role))
	return h[:]
}

// LoadPublicKeyPEM reads the PEM-encoded public key from the local user's key
// directory. Does not generate a key if one is missing.
func LoadPublicKeyPEM(username string) (string, error) {
	_, pubPath, err := keyPaths(username)
	if err != nil {
		return "", err
	}
	pubPEM, err := os.ReadFile(pubPath)
	if err != nil {
		return "", fmt.Errorf("read public key: %w", err)
	}
	return string(pubPEM), nil
}

// loadPrivateRSAKey reads and parses the local private key.
func loadPrivateRSAKey(username string) (*rsa.PrivateKey, error) {
	privPath, _, err := keyPaths(username)
	if err != nil {
		return nil, err
	}
	privPEM, err := os.ReadFile(privPath)
	if err != nil {
		return nil, fmt.Errorf("read private key: %w", err)
	}
	block, _ := pem.Decode(privPEM)
	if block == nil {
		return nil, fmt.Errorf("decode private key PEM: no block found")
	}
	privKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	rsaKey, ok := privKey.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("expected RSA private key")
	}
	return rsaKey, nil
}

// signWithRole signs verifyHash(...role) with the user's private key.
func signWithRole(code, sessionID, initiatorPubPEM, responderPubPEM, role, username string) (string, error) {
	rsaKey, err := loadPrivateRSAKey(username)
	if err != nil {
		return "", err
	}
	sig, err := rsa.SignPSS(rand.Reader, rsaKey, crypto.SHA256, verifyHash(code, sessionID, initiatorPubPEM, responderPubPEM, role), nil)
	if err != nil {
		return "", fmt.Errorf("sign: %w", err)
	}
	return base64.StdEncoding.EncodeToString(sig), nil
}

// verifyWithRole verifies sigB64 against verifyHash(...role) using signerPubPEM.
func verifyWithRole(code, sessionID, initiatorPubPEM, responderPubPEM, role, signerPubPEM, sigB64 string) error {
	block, _ := pem.Decode([]byte(signerPubPEM))
	if block == nil {
		return fmt.Errorf("decode public key PEM: no block found")
	}
	pubIface, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return fmt.Errorf("parse public key: %w", err)
	}
	rsaPub, ok := pubIface.(*rsa.PublicKey)
	if !ok {
		return fmt.Errorf("expected RSA public key")
	}
	sig, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}
	if err := rsa.VerifyPSS(rsaPub, crypto.SHA256, verifyHash(code, sessionID, initiatorPubPEM, responderPubPEM, role), sig, nil); err != nil {
		return fmt.Errorf("signature verification failed: %w", err)
	}
	return nil
}

// SignVerificationResponse is B's step 2 of the mutual handshake. B signs
// H(code | sessionID | pk_A | pk_B | "B") with B's private key and returns
// pk_B plus the base64-encoded PSS signature.
func SignVerificationResponse(code, sessionID, initiatorPubPEM, username string) (responderPubPEM, sigB64 string, err error) {
	responderPubPEM, err = LoadPublicKeyPEM(username)
	if err != nil {
		return "", "", err
	}
	sigB64, err = signWithRole(code, sessionID, initiatorPubPEM, responderPubPEM, "B", username)
	if err != nil {
		return "", "", err
	}
	return responderPubPEM, sigB64, nil
}

// SignVerificationConfirm is A's step 3 of the mutual handshake. A signs
// H(code | sessionID | pk_A | pk_B | "A") with A's private key after verifying
// B's response. Returns the base64-encoded PSS signature.
func SignVerificationConfirm(code, sessionID, initiatorPubPEM, responderPubPEM, username string) (string, error) {
	return signWithRole(code, sessionID, initiatorPubPEM, responderPubPEM, "A", username)
}

// VerifyResponseSignature is A's check of B's response. Returns nil if sig is
// a valid B-signature over (code, sessionID, pk_A, pk_B, "B") under pk_B.
func VerifyResponseSignature(code, sessionID, initiatorPubPEM, responderPubPEM, sigB64 string) error {
	return verifyWithRole(code, sessionID, initiatorPubPEM, responderPubPEM, "B", responderPubPEM, sigB64)
}

// VerifyConfirmSignature is B's check of A's confirm. Returns nil if sig is
// a valid A-signature over (code, sessionID, pk_A, pk_B, "A") under pk_A.
func VerifyConfirmSignature(code, sessionID, initiatorPubPEM, responderPubPEM, sigB64 string) error {
	return verifyWithRole(code, sessionID, initiatorPubPEM, responderPubPEM, "A", initiatorPubPEM, sigB64)
}

// ── Pending-confirm context cache (B's side) ──────────────────────────────────
//
// Between sending the Response and receiving the Confirm, B must remember the
// code (typed by user) plus pk_A and pk_B so the incoming confirm signature can
// be verified. Held in memory only; never persisted. TTL-swept.

type pendingConfirm struct {
	fromUsername    string
	code            string
	initiatorPubPEM string
	responderPubPEM string
	expiresAt       time.Time
}

var (
	pendingConfirmsMu sync.Mutex
	pendingConfirms   = map[string]pendingConfirm{}
	pendingSweepOnce  sync.Once
)

func startPendingConfirmSweeper() {
	pendingSweepOnce.Do(func() {
		go func() {
			t := time.NewTicker(30 * time.Second)
			defer t.Stop()
			for range t.C {
				now := time.Now()
				pendingConfirmsMu.Lock()
				for k, v := range pendingConfirms {
					if now.After(v.expiresAt) {
						delete(pendingConfirms, k)
					}
				}
				pendingConfirmsMu.Unlock()
			}
		}()
	})
}

// RememberConfirmContext stores the crypto context B needs to later verify a
// follow-up KeyVerifyConfirm from A. ttl controls how long the entry survives.
func RememberConfirmContext(sessionID, fromUsername, code, initiatorPubPEM, responderPubPEM string, ttl time.Duration) {
	startPendingConfirmSweeper()
	pendingConfirmsMu.Lock()
	pendingConfirms[sessionID] = pendingConfirm{
		fromUsername:    fromUsername,
		code:            code,
		initiatorPubPEM: initiatorPubPEM,
		responderPubPEM: responderPubPEM,
		expiresAt:       time.Now().Add(ttl),
	}
	pendingConfirmsMu.Unlock()
}

// ConsumeConfirmContext returns and removes the pending-confirm context for
// sessionID. Returns ok=false if none was stored or it has expired.
func ConsumeConfirmContext(sessionID string) (fromUsername, code, initiatorPubPEM, responderPubPEM string, ok bool) {
	pendingConfirmsMu.Lock()
	defer pendingConfirmsMu.Unlock()
	pc, exists := pendingConfirms[sessionID]
	if !exists {
		return "", "", "", "", false
	}
	delete(pendingConfirms, sessionID)
	if time.Now().After(pc.expiresAt) {
		return "", "", "", "", false
	}
	return pc.fromUsername, pc.code, pc.initiatorPubPEM, pc.responderPubPEM, true
}
