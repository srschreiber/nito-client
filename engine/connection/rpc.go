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

	// pongMu guards pongCh. Exactly one ping may be in flight at a time; a
	// second PingSession call while one is pending will replace the waiter,
	// so callers must serialise themselves if that matters.
	pongMu sync.Mutex
	pongCh chan struct{}
)

// SetVoiceMessageHandler registers a callback invoked for every incoming sounds RPC
// (voice_answer, voice_offer). Safe to call from any goroutine.
func SetVoiceMessageHandler(h func(rpcName string, payload []byte)) {
	vmhMu.Lock()
	voiceMessageHandler = h
	vmhMu.Unlock()
}

// Connect establishes a persistent WebSocket connection to the broker.
// jwtToken must be obtained first via Login. The session device id is
// derived locally from the user's on-disk public key — never trusted from
// the broker — so a compromised broker can't swap it to one we've pinned.
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
	localPubPEM, err := keys.LoadPublicKeyPEM(userID)
	if err != nil {
		return fmt.Errorf("load local public key: %w", err)
	}
	deviceID, err := keys.DeviceIDFromPublicKeyPEM(localPubPEM)
	if err != nil {
		return fmt.Errorf("derive device id: %w", err)
	}
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
	// Pong handler fires when the server replies to our client-initiated ping
	// (see PingSession). Signal the waiter without blocking — if no one's
	// waiting, drop the pong.
	c.SetPongHandler(func(string) error {
		pongMu.Lock()
		ch := pongCh
		pongMu.Unlock()
		if ch != nil {
			select {
			case ch <- struct{}{}:
			default:
			}
		}
		return nil
	})

	roomMessageChan = make(chan []byte, 16)
	nc := make(chan []byte, 16)
	echoChan = make(chan []byte, 16)
	dmChan = make(chan []byte, 16)
	keyVerifyChallChan = make(chan []byte, 8)
	keyVerifyConfirmChan = make(chan []byte, 8)
	lateVerifyRespChan = make(chan string, 8)
	conn = c
	session = &Session{Username: userID, DeviceID: deviceID, BrokerURL: brokerURL, JWTToken: jwtToken, KeyManager: map[string]*keys.RoomKeyChain{}}
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

// PingSession sends a WebSocket ping control frame and waits for the matching
// pong. Returns the round-trip time, or an error if not connected, the write
// fails, or the pong doesn't arrive inside timeout. This measures the live
// session — unlike PingBroker (HTTP), a stale WebSocket will fail here even
// if the broker's HTTP endpoint is still up.
func PingSession(timeout time.Duration) (time.Duration, error) {
	mu.Lock()
	c := conn
	mu.Unlock()
	if c == nil {
		return 0, errors.New("not connected")
	}
	ch := make(chan struct{}, 1)
	pongMu.Lock()
	pongCh = ch
	pongMu.Unlock()
	defer func() {
		pongMu.Lock()
		if pongCh == ch {
			pongCh = nil
		}
		pongMu.Unlock()
	}()

	start := time.Now()
	// WriteControl is safe without wmu (see note in Connect).
	if err := c.WriteControl(websocket.PingMessage, nil, time.Now().Add(timeout)); err != nil {
		return 0, fmt.Errorf("ping write: %w", err)
	}
	select {
	case <-ch:
		return time.Since(start), nil
	case <-time.After(timeout):
		return 0, errors.New("pong timeout")
	}
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
