// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package commands

import (
	"errors"
	"fmt"

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
