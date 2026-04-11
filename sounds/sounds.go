// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

// Package sounds plays embedded UI sound effects using the shared oto audio context.
package sounds

import (
	"bytes"
	_ "embed"

	"github.com/hajimehoshi/go-mp3"
	"github.com/srschreiber/nito-client/shellapp/clientlog"
	"github.com/srschreiber/nito-client/shellapp/voice"
)

//go:embed enter.mp3
var enterMP3 []byte

//go:embed exit.mp3
var exitMP3 []byte

func playSound(name string, data []byte) {
	go func() {
		ctx, err := voice.GetOtoCtx()
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
		defer player.Close()
		player.Play()
	}()
}

// PlayEnter plays the room-entry notification sound. Non-blocking.
func PlayEnter() { playSound("enter.mp3", enterMP3) }

// PlayExit plays the room-exit notification sound. Non-blocking.
func PlayExit() { playSound("exit.mp3", exitMP3) }
