// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	wstypes "github.com/srschreiber/nito-client/shared/websocket_types"
	"github.com/srschreiber/nito-client/shellapp/connection"
	"github.com/srschreiber/nito-client/shellapp/voice"
)

func voiceJoinCmd() (string, error) {
	roomID := connection.GetSessionRoomID()
	if roomID == nil {
		return "", errors.New("voice-join: no room selected (use room-select first)")
	}
	if err := voice.Join(*roomID); err != nil {
		return "", fmt.Errorf("voice-join: %w", err)
	}
	return "joined voice in room " + *roomID, nil
}

func voiceLeaveCmd() (string, error) {
	roomID := connection.GetSessionRoomID()
	if roomID == nil {
		return "", errors.New("voice-leave: no room selected")
	}
	if err := voice.Leave(*roomID); err != nil {
		return "", fmt.Errorf("voice-leave: %w", err)
	}
	return "left voice in room " + *roomID, nil
}

// VoiceJoinDirect joins voice in the currently selected room.
func VoiceJoinDirect() error {
	_, err := voiceJoinCmd()
	return err
}

// VoiceLeaveDirect leaves the active voice session.
func VoiceLeaveDirect() error {
	_, err := voiceLeaveCmd()
	return err
}

// VoiceTestAudioDirect starts a loopback audio test (roomID="self").
func VoiceTestAudioDirect() error {
	return voice.JoinSelf()
}

// VoiceLeaveTestAudioDirect stops the loopback test audio session.
func VoiceLeaveTestAudioDirect() error {
	return voice.Leave(voice.SelfRoomID)
}

// PlayAudioDirect sends a play_audio RPC to the broker for all users in the active voice room.
// track must be 0–2; it is broadcast to all receivers so they play on the same track.
func PlayAudioDirect(audioURL string, track int) error {
	_, err := playCmd(audioURL, track)
	return err
}

func playCmd(audioURL string, track int) (string, error) {
	roomID := voice.ActiveRoomID()
	if roomID == "" {
		return "", errors.New("play: not in a voice call (use voice-join first)")
	}
	s := connection.CurrentSession()
	if s == nil {
		return "", errors.New("play: not connected")
	}
	payload, err := json.Marshal(wstypes.PlayAudioPayload{
		FromUsername: s.UserID,
		RoomID:       roomID,
		AudioURL:     audioURL,
		Track:        track,
	})
	if err != nil {
		return "", fmt.Errorf("play: marshal: %w", err)
	}
	msg := wstypes.ToBrokerWsMessage{
		RPCName:   wstypes.PlayAudio,
		RequestID: fmt.Sprintf("%d", time.Now().UnixNano()),
		UserID:    s.UserID,
		Nonce:     fmt.Sprintf("%d", time.Now().UnixNano()),
		Timestamp: time.Now().Unix(),
		Payload:   payload,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return "", fmt.Errorf("play: marshal message: %w", err)
	}
	if err := connection.Send(data); err != nil {
		return "", fmt.Errorf("play: send: %w", err)
	}
	return "playing " + audioURL, nil
}
