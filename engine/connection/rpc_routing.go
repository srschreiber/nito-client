// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package connection

import (
	"encoding/json"
	"log"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/srschreiber/nito-client/engine/keys"
	wstypes "github.com/srschreiber/nito-client/shared/websocket_types"
)

// ── Incoming channels ─────────────────────────────────────────────────────────
// All populated by readLoop; consumed by higher-level UI/connection code.

var (
	notifChan            chan []byte // server-push notification text
	echoChan             chan []byte // echo messages from the server (for testing connectivity)
	roomMessageChan      chan []byte // incoming room messages (raw JSON for the UI to dispatch)
	dmChan               chan []byte // incoming direct messages, already decrypted (JSON-encoded decryptedDM)
	keyVerifyChallChan   chan []byte // incoming key-verify challenges (raw payload JSON)
	keyVerifyConfirmChan chan []byte // incoming key-verify confirms (raw payload JSON)
	lateVerifyRespChan   chan string // fromUsername of responses that arrived after the session was cancelled/timed out

	// pendingVerifications maps sessionID → chan KeyVerifyResponsePayload for in-flight verify waits.
	// Populated by WaitForKeyVerifyResponse, drained by readLoop.
	pendingVerifications sync.Map
)

// readLoop runs in the background, routing messages to their respective channels.
func readLoop(c *websocket.Conn, echoChan, roomMessageChan, dmChan, nc, kvChalChan, kvConfirmChan chan []byte) {
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
		close(kvChalChan)
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
				myUserID = session.Username
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
				Text:     "incoming sounds clip",
				Username: payload.FromUsername,
				Data:     payload,
			}
			notifBytes, _ := json.Marshal(notif)
			nc <- notifBytes
			continue

		case wstypes.RPCKeyVerifyChallenge:
			select {
			case kvChalChan <- message.Payload:
			default:
			}
			continue

		case wstypes.RPCKeyVerifyConfirm:
			select {
			case kvConfirmChan <- message.Payload:
			default:
			}
			continue

		case wstypes.RPCKeyVerifyResponse:
			var resp wstypes.KeyVerifyResponsePayload
			if json.Unmarshal(message.Payload, &resp) != nil {
				continue
			}
			if ch, ok := pendingVerifications.Load(resp.SessionID); ok {
				respCh := ch.(chan wstypes.KeyVerifyResponsePayload)
				select {
				case respCh <- resp:
				default:
				}
			} else if lateVerifyRespChan != nil {
				// Session is no longer being waited on — cancelled, timed out,
				// or the sender restarted. Surface it so the UI can warn.
				select {
				case lateVerifyRespChan <- resp.ToUsername:
				default:
				}
			}
			continue
		}
	}
}

// ── Channel accessors ─────────────────────────────────────────────────────────

// NotifChan returns a receive-only channel of server-push notification text.
// Returns nil when not connected.
func NotifChan() <-chan []byte {
	mu.Lock()
	defer mu.Unlock()
	return notifChan
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

// KeyVerifyChallengeChan returns the channel that receives incoming verification
// challenges (raw KeyVerifyChallengePayload JSON). Returns nil when not connected.
func KeyVerifyChallengeChan() <-chan []byte {
	mu.Lock()
	defer mu.Unlock()
	return keyVerifyChallChan
}

// KeyVerifyConfirmChan returns the channel that receives incoming verification
// confirms (raw KeyVerifyConfirmPayload JSON). Returns nil when not connected.
func KeyVerifyConfirmChan() <-chan []byte {
	mu.Lock()
	defer mu.Unlock()
	return keyVerifyConfirmChan
}

// LateVerifyResponseChan returns a channel of `fromUsername` values for
// KeyVerifyResponse messages that arrived after the local session was
// cancelled/timed out. UI should warn the user so they know not to trust any
// resumed flow for that peer. Returns nil when not connected.
func LateVerifyResponseChan() <-chan string {
	mu.Lock()
	defer mu.Unlock()
	return lateVerifyRespChan
}
