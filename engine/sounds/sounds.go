// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

// Package sounds plays embedded UI sounds effects using the shared oto audio context.
package sounds

import (
	"bytes"
	_ "embed"
	"time"

	"github.com/hajimehoshi/go-mp3"
	"github.com/srschreiber/nito-client/ui/clientlog"
)

//go:embed enter.mp3
var enterMP3 []byte

//go:embed boop.mp3
var boopMP3 []byte

//go:embed exit.mp3
var exitMP3 []byte

//go:embed message_received.mp3
var messageReceived []byte

//go:embed voice_leave.mp3
var voiceLeave []byte

//go:embed voice_join.mp3
var voiceJoin []byte

func playSound(name string, data []byte) {
	go func() {
		ctx, err := GetOtoCtx()
		if err != nil {
			clientlog.Error("sounds: oto init: %v", err)
			return
		}
		dec, err := mp3.NewDecoder(bytes.NewReader(data))
		if err != nil {
			clientlog.Error("sounds: decode %s: %v", name, err)
			return
		}
		player := ctx.NewPlayer(dec)
		player.SetVolume(EffectiveChatSFXVolume())
		player.Play()
		for player.IsPlaying() {
			time.Sleep(10 * time.Millisecond)
		}
		player.Close()
	}()
}

func PlayVoiceJoin() { playSound("voice_join.mp3", voiceJoin) }

func PlayVoiceLeave() { playSound("voice_leave.mp3", voiceLeave) }

// PlayEnter plays the room-entry notification sounds. Non-blocking.
func PlayEnter() { playSound("enter.mp3", enterMP3) }

// PlayExit plays the room-exit notification sounds. Non-blocking.
func PlayExit() { playSound("exit.mp3", exitMP3) }

func PlayMessageReceived() { playSound("message_received.mp3", messageReceived) }
