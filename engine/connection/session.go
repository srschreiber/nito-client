// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

// Package connection manages the client's broker connection: HTTP REST calls,
// the persistent WebSocket, session state (room key cache, credentials), and
// the key-verification flow. Files:
//
//	session.go        — types, globals, session-state accessors
//	utils.go          — signed HTTP helpers, URL normalisation
//	login.go           — REST endpoints (register, login, ping, public-key)
//	rooms.go          — REST endpoints for rooms (create/list/invite/etc.)
//	rpc.go            — WebSocket lifecycle (Connect/Disconnect/Send) + sendWsRPC
//	rpc_routing.go    — readLoop + channel accessors that route incoming frames
//	keyverify.go      — mutual key-verification RPCs (challenge / response / confirm)
//	giphy_client.go   — Giphy-proxy REST client
package connection

import (
	"errors"
	"fmt"
	"log"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/srschreiber/nito-client/engine/keys"
	"github.com/srschreiber/nito-client/shared/utils"
	"github.com/srschreiber/nito-client/ui/clientlog"
)

// RoomInfo holds per-room state fetched on room selection and updated locally.
type RoomInfo struct {
	// SentMessageCount is the number of messages this user has sent in this room.
	// Incremented locally after each successful send; used as the ratchet counter for encryption.
	SentMessageCount int
}

type Session struct {
	Username         string // the authenticated user's username; used as the identity token on every broker call
	BrokerURL        string
	JWTToken         string                        // JWT token for API authentication
	RoomID           *string                       // currently selected room
	EncryptedRoomKey *string                       // encrypted with pub key
	KeyManager       map[string]*keys.RoomKeyChain // in-memory cache of room key chains for each room, indexed by room ID
	RoomKeyVersion   *int                          // key version for the current room's key
	RoomKeyRotator   string                        // username who signed the current room key's manifest (empty if no manifest)
	RoomInfo         *RoomInfo                     // info about this user's activity in the selected room
}

// v0 returns the full HTTP URL for the given /api/v0 path (e.g. "/rooms").
func (s *Session) v0(path string) string {
	return "http://" + s.BrokerURL + "/api/v0" + path
}

// decryptedDM is the struct marshalled into dmChan after the readLoop decrypts
// an incoming direct message with the local private key.
type decryptedDM struct {
	FromUsername string `json:"fromUsername"`
	PlainText    string `json:"plainText"`
}

// ── Shared globals ────────────────────────────────────────────────────────────
// mu guards session and the WebSocket connection + the channels below. Other
// files in this package acquire mu freely; sendWsRPC / Send / CurrentSession
// all go through the same lock.

var (
	mu      sync.Mutex
	wmu     sync.Mutex // serialises writes to conn (WebSocket.WriteMessage is not concurrency-safe)
	conn    *websocket.Conn
	session *Session

	// Last successful connection credentials; used by Reconnect.
	credMu       sync.Mutex
	storedBroker string
	storedUserID string
	storedJWT    string
)

// CurrentSession returns a pointer to the active session, or nil if disconnected.
func CurrentSession() *Session {
	mu.Lock()
	defer mu.Unlock()
	return session
}

// BrokerURL returns the currently-connected broker's URL, or "" if none.
func BrokerURL() string {
	mu.Lock()
	defer mu.Unlock()
	if session == nil {
		return ""
	}
	return session.BrokerURL
}

// GetSessionRoomID returns the currently selected room ID, or nil if none selected.
func GetSessionRoomID() *string {
	mu.Lock()
	defer mu.Unlock()
	if session == nil {
		return nil
	}
	return session.RoomID
}

// GetSessionEncryptedRoomKey returns the encrypted room key for the start of the chain.
func GetSessionEncryptedRoomKey() *string {
	mu.Lock()
	defer mu.Unlock()
	if session == nil {
		return nil
	}
	return session.EncryptedRoomKey
}

// GetSessionUsername returns the authenticated user's username, or "" if no
// session is active. (Formerly GetSessionUserID — renamed because Session's
// primary identity token is a username, not an opaque id.)
func GetSessionUsername() string {
	mu.Lock()
	defer mu.Unlock()
	if session == nil {
		return ""
	}
	return session.Username
}

// GetOrCreateRoomKeyChain returns the ratcheting AES-key chain for the active
// room, decrypting the stored room key on first use.
func GetOrCreateRoomKeyChain() (*keys.RoomKeyChain, error) {
	mu.Lock()
	defer mu.Unlock()
	if session == nil {
		return nil, fmt.Errorf("not connected")
	}
	roomID := utils.DerefOrZero(session.RoomID)
	chain, ok := session.KeyManager[roomID]
	if !ok {
		baseKey := session.EncryptedRoomKey
		keyDecrypted, err := keys.DecryptRoomKey(utils.DerefOrZero(baseKey), session.Username)
		if err != nil {
			log.Printf("decrypt room key for room %s: %v", roomID, err)
			return nil, err
		}
		chain = keys.NewRoomKeyChain(keyDecrypted)
		session.KeyManager[roomID] = chain
	}
	return chain, nil
}

func GetSessionRoomKeyVersion() *int {
	mu.Lock()
	defer mu.Unlock()
	if session == nil {
		return nil
	}
	return session.RoomKeyVersion
}

// GetSessionRoomInfo returns a copy of the current room info, or nil if no room is selected.
func GetSessionRoomInfo() *RoomInfo {
	mu.Lock()
	defer mu.Unlock()
	if session == nil || session.RoomInfo == nil {
		return nil
	}
	copy := *session.RoomInfo
	return &copy
}

// IncrementSessionSentMessageCount atomically increments the sent message counter for the selected room.
func IncrementSessionSentMessageCount() {
	mu.Lock()
	defer mu.Unlock()
	if session != nil && session.RoomInfo != nil {
		session.RoomInfo.SentMessageCount++
	}
}

// GetRoomKeyBytes returns the raw decrypted bytes of the current room's AES key.
func GetRoomKeyBytes() ([]byte, error) {
	mu.Lock()
	defer mu.Unlock()
	if session == nil {
		return nil, errors.New("not connected")
	}
	if session.EncryptedRoomKey == nil {
		return nil, errors.New("no room selected")
	}
	return keys.DecryptRoomKey(*session.EncryptedRoomKey, session.Username)
}

// UnverifiedRotatorError is returned by SetSessionRoom when the room's current
// key was signed by a rotator whose public key is only TOFU-pinned — i.e. the
// signature is mathematically valid but the UI hasn't cryptographically
// verified that identity through the mutual-handshake flow. The UI should
// warn the user, offer to run verification, and retry via
// SetSessionRoomTrustingUnverified to bypass the check.
type UnverifiedRotatorError struct {
	RoomID  string
	Rotator string
}

func (e *UnverifiedRotatorError) Error() string {
	return fmt.Sprintf("room %s: key rotator %q is not verified out-of-band", e.RoomID, e.Rotator)
}

// SetSessionRoom joins the given room, enforcing that the room-key rotator's
// identity is already cryptographically verified (or is ourselves). Returns
// *UnverifiedRotatorError if the rotator is only TOFU-pinned, leaving the
// session unchanged.
func SetSessionRoom(roomID string) error {
	return setSessionRoomWithTrust(roomID, false)
}

// SetSessionRoomTrustingUnverified is the bypass variant called after the UI
// has warned the user about an unverified rotator and received explicit
// consent. It joins the room regardless of the rotator's trust level.
func SetSessionRoomTrustingUnverified(roomID string) error {
	return setSessionRoomWithTrust(roomID, true)
}

func setSessionRoomWithTrust(roomID string, allowUnverified bool) error {
	mu.Lock()
	if session == nil {
		mu.Unlock()
		return fmt.Errorf("not connected")
	}
	me := session.Username
	mu.Unlock()

	// Fetch room key and room info outside the lock to avoid deadlock
	// (both call CurrentSession which also locks mu).
	rk, kv, rotator, err := getMyRoomKey(roomID)
	if err != nil {
		return fmt.Errorf("room-select: retrieve room key failed: %w", err)
	}

	// Trust check: the rotator's manifest signature already verified, but we
	// want an explicit user-approved TOFU-bypass before we commit to using
	// this key. Skip the check if the rotator is ourselves — we trust our own
	// key store implicitly — or if the caller opted in via the bypass entry.
	if !allowUnverified && rotator != "" && rotator != me {
		rec, ok := keys.LoadPeerPublicKey(rotator)
		if !ok || !rec.Verified {
			return &UnverifiedRotatorError{RoomID: roomID, Rotator: rotator}
		}
	}

	info, err := getRoomInfo(roomID)
	if err != nil {
		return fmt.Errorf("room-select: retrieve room info failed: %w", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if session == nil {
		return fmt.Errorf("not connected")
	}
	session.RoomID = &roomID
	session.EncryptedRoomKey = &rk
	session.RoomKeyVersion = &kv
	session.RoomKeyRotator = rotator
	session.RoomInfo = info
	clientlog.Info("joined room %s", roomID)
	return nil
}

// GetSessionRoomRotator returns the username that signed the current room's
// key manifest, or "" if none is active. UI uses this to display a trust
// banner at the top of the room view.
func GetSessionRoomRotator() string {
	mu.Lock()
	defer mu.Unlock()
	if session == nil {
		return ""
	}
	return session.RoomKeyRotator
}
