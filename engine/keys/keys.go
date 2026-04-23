// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package keys

import (
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/crypto/chacha20poly1305"

	"github.com/srschreiber/nito-client/shared/utils"
)

var NonceMap = map[string]map[string]struct{}{}

const keyDir = ".nito"

var (
	activeBrokerMu sync.RWMutex
	activeBroker   string
)

// SetActiveBroker records the broker URL so key files are stored under
// ~/.nito/<broker>/users/<username>/keys/. Call this before any key operation.
func SetActiveBroker(brokerURL string) {
	activeBrokerMu.Lock()
	defer activeBrokerMu.Unlock()
	activeBroker = sanitizeBroker(brokerURL)
}

// sanitizeBroker extracts the host (and port if present) from a broker URL and
// replaces characters that are not safe in directory names with underscores.
func sanitizeBroker(brokerURL string) string {
	if u, err := url.Parse(brokerURL); err == nil && u.Host != "" {
		brokerURL = u.Host
	}
	var b strings.Builder
	for _, r := range brokerURL {
		if r == ':' || r == '/' || r == '\\' {
			b.WriteRune('_')
		} else {
			b.WriteRune(r)
		}
	}
	s := b.String()
	if s == "" {
		return "default"
	}
	return s
}

func keyPaths(username string) (privPath, pubPath string, err error) {
	activeBrokerMu.RLock()
	broker := activeBroker
	activeBrokerMu.RUnlock()
	if broker == "" {
		broker = "default"
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", fmt.Errorf("get home dir: %w", err)
	}
	dir := filepath.Join(home, keyDir, broker, "users", username, "keys")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", "", fmt.Errorf("create Key dir: %w", err)
	}
	return filepath.Join(dir, "private_key.pem"), filepath.Join(dir, "public_key.pem"), nil
}

// HaveKeys returns true if a local Key pair exists for the given username (i.e. the user has registered).
func HaveKeys(username string) bool {
	privPath, _, err := keyPaths(username)
	if err != nil {
		return false
	}
	_, err = os.Stat(privPath)
	return err == nil
}

// LoadOrGenerate loads the RSA-2048 Key pair from .nito/users/<username>/keys/ in the working directory,
// generating and saving them if they don't exist yet.
// Returns the public Key as a PEM string ready to send to the broker.
func LoadOrGenerate(username string) (pub string, err error) {
	privPath, pubPath, err := keyPaths(username)
	if err != nil {
		return "", err
	}

	if _, err := os.Stat(privPath); err == nil {
		// Keys already exist — load them.
		pubPEM, err := os.ReadFile(pubPath)
		if err != nil {
			return "", fmt.Errorf("read public Key: %w", err)
		}
		return string(pubPEM), nil
	}

	// Generate new Key pair.
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", fmt.Errorf("generate Key: %w", err)
	}

	privBytes, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", err
	}
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privBytes})
	if err := os.WriteFile(privPath, privPEM, 0600); err != nil {
		return "", fmt.Errorf("save private Key: %w", err)
	}

	pubBytes, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return "", err
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubBytes})
	if err := os.WriteFile(pubPath, pubPEM, 0644); err != nil {
		return "", fmt.Errorf("save public Key: %w", err)
	}

	fmt.Printf("keys saved to %s\n", filepath.Join(privPath, "..", ".."))
	return string(pubPEM), nil
}

// Sign signs message with the local RSA private Key using PSS-SHA256 and returns
// the base64-encoded signature. Used to authenticate API and RPC requests.
func Sign(message, username string) (string, error) {
	privPath, _, err := keyPaths(username)
	if err != nil {
		return "", err
	}
	privPEM, err := os.ReadFile(privPath)
	if err != nil {
		return "", fmt.Errorf("read private Key: %w", err)
	}
	block, _ := pem.Decode(privPEM)
	if block == nil {
		return "", fmt.Errorf("decode private Key PEM: no block found")
	}
	privKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("parse private Key: %w", err)
	}
	rsaKey, ok := privKey.(*rsa.PrivateKey)
	if !ok {
		return "", fmt.Errorf("expected RSA private Key")
	}
	h := sha256.Sum256([]byte(message))
	sig, err := rsa.SignPSS(rand.Reader, rsaKey, crypto.SHA256, h[:], nil)
	if err != nil {
		return "", fmt.Errorf("sign: %w", err)
	}
	return base64.StdEncoding.EncodeToString(sig), nil
}

// DecryptRoomKey decrypts a base64-encoded RSA-OAEP ciphertext using the local private Key.
func DecryptRoomKey(encryptedKeyB64, username string) ([]byte, error) {
	privPath, _, err := keyPaths(username)
	if err != nil {
		return nil, err
	}
	privPEM, err := os.ReadFile(privPath)
	if err != nil {
		return nil, fmt.Errorf("read private Key: %w", err)
	}
	block, _ := pem.Decode(privPEM)
	if block == nil {
		return nil, fmt.Errorf("decode private Key PEM: no block found")
	}
	privKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse private Key: %w", err)
	}
	rsaKey, ok := privKey.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("expected RSA private Key")
	}
	ct, err := base64.StdEncoding.DecodeString(encryptedKeyB64)
	if err != nil {
		return nil, fmt.Errorf("decode ciphertext: %w", err)
	}
	roomKey, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, rsaKey, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt room Key: %w", err)
	}
	return roomKey, nil
}

// EncryptDataWithPEM encrypts roomKey with the given RSA public Key PEM using OAEP-SHA256.
func EncryptDataWithPEM(roomKey []byte, publicKeyPEM string) (string, error) {
	block, _ := pem.Decode([]byte(publicKeyPEM))
	if block == nil {
		return "", fmt.Errorf("decode public Key PEM: no block found")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("parse public Key: %w", err)
	}
	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return "", fmt.Errorf("expected RSA public Key")
	}
	ct, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, rsaPub, roomKey, nil)
	if err != nil {
		return "", fmt.Errorf("encrypt room Key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(ct), nil
}

// GenerateRoomKey generates a random 32-byte Key suitable for AES-256-GCM,
// which provides authenticated encryption fast enough for real-time use (e.g. VoIP).
func GenerateRoomKey() ([]byte, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate room Key: %w", err)
	}

	return key, nil
}

// EncryptRoomKey encrypts roomKey with the RSA-2048 public Key on disk using
// OAEP-SHA256, and returns a base64-encoded ciphertext safe to send to the broker.
func EncryptRoomKey(roomKey []byte, username string) (string, error) {
	_, pubPath, err := keyPaths(username)
	if err != nil {
		return "", err
	}
	pubPEM, err := os.ReadFile(pubPath)
	if err != nil {
		return "", fmt.Errorf("read public Key: %w", err)
	}
	block, _ := pem.Decode(pubPEM)
	if block == nil {
		return "", fmt.Errorf("decode public Key PEM: no block found")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("parse public Key: %w", err)
	}
	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return "", fmt.Errorf("expected RSA public Key")
	}
	ct, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, rsaPub, roomKey, nil)
	if err != nil {
		return "", fmt.Errorf("encrypt room Key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(ct), nil
}

// GenerateMessageEncryptionKey generates a message encryption Key derived from the room Key and user ID
// using HMAC-SHA256
func GenerateMessageEncryptionKey(roomKey []byte, userID string) []byte {
	hash := hmac.New(sha256.New, roomKey)
	hash.Write([]byte(userID))
	return hash.Sum(nil)
}

func FormatHMACInput(userID string, userMessageCount *int) string {
	return fmt.Sprintf("%s/%d", userID, utils.DerefOrZero(userMessageCount))
}

// EncryptMessageWithRoomKey encrypts the message with the room Key
// The function looks like
// key_message1 = HMAC(roomKey, userID || userMessageCount)
// key_message2 = HMAC(key_message1, userID || userMessageCount)
// key_message3 = HMAC(key_message2, userID || userMessageCount)
// it is on a per-user basis to avoid race conditions that can affect message ordering
// once the Key is obtained, ChaCha20-Poly1305 can be used to encrypt the message with the derived Key
// Apparently, ChaCha20-Poly1305 is fast for software-only environments with short messages
// This can be used as a util for a ratchet scheme if userMessageCount is set and increments, using the
// output Key as the new room Key for the next message.
// This way, even if a Key is compromised, only messages encrypted with that Key are at risk, and future messages remain secure.
func (rkc *RoomKeyChain) EncryptMessageWithRoomKey(message []byte, userID string, userMessageCount *int) ([]byte, error) {
	encKey, err := rkc.GetUserKey(userID, utils.DerefOrZero(userMessageCount))
	if err != nil {
		return nil, fmt.Errorf("derive message encryption Key: %w", err)
	}

	aead, err := chacha20poly1305.New(encKey)
	if err != nil {
		return nil, fmt.Errorf("create AEAD cipher: %w", err)
	}

	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	ciphertext := aead.Seal(nil, nonce, message, nil)
	return append(nonce, ciphertext...), nil
}

func (rkc *RoomKeyChain) DecryptMessageWithRoomKey(message []byte, userID string, userMessageCount *int) ([]byte, error) {
	encKey, err := rkc.GetUserKey(userID, utils.DerefOrZero(userMessageCount))
	if err != nil {
		return nil, fmt.Errorf("derive message encryption Key: %w", err)
	}

	aead, err := chacha20poly1305.New(encKey)
	if err != nil {
		return nil, fmt.Errorf("create AEAD cipher: %w", err)
	}

	nonceSize := aead.NonceSize()
	if len(message) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := message[:nonceSize], message[nonceSize:]
	userNonces, ok := NonceMap[userID]
	if !ok {
		userNonces = make(map[string]struct{})
		NonceMap[userID] = userNonces
	}
	if _, exists := userNonces[string(nonce)]; exists {
		return nil, fmt.Errorf("nonce reuse detected for user %s", userID)
	}
	userNonces[string(nonce)] = struct{}{}

	plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt message: %w", err)
	}
	return plaintext, nil
}
