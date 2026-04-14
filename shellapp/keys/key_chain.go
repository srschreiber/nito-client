// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package keys

import (
	"fmt"

	"golang.org/x/crypto/chacha20poly1305"

	"github.com/srschreiber/nito-client/shared/utils"
)

type Key []byte
type RoomKeyChain struct {
	userChain    map[string]Key
	baseKey      Key
	chainCounter map[string]int
}

func NewRoomKeyChain(baseKey Key) *RoomKeyChain {
	return &RoomKeyChain{
		userChain:    map[string]Key{},
		baseKey:      baseKey,
		chainCounter: map[string]int{},
	}
}

// DecryptHistoricalMessage decrypts a message without nonce-replay tracking.
// Safe for loading server-stored history where replay attacks are not a concern.
func (rkc *RoomKeyChain) DecryptHistoricalMessage(message []byte, userID string, userMessageCount *int) ([]byte, error) {
	encKey, err := rkc.GetUserKey(userID, utils.DerefOrZero(userMessageCount))
	if err != nil {
		return nil, fmt.Errorf("derive message encryption key: %w", err)
	}
	aead, err := chacha20poly1305.New(encKey)
	if err != nil {
		return nil, fmt.Errorf("create AEAD cipher: %w", err)
	}
	nonceSize := aead.NonceSize()
	if len(message) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	plaintext, err := aead.Open(nil, message[:nonceSize], message[nonceSize:], nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt message: %w", err)
	}
	return plaintext, nil
}

// GetUserKey derives the user Key using a ratchet mechanism to ensure forward secrecy.
func (kc *RoomKeyChain) GetUserKey(userId string, messageCount int) (Key, error) {
	if _, ok := kc.userChain[userId]; !ok {
		kc.userChain[userId] = kc.baseKey
		kc.chainCounter[userId] = 0
	}

	if messageCount == kc.chainCounter[userId] {
		return kc.userChain[userId], nil
	}

	for kc.chainCounter[userId] < messageCount {
		c := kc.chainCounter[userId]
		next := GenerateMessageEncryptionKey(kc.userChain[userId], FormatHMACInput(userId, &c))
		kc.userChain[userId] = next
		kc.chainCounter[userId]++
	}

	return kc.userChain[userId], nil
}
