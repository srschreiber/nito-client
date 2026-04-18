// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package main

import (
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/srschreiber/nito-client/engine/connection"
	"github.com/srschreiber/nito-client/engine/keys"
	"github.com/srschreiber/nito-client/engine/sounds"
	apitypes "github.com/srschreiber/nito-client/shared/api_types"
	wstypes "github.com/srschreiber/nito-client/shared/websocket_types"
	"github.com/srschreiber/nito-client/ui/clientlog"

	"fyne.io/fyne/v2"
)

// decodeSoundClipPayload extracts a PlayAudioPayload from a NotificationPayload.
// After a JSON round-trip, notif.Data becomes map[string]any, so we marshal+unmarshal.
func decodeSoundClipPayload(notif wstypes.NotificationPayload) (wstypes.PlayAudioPayload, bool) {
	raw, err := json.Marshal(notif.Data)
	if err != nil {
		return wstypes.PlayAudioPayload{}, false
	}
	var p wstypes.PlayAudioPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return wstypes.PlayAudioPayload{}, false
	}
	return p, true
}

// decryptHistoricalMessages decrypts a GetRoomMessagesResponse into chatMessages.
// Messages that cannot be decrypted are silently skipped.
func decryptHistoricalMessages(resp *apitypes.GetRoomMessagesResponse) []chatMessage {
	if resp == nil {
		return nil
	}

	myUserID := connection.GetSessionUserID()

	// Build one RoomKeyChain per key version using the user's encrypted copies.
	keyChains := make(map[int]*keys.RoomKeyChain)
	for _, rk := range resp.RoomKeys {
		rawKey, err := keys.DecryptRoomKey(rk.EncryptedRoomKey, myUserID)
		if err != nil {
			continue
		}
		keyChains[rk.KeyVersion] = keys.NewRoomKeyChain(rawKey)
	}

	var out []chatMessage
	for _, msg := range resp.UserMessages {
		chain, ok := keyChains[msg.RoomKeyVersion]
		if !ok {
			continue
		}
		ct, err := base64.StdEncoding.DecodeString(msg.EncryptedMessage)
		if err != nil {
			continue
		}
		plaintext, err := chain.DecryptHistoricalMessage(ct, msg.SenderUsername, &msg.SenderMessageCount)
		if err != nil {
			continue
		}

		kind := msgChat
		if msg.SenderUsername == myUserID {
			kind = msgSelf
		}
		out = append(out, chatMessage{
			kind:      kind,
			timestamp: msg.CreatedAt.Local().Format("15:04"),
			date:      msg.CreatedAt.Local().Format("2006-01-02"),
			from:      msg.SenderUsername,
			body:      string(plaintext),
		})
	}
	return out
}

// startBackend launches all background receive loops. Call from main goroutine
// after the session is established and the UI panels exist.
func startBackend(cp *ChatPanel, sp *StatusPanel, w fyne.Window) {
	go messageReceiveLoop(cp, w)
	go dmReceiveLoop(cp, w)
	go notifLoop(cp, sp, w)
	go roomPollLoop(cp, sp)
	go keyVerifyChallengeLoop(w)
}

// keyVerifyChallengeLoop forwards incoming key-verification challenges to the UI.
// Runs until the program exits; restarts automatically on reconnect.
func keyVerifyChallengeLoop(w fyne.Window) {
	for {
		ch := connection.KeyVerifyChallengeChan()
		if ch == nil {
			time.Sleep(500 * time.Millisecond)
			continue
		}
		for data := range ch {
			var payload wstypes.KeyVerifyChallengePayload
			if err := json.Unmarshal(data, &payload); err != nil {
				clientlog.Error("keyVerifyChallengeLoop: unmarshal: %v", err)
				continue
			}
			fromUsername := payload.FromUsername
			sessionID := payload.SessionID
			fyne.Do(func() {
				ShowVerificationRequestPopup(fromUsername, sessionID, w)
			})
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// messageReceiveLoop reads incoming room messages, decrypts them, and appends
// them to the ChatPanel. Restarts if the channel closes (reconnect).
func messageReceiveLoop(cp *ChatPanel, w fyne.Window) {
	for {
		ch := connection.RoomMessageChan()
		if ch == nil {
			time.Sleep(500 * time.Millisecond)
			continue
		}
		for data := range ch {
			var payload wstypes.RoomMessagePayload
			if err := json.Unmarshal(data, &payload); err != nil {
				clientlog.Error("messageReceiveLoop: unmarshal: %v", err)
				continue
			}

			chain, err := connection.GetOrCreateRoomKeyChain()
			if err != nil {
				clientlog.Error("messageReceiveLoop: get key chain: %v", err)
				continue
			}

			ct, err := base64.StdEncoding.DecodeString(payload.EncryptedText)
			if err != nil {
				clientlog.Error("messageReceiveLoop: decode ciphertext: %v", err)
				continue
			}

			plaintext, err := chain.DecryptMessageWithRoomKey(ct, payload.FromUsername, &payload.SenderMessageCount)
			if err != nil {
				clientlog.Error("messageReceiveLoop: decrypt: %v", err)
				continue
			}

			myUserID := connection.GetSessionUserID()
			kind := msgChat
			if payload.FromUsername == myUserID {
				kind = msgSelf
			}
			m := chatMessage{
				kind:      kind,
				timestamp: time.Now().Format("15:04"),
				date:      time.Now().Format("2006-01-02"),
				from:      payload.FromUsername,
				body:      string(plaintext),
			}

			appendAndPlay := func() {
				cp.AppendMessage(m)
				sounds.PlayMessageReceived()
			}
			fyne.Do(appendAndPlay)
		}
		// Channel was closed; brief pause before re-checking.
		time.Sleep(500 * time.Millisecond)
	}
}

// dmReceiveLoop reads incoming DMs and dispatches them to the ChatPanel.
func dmReceiveLoop(cp *ChatPanel, w fyne.Window) {
	type decryptedDM struct {
		FromUsername string `json:"fromUsername"`
		PlainText    string `json:"plainText"`
	}

	for {
		ch := connection.DMChan()
		if ch == nil {
			time.Sleep(500 * time.Millisecond)
			continue
		}
		for data := range ch {
			var dm decryptedDM
			if err := json.Unmarshal(data, &dm); err != nil {
				clientlog.Error("dmReceiveLoop: unmarshal: %v", err)
				continue
			}
			m := chatMessage{
				kind:      msgDM,
				timestamp: time.Now().Format("15:04"),
				date:      time.Now().Format("2006-01-02"),
				from:      dm.FromUsername,
				body:      dm.PlainText,
			}
			fyne.Do(func() { cp.AppendDMMessage(dm.FromUsername, m) })
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// notifLoop reads server-push notifications and shows toasts for relevant ones.
func notifLoop(cp *ChatPanel, sp *StatusPanel, w fyne.Window) {
	for {
		ch := connection.NotifChan()
		if ch == nil {
			time.Sleep(500 * time.Millisecond)
			continue
		}
		for data := range ch {
			var notif wstypes.NotificationPayload
			if err := json.Unmarshal(data, &notif); err != nil {
				continue
			}
			switch notif.Type {
			case wstypes.NotificationTypeUserAddedToRoom:
				clientlog.Info("received room invite")
				fyne.Do(func() {
					showToast(w, "you were invited to a room", toastInfo)
				})
				// Refresh the invite list.
				go func() {
					invites, err := connection.ListPendingInvites()
					if err == nil {
						fyne.Do(func() { sp.SetInvites(invites) })
					}
				}()

			case wstypes.NotificationTypeUserJoinedRoom:
				username := notif.Username
				clientlog.Info(username + " joined the room")
				sounds.PlayEnter()
				fyne.Do(func() {
					if username != "" {
						showToast(w, username+" joined the room", toastInfo)
					}
				})
				go func() {
					var roomID string
					fyne.DoAndWait(func() { roomID = cp.currentRoomID })
					if roomID == "" {
						return
					}
					members, err := connection.ListRoomMembers(roomID)
					if err == nil {
						fyne.Do(func() { cp.SetMembers(members) })
					}
				}()

			case wstypes.NotificationTypeUserLeftRoom:
				username := notif.Username
				clientlog.Info(username + " left the room")
				sounds.PlayExit()
				fyne.Do(func() {
					if username != "" {
						showToast(w, username+" left the room", toastInfo)
					}
				})
				go func() {
					var roomID string
					fyne.DoAndWait(func() { roomID = cp.currentRoomID })
					if roomID == "" {
						return
					}
					members, err := connection.ListRoomMembers(roomID)
					if err == nil {
						fyne.Do(func() { cp.SetMembers(members) })
					}
				}()

			case wstypes.NotificationTypeUserJoinedVoiceChat:
				username := notif.Username
				clientlog.Info(username + " joined sounds chat")
				sounds.PlayEnter()
				fyne.Do(func() {
					if username != "" {
						showToast(w, username+" joined sounds", toastInfo)
					}
				})

			case wstypes.NotificationTypeUserLeftVoiceChat:
				username := notif.Username
				clientlog.Info(username + " left sounds chat")
				sounds.PlayExit()
				fyne.Do(func() {
					if username != "" {
						showToast(w, username+" left sounds", toastInfo)
					}
				})

			case wstypes.NotificationTypeRoomKeyRotated:
				clientlog.Warn("room key rotated")
				fyne.Do(func() {
					showToast(w, "room key rotated", toastInfo)
				})

			case wstypes.NotificationTypeSoundClip:
				// Broker echoes this to all room members (including sender) so
				// everyone hears the broadcast at the same time.
				if payload, ok := decodeSoundClipPayload(notif); ok {
					clientlog.Info("soundclip from " + payload.FromUsername + ": " + payload.AudioURL)
					track := payload.Track
					audioURL := payload.AudioURL
					go startTrackLocal(track, audioURL)
				}
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// roomPollLoop periodically refreshes the room list and pending invites.
func roomPollLoop(cp *ChatPanel, sp *StatusPanel) {
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()

	// Initial fetch right away.
	doRefresh(cp, sp)

	for range tick.C {
		doRefresh(cp, sp)
	}
}

func doRefresh(cp *ChatPanel, sp *StatusPanel) {
	rooms, err := connection.ListRooms()
	if err == nil {
		fyne.Do(func() { cp.SetRooms(rooms) })
	}

	invites, err := connection.ListPendingInvites()
	if err == nil {
		fyne.Do(func() { sp.SetInvites(invites) })
	}

}
