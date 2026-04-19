// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package connection

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/srschreiber/nito-client/engine/keys"
	wstypes "github.com/srschreiber/nito-client/shared/websocket_types"
	"github.com/srschreiber/nito-client/ui/clientlog"
)

var (
	vmhMu               sync.Mutex // serialises updates to voiceMessageHandler
	voiceMessageHandler func(rpcName string, payload []byte)
)

// SetVoiceMessageHandler registers a callback invoked for every incoming sounds RPC
// (voice_answer, voice_offer). Safe to call from any goroutine.
func SetVoiceMessageHandler(h func(rpcName string, payload []byte)) {
	vmhMu.Lock()
	voiceMessageHandler = h
	vmhMu.Unlock()
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
	keyVerifyChallChan = make(chan []byte, 8)
	keyVerifyConfirmChan = make(chan []byte, 8)
	lateVerifyRespChan = make(chan string, 8)
	conn = c
	session = &Session{UserID: userID, BrokerURL: brokerURL, JWTToken: jwtToken, KeyManager: map[string]*keys.RoomKeyChain{}}
	notifChan = nc

	go readLoop(c, echoChan, roomMessageChan, dmChan, nc, keyVerifyChallChan, keyVerifyConfirmChan)
	clientlog.Info("connected to broker %s as %s", brokerURL, userID)
	return nil
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

// sendWsRPC marshals payload as a ToBrokerWsMessage with a fresh nonce/timestamp and sends it.
func sendWsRPC(rpcName string, userID string, payload any) error {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	msg := wstypes.ToBrokerWsMessage{
		RPCName:   rpcName,
		RequestID: fmt.Sprintf("%d", time.Now().UnixNano()),
		UserID:    userID,
		Nonce:     fmt.Sprintf("%d", time.Now().UnixNano()),
		Timestamp: time.Now().Unix(),
		Payload:   payloadBytes,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}
	return Send(data)
}
