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

// verifyHash returns SHA-256(code|sessionID|pubKeyPEM) as the signed payload.
// Both sides must produce the same bytes for the signature to verify.
func verifyHash(code, sessionID, pubKeyPEM string) []byte {
	h := sha256.Sum256([]byte(code + "|" + sessionID + "|" + pubKeyPEM))
	return h[:]
}

// SignVerificationResponse is called by the peer (B) upon receiving a challenge.
// It signs H(code||sessionID||pk_B) with the local private key and returns the
// local public key PEM and a base64-encoded PSS signature.
func SignVerificationResponse(code, sessionID, username string) (pubKeyPEM, sigB64 string, err error) {
	privPath, pubPath, err := keyPaths(username)
	if err != nil {
		return "", "", err
	}
	privPEM, err := os.ReadFile(privPath)
	if err != nil {
		return "", "", fmt.Errorf("read private key: %w", err)
	}
	pubPEM, err := os.ReadFile(pubPath)
	if err != nil {
		return "", "", fmt.Errorf("read public key: %w", err)
	}

	block, _ := pem.Decode(privPEM)
	if block == nil {
		return "", "", fmt.Errorf("decode private key PEM: no block found")
	}
	privKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return "", "", fmt.Errorf("parse private key: %w", err)
	}
	rsaKey, ok := privKey.(*rsa.PrivateKey)
	if !ok {
		return "", "", fmt.Errorf("expected RSA private key")
	}

	hash := verifyHash(code, sessionID, string(pubPEM))
	sig, err := rsa.SignPSS(rand.Reader, rsaKey, crypto.SHA256, hash, nil)
	if err != nil {
		return "", "", fmt.Errorf("sign: %w", err)
	}
	return string(pubPEM), base64.StdEncoding.EncodeToString(sig), nil
}

// VerifyPeerSignature checks that sig = Sign(sk_B, H(code||sessionID||publicKeyPEM)).
// Returns nil if the signature is valid — the key can then be pinned.
func VerifyPeerSignature(code, sessionID, publicKeyPEM, sigB64 string) error {
	block, _ := pem.Decode([]byte(publicKeyPEM))
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
	hash := verifyHash(code, sessionID, publicKeyPEM)
	if err := rsa.VerifyPSS(rsaPub, crypto.SHA256, hash, sig, nil); err != nil {
		return fmt.Errorf("signature verification failed: %w", err)
	}
	return nil
}
