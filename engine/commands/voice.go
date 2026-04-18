// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/srschreiber/nito-client/engine/connection"
	"github.com/srschreiber/nito-client/engine/sounds"
	wstypes "github.com/srschreiber/nito-client/shared/websocket_types"
	"github.com/srschreiber/nito-client/ui/clientlog"
)

func voiceJoinCmd() (string, error) {
	roomID := connection.GetSessionRoomID()
	if roomID == nil {
		return "", errors.New("sounds-join: no room selected (use room-select first)")
	}
	if err := sounds.Join(*roomID); err != nil {
		return "", fmt.Errorf("sounds-join: %w", err)
	}
	return "joined sounds in room " + *roomID, nil
}

func voiceLeaveCmd() (string, error) {
	roomID := connection.GetSessionRoomID()
	if roomID == nil {
		return "", errors.New("sounds-leave: no room selected")
	}
	if err := sounds.Leave(*roomID); err != nil {
		return "", fmt.Errorf("sounds-leave: %w", err)
	}
	return "left sounds in room " + *roomID, nil
}

// VoiceJoinDirect joins sounds in the currently selected room.
func VoiceJoinDirect() error {
	_, err := voiceJoinCmd()
	return err
}

// VoiceLeaveDirect leaves the active sounds session.
func VoiceLeaveDirect() error {
	_, err := voiceLeaveCmd()
	return err
}

// VoiceTestAudioDirect starts a loopback audio test (roomID="self").
func VoiceTestAudioDirect() error {
	return sounds.JoinSelf()
}

// VoiceLeaveTestAudioDirect stops the loopback test audio session.
func VoiceLeaveTestAudioDirect() error {
	return sounds.Leave(sounds.SelfRoomID)
}

// PlayAudioDirect sends a play_audio RPC to the broker for all users in the active sounds room.
// track must be 0–2; it is broadcast to all receivers so they play on the same track.
func PlayAudioDirect(audioURL string, track int) error {
	_, err := playCmd(audioURL, track)
	return err
}

func playCmd(audioURL string, track int) (string, error) {
	clientlog.Info("play_audio RPC SEND: url=%s track=%d", audioURL, track)
	roomID := sounds.ActiveRoomID()
	if roomID == "" {
		return "", errors.New("play: not in a sounds call (use sounds-join first)")
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
