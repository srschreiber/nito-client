// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package connection

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/srschreiber/nito-client/engine/keys"
	apitypes "github.com/srschreiber/nito-client/shared/api_types"
	"github.com/srschreiber/nito-client/shared/utils"
	wstypes "github.com/srschreiber/nito-client/shared/websocket_types"
	"github.com/srschreiber/nito-client/ui/clientlog"
)

// RoomInfo holds per-room state fetched on room selection and updated locally.
type RoomInfo struct {
	// SentMessageCount is the number of messages this user has sent in this room.
	// Incremented locally after each successful send; used as the ratchet counter for encryption.
	SentMessageCount int
}

type Session struct {
	UserID           string // username (used as the identity token sent to the broker)
	BrokerURL        string
	JWTToken         string                        // JWT token for API authentication
	RoomID           *string                       // currently selected room
	EncryptedRoomKey *string                       // encrypted with pub key
	KeyManager       map[string]*keys.RoomKeyChain // in-memory cache of room key chains for each room, indexed by room ID
	RoomKeyVersion   *int                          // key version for the current room's key
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

var (
	mu              sync.Mutex
	wmu             sync.Mutex // serializes all writes to conn
	conn            *websocket.Conn
	session         *Session
	notifChan       chan []byte // server-push notification text
	echoChan        chan []byte // echo messages from the server (for testing connectivity)
	roomMessageChan chan []byte // incoming room messages (raw JSON for the TUI model to dispatch)
	dmChan          chan []byte // incoming direct messages, already decrypted (JSON-encoded decryptedDM)

	vmhMu               sync.Mutex
	voiceMessageHandler func(rpcName string, payload []byte)

	// Last successful connection credentials; used by Reconnect.
	credMu       sync.Mutex
	storedBroker string
	storedUserID string
	storedJWT    string
)

// SetVoiceMessageHandler registers a callback invoked for every incoming voice RPC
// (voice_answer, voice_offer). Safe to call from any goroutine.
func SetVoiceMessageHandler(h func(rpcName string, payload []byte)) {
	vmhMu.Lock()
	voiceMessageHandler = h
	vmhMu.Unlock()
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
	return keys.DecryptRoomKey(*session.EncryptedRoomKey, session.UserID)
}

func normalizeURL(url string) string {
	url = strings.TrimPrefix(url, "ws://")
	url = strings.TrimPrefix(url, "wss://")
	url = strings.TrimPrefix(url, "http://")
	url = strings.TrimPrefix(url, "https://")
	return url
}

// Register sends username and public key to the broker, creating a DB entry if the user
// doesn't exist yet. Returns the user's ID and whether they were already registered.
func Register(ctx context.Context, brokerURL, username, password, publicKey string) (*apitypes.RegisterResponse, error) {
	brokerURL = normalizeURL(brokerURL)
	body, _ := json.Marshal(apitypes.RegisterRequest{
		Username:   username,
		Password:   password,
		PublicKey:  publicKey,
		DeviceName: "primary",
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+brokerURL+"/api/v0/register", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("register: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("register: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("register: broker returned %s", resp.Status)
	}
	var result apitypes.RegisterResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("register: decode response: %w", err)
	}
	return &result, nil
}

// Login performs the full authentication flow against the broker:
// requests a challenge, signs it, and exchanges credentials for a JWT token.
func Login(ctx context.Context, brokerURL, username, password string) (string, error) {
	brokerURL = normalizeURL(brokerURL)
	keys.SetActiveBroker(brokerURL)

	// Request challenge.
	challengeBody, _ := json.Marshal(apitypes.LoginChallengeRequest{Username: username})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+brokerURL+"/api/v0/login/challenge", bytes.NewReader(challengeBody))
	if err != nil {
		return "", fmt.Errorf("login challenge: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("login challenge: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("login challenge: broker returned %s", resp.Status)
	}
	var challengeResp apitypes.LoginChallengeResponse
	if err := json.NewDecoder(resp.Body).Decode(&challengeResp); err != nil {
		return "", fmt.Errorf("login challenge: decode: %w", err)
	}

	// Sign "login:<username>:<challenge>" with our private key.
	msg := fmt.Sprintf("login:%s:%s", username, challengeResp.Challenge)
	sig, err := keys.Sign(msg, username)
	if err != nil {
		return "", fmt.Errorf("login sign: %w", err)
	}

	// Exchange credentials + signature for a JWT.
	loginBody, _ := json.Marshal(apitypes.LoginRequest{
		Username:  username,
		Password:  password,
		Challenge: challengeResp.Challenge,
		Signature: sig,
	})
	loginReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+brokerURL+"/api/v0/login", bytes.NewReader(loginBody))
	if err != nil {
		return "", fmt.Errorf("login: %w", err)
	}
	loginReq.Header.Set("Content-Type", "application/json")
	loginResp, err := http.DefaultClient.Do(loginReq)
	if err != nil {
		return "", fmt.Errorf("login: %w", err)
	}
	defer loginResp.Body.Close()
	if loginResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("login: broker returned %s", loginResp.Status)
	}
	var result apitypes.LoginResponse
	if err := json.NewDecoder(loginResp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("login: decode: %w", err)
	}
	return result.Token, nil
}

// Connect establishes a persistent WebSocket connection to the broker.
// jwtToken must be obtained first via Login.
func Connect(ctx context.Context, brokerURL, userID, jwtToken string) error {
	mu.Lock()
	defer mu.Unlock()

	if conn != nil {
		conn.Close()
		conn = nil
		session = nil
	}

	brokerURL = normalizeURL(brokerURL)
	keys.SetActiveBroker(brokerURL)
	credMu.Lock()
	storedBroker, storedUserID, storedJWT = brokerURL, userID, jwtToken
	credMu.Unlock()
	sig, err := keys.Sign(userID+":/ws", userID)
	if err != nil {
		return fmt.Errorf("sign handshake: %w", err)
	}
	headers := http.Header{}
	headers.Set("X-Username", userID)
	headers.Set("X-Signature", sig)
	headers.Set("Authorization", "Bearer "+jwtToken)
	dialer := &websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	c, _, err := dialer.DialContext(ctx, "ws://"+brokerURL+"/ws?user_id="+userID, headers)
	if err != nil {
		return err
	}

	// WriteControl is safe to call concurrently with WriteMessage, so no wmu needed.
	c.SetPingHandler(func(data string) error {
		return c.WriteControl(websocket.PongMessage, []byte(data), time.Now().Add(5*time.Second))
	})

	roomMessageChan = make(chan []byte, 16)
	nc := make(chan []byte, 16)
	echoChan = make(chan []byte, 16)
	dmChan = make(chan []byte, 16)
	conn = c
	session = &Session{UserID: userID, BrokerURL: brokerURL, JWTToken: jwtToken, KeyManager: map[string]*keys.RoomKeyChain{}}
	notifChan = nc

	go readLoop(c, echoChan, roomMessageChan, dmChan, nc)
	clientlog.Info("connected to broker %s as %s", brokerURL, userID)
	return nil
}

// readLoop runs in the background, routing messages to their respective channels.
func readLoop(c *websocket.Conn, echoChan, roomMessageChan, dmChan, nc chan []byte) {
	defer func() {
		mu.Lock()
		if conn == c {
			conn = nil
			session = nil
		}
		mu.Unlock()
		close(roomMessageChan)
		close(dmChan)
		close(nc)
	}()
	for {
		_, data, err := c.ReadMessage()
		if err != nil {
			return
		}
		var message wstypes.ToClientWsMessage
		if json.Unmarshal(data, &message) != nil {
			log.Println("unmarshal WS message:", err)
			continue
		}

		switch message.RPCName {
		case wstypes.RPCVoiceAnswer, wstypes.RPCVoiceOffer:
			vmhMu.Lock()
			h := voiceMessageHandler
			vmhMu.Unlock()
			if h != nil {
				go h(message.RPCName, message.Payload)
			}
			continue
		case wstypes.RPCNotification:
			var notificationPayload wstypes.NotificationPayload
			if json.Unmarshal(data, &notificationPayload) != nil {
				log.Println("unmarshal notification payload:", err)
				continue
			}
			nc <- message.Payload
			continue
		case wstypes.RPCEcho:
			var echoPayload wstypes.EchoPayload
			if json.Unmarshal(message.Payload, &echoPayload) != nil {
				log.Printf("Echo from server: %s", echoPayload.Text)
				continue
			}
			echoChan <- message.Payload
			continue
		case wstypes.RPCRoomMessage:
			var roomMessagePayload wstypes.RoomMessagePayload
			if json.Unmarshal(message.Payload, &roomMessagePayload) != nil {
				log.Printf("Message from %s in room %s: %s", roomMessagePayload.FromUsername, roomMessagePayload.RoomID, roomMessagePayload.EncryptedText)
				continue
			}
			roomMessageChan <- message.Payload
			continue
		case wstypes.RPCDirectMessage:
			var payload wstypes.DirectMessagePayload
			if json.Unmarshal(message.Payload, &payload) != nil {
				log.Println("unmarshal DM payload:", err)
				continue
			}
			mu.Lock()
			myUserID := ""
			if session != nil {
				myUserID = session.UserID
			}
			mu.Unlock()
			if myUserID == "" {
				continue
			}
			plainBytes, decErr := keys.DecryptRoomKey(payload.CipherText, myUserID)
			if decErr != nil {
				log.Printf("decrypt DM from %s: %v", payload.FromUsername, decErr)
				continue
			}
			fromUser := payload.FromUsername
			if fromUser == "" {
				fromUser = message.UserID // fall back to outer envelope UserID
			}
			dmData, _ := json.Marshal(decryptedDM{
				FromUsername: fromUser,
				PlainText:    string(plainBytes),
			})
			dmChan <- dmData
			continue
		case wstypes.PlayAudio:
			var payload wstypes.PlayAudioPayload
			if json.Unmarshal(message.Payload, &payload) != nil {
				log.Println("unmarshal PlayAudio payload:", err)
			}

			// just wrap as a notification to use same plumbing. sorta a hack, but avoids additional wiring
			notif := wstypes.NotificationPayload{
				Type:     wstypes.NotificationTypeSoundClip,
				Text:     "incoming sound clip",
				Username: payload.FromUsername,
				Data:     payload,
			}

			// conv to bytes
			notifBytes, _ := json.Marshal(notif)
			nc <- notifBytes
			continue
		}
	}
}

// NotifChan returns a receive-only channel of server-push notification text.
// Returns nil when not connected.
func NotifChan() <-chan []byte {
	mu.Lock()
	defer mu.Unlock()
	return notifChan
}

// Reconnect re-establishes the WebSocket connection using the credentials from the last
// successful Connect call. Returns an error if no prior connection was made or if the
// dial fails (e.g. JWT expired, broker unreachable).
func Reconnect(ctx context.Context) error {
	credMu.Lock()
	url, uid, jwt := storedBroker, storedUserID, storedJWT
	credMu.Unlock()
	if url == "" || uid == "" || jwt == "" {
		return errors.New("no prior connection credentials")
	}
	return Connect(ctx, url, uid, jwt)
}

func Disconnect() {
	clientlog.Info("disconnecting from broker")
	mu.Lock()
	defer mu.Unlock()
	if conn != nil {
		conn.Close()
		conn = nil
	}
	session = nil
}

func IsConnected() bool {
	mu.Lock()
	defer mu.Unlock()
	return conn != nil
}

func CurrentSession() *Session {
	mu.Lock()
	defer mu.Unlock()
	return session
}

// Send writes a JSON-encoded message to the active WebSocket connection.
func Send(data []byte) error {
	mu.Lock()
	defer mu.Unlock()
	if conn == nil {
		return errors.New("not connected")
	}
	wmu.Lock()
	defer wmu.Unlock()
	return conn.WriteMessage(websocket.TextMessage, data)
}

func EchoChan() <-chan []byte {
	mu.Lock()
	defer mu.Unlock()
	return echoChan
}

func RoomMessageChan() <-chan []byte {
	mu.Lock()
	defer mu.Unlock()
	return roomMessageChan
}

// DMChan returns a receive-only channel of decrypted direct messages (JSON-encoded decryptedDM).
// Returns nil when not connected.
func DMChan() <-chan []byte {
	mu.Lock()
	defer mu.Unlock()
	return dmChan
}

// signedPost builds a POST request with X-Username, X-Signature, and Authorization headers.
// apiPath is the bare path (e.g. "/api/v0/rooms") used as the signature payload.
func signedPost(url, username, apiPath string, body []byte) (*http.Response, error) {
	sig, err := keys.Sign(username+":"+apiPath, username)
	if err != nil {
		return nil, fmt.Errorf("sign request: %w", err)
	}
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Username", username)
	req.Header.Set("X-Signature", sig)
	if s := CurrentSession(); s != nil && s.JWTToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.JWTToken)
	}
	return http.DefaultClient.Do(req)
}

// signedGet builds a GET request with X-Username, X-Signature, and Authorization headers.
func signedGet(url, username, apiPath string) (*http.Response, error) {
	sig, err := keys.Sign(username+":"+apiPath, username)
	if err != nil {
		return nil, fmt.Errorf("sign request: %w", err)
	}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Username", username)
	req.Header.Set("X-Signature", sig)
	if s := CurrentSession(); s != nil && s.JWTToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.JWTToken)
	}
	return http.DefaultClient.Do(req)
}

// CreateRoom creates a new room on the broker. Requires an active session.
// encryptedRoomKey is the base64-encoded RSA-OAEP ciphertext of the room's AES key.
func CreateRoom(name, encryptedRoomKey string) (id, roomName string, err error) {
	s := CurrentSession()
	if s == nil {
		return "", "", errors.New("not connected")
	}
	body, _ := json.Marshal(map[string]string{
		"name":             name,
		"userId":           s.UserID,
		"encryptedRoomKey": encryptedRoomKey,
	})
	resp, err := signedPost(s.v0("/rooms"), s.UserID, "/api/v0/rooms", body)
	if err != nil {
		return "", "", fmt.Errorf("create room: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("create room: broker returned %s", resp.Status)
	}
	var result struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", fmt.Errorf("create room: decode: %w", err)
	}
	return result.ID, result.Name, nil
}

// ListRooms returns all rooms the current user is a member of.
func ListRooms() ([]apitypes.RoomEntry, error) {
	s := CurrentSession()
	if s == nil {
		return nil, errors.New("not connected")
	}
	resp, err := signedGet(
		s.v0("/rooms/list?user_id="+s.UserID),
		s.UserID,
		"/api/v0/rooms/list",
	)
	if err != nil {
		return nil, fmt.Errorf("list rooms: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list rooms: broker returned %s", resp.Status)
	}
	var result struct {
		Rooms []apitypes.RoomEntry `json:"rooms"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("list rooms: decode: %w", err)
	}
	return result.Rooms, nil
}

func BrokerURL() string {
	mu.Lock()
	defer mu.Unlock()
	if session == nil {
		return ""
	}
	return session.BrokerURL
}

// SetSessionRoom stores the selected room ID in the session, fetching the room key and room info.
func SetSessionRoom(roomID string) error {
	mu.Lock()
	if session == nil {
		mu.Unlock()
		return fmt.Errorf("not connected")
	}
	mu.Unlock()

	// Fetch room key and room info outside the lock to avoid deadlock
	// (both call CurrentSession which also locks mu).
	rk, kv, err := getMyRoomKey(roomID)
	if err != nil {
		return fmt.Errorf("room-select: retrieve room key failed: %w", err)
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
	session.RoomInfo = info
	clientlog.Info("joined room %s", roomID)
	return nil
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

// GetSessionEncryptedRoomKey returns the encrypted room key for the start of the chain
func GetSessionEncryptedRoomKey() *string {
	mu.Lock()
	defer mu.Unlock()
	if session == nil {
		return nil
	}
	return session.EncryptedRoomKey
}

func GetSessionUserID() string {
	mu.Lock()
	defer mu.Unlock()
	if session == nil {
		return ""
	}
	return session.UserID
}

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
		// decrypt
		keyDecrypted, err := keys.DecryptRoomKey(utils.DerefOrZero(baseKey), session.UserID)
		if err != nil {
			log.Printf("decrypt room key for room %s: %v", roomID, err)
			// return an empty chain that will fail to encrypt/decrypt, rather than panicking or returning an error that callers would have to handle
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

var pingClient = &http.Client{Timeout: 5 * time.Second}

// PingBroker sends a ping to the broker's HTTP API and returns an error if unreachable.
// Returns an error immediately if there is no active session.
func PingBroker() error {
	mu.Lock()
	s := session
	mu.Unlock()
	if s == nil {
		return errors.New("not connected")
	}
	body, _ := json.Marshal(map[string]string{"message": "ping"})
	resp, err := pingClient.Post(s.v0("/ping"), "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ping: broker returned %s", resp.Status)
	}
	return nil
}

// GetUserPublicKey fetches the public key PEM for a given username from the broker.
func GetUserPublicKey(username string) (string, error) {
	s := CurrentSession()
	if s == nil {
		return "", errors.New("not connected")
	}
	resp, err := http.Get(s.v0("/users/public-key?username=" + username))
	if err != nil {
		return "", fmt.Errorf("get public key: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("get public key: broker returned %s", resp.Status)
	}
	var result struct {
		PublicKey string `json:"publicKey"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("get public key: decode: %w", err)
	}
	return result.PublicKey, nil
}

// getMyRoomKey fetches the caller's encrypted room key and key version for the given room.
func getMyRoomKey(roomID string) (encryptedKey string, keyVersion int, err error) {
	s := CurrentSession()
	if s == nil {
		return "", 0, errors.New("not connected")
	}
	resp, err := signedGet(
		s.v0("/rooms/key?room_id="+roomID),
		s.UserID,
		"/api/v0/rooms/key",
	)
	if err != nil {
		return "", 0, fmt.Errorf("get room key: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("get room key: broker returned %s", resp.Status)
	}
	var result apitypes.GetRoomKeyResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", 0, fmt.Errorf("get room key: decode: %w", err)
	}
	return result.EncryptedRoomKey, result.KeyVersion, nil
}

// getRoomInfo fetches the caller's room info (e.g. sent message count) for the given room.
func getRoomInfo(roomID string) (*RoomInfo, error) {
	s := CurrentSession()
	if s == nil {
		return nil, errors.New("not connected")
	}
	resp, err := signedGet(
		s.v0("/rooms/info?room_id="+roomID),
		s.UserID,
		"/api/v0/rooms/info",
	)
	if err != nil {
		return nil, fmt.Errorf("get room info: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get room info: broker returned %s", resp.Status)
	}
	var result apitypes.GetRoomInfoResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("get room info: decode: %w", err)
	}
	return &RoomInfo{SentMessageCount: result.SentMessageCount}, nil
}

// InviteUser invites a user to a room, sending their encrypted copy of the room key.
func InviteUser(roomID, invitedUsername, encryptedRoomKey string) error {
	s := CurrentSession()
	if s == nil {
		return errors.New("not connected")
	}
	body, _ := json.Marshal(map[string]string{
		"roomId":           roomID,
		"invitedUsername":  invitedUsername,
		"encryptedRoomKey": encryptedRoomKey,
	})
	resp, err := signedPost(s.v0("/rooms/invite"), s.UserID, "/api/v0/rooms/invite", body)
	if err != nil {
		return fmt.Errorf("invite user: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("invite user: broker returned %s", resp.Status)
	}
	return nil
}

// ListRoomMembers returns the joined members of a room.
func ListRoomMembers(roomID string) ([]apitypes.RoomMemberEntry, error) {
	s := CurrentSession()
	if s == nil {
		return nil, errors.New("not connected")
	}
	resp, err := signedGet(
		s.v0("/rooms/members?room_id="+roomID),
		s.UserID,
		"/api/v0/rooms/members",
	)
	if err != nil {
		return nil, fmt.Errorf("list room members: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list room members: broker returned %s", resp.Status)
	}
	var result struct {
		Members []apitypes.RoomMemberEntry `json:"members"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("list room members: decode: %w", err)
	}
	return result.Members, nil
}

// ListPendingInvites returns rooms the current user has been invited to but not yet joined.
func ListPendingInvites() ([]apitypes.PendingInvite, error) {
	s := CurrentSession()
	if s == nil {
		return nil, errors.New("not connected")
	}
	resp, err := signedGet(
		s.v0("/rooms/invites?user_id="+s.UserID),
		s.UserID,
		"/api/v0/rooms/invites",
	)
	if err != nil {
		return nil, fmt.Errorf("list invites: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list invites: broker returned %s", resp.Status)
	}
	var result struct {
		Invites []apitypes.PendingInvite `json:"invites"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("list invites: decode: %w", err)
	}
	return result.Invites, nil
}

// GetRoomMessages fetches the most recent limit messages from the given room along with
// the encrypted room keys needed to decrypt them.
func GetRoomMessages(roomID string, limit int) (*apitypes.GetRoomMessagesResponse, error) {
	s := CurrentSession()
	if s == nil {
		return nil, errors.New("not connected")
	}
	body, _ := json.Marshal(apitypes.GetRoomMessagesRequest{
		RoomID: roomID,
		Limit:  &limit,
	})
	resp, err := signedPost(s.v0("/rooms/messages"), s.UserID, "/api/v0/rooms/messages", body)
	if err != nil {
		return nil, fmt.Errorf("get room messages: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get room messages: broker returned %s", resp.Status)
	}
	var result apitypes.GetRoomMessagesResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("get room messages: decode: %w", err)
	}
	return &result, nil
}

// AcceptInvite accepts a pending room invitation.
func AcceptInvite(roomID string) error {
	s := CurrentSession()
	if s == nil {
		return errors.New("not connected")
	}
	body, _ := json.Marshal(map[string]string{"roomId": roomID})
	resp, err := signedPost(s.v0("/rooms/invites/accept"), s.UserID, "/api/v0/rooms/invites/accept", body)
	if err != nil {
		return fmt.Errorf("accept invite: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("accept invite: broker returned %s", resp.Status)
		clientlog.Error("invite accept failed: %v", err)
		return err
	}
	clientlog.Info("accepted invite to room %s", roomID)
	return nil
}
