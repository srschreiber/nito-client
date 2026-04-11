// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

// Package sounds plays embedded UI sound effects using the shared oto audio context.
package sounds

import (
	"bytes"
	_ "embed"

	"github.com/hajimehoshi/go-mp3"
	"github.com/srschreiber/nito-client/shellapp/voice"
)

//go:embed enter.mp3
var enterMP3 []byte

// PlayEnter plays the room-entry notification sound. Non-blocking.
func PlayEnter() {
	go func() {
		ctx, err := voice.GetOtoCtx()
		if err != nil {
			return
		}
		dec, err := mp3.NewDecoder(bytes.NewReader(enterMP3))
		if err != nil {
			return
		}
		player := ctx.NewPlayer(dec)
		defer player.Close()
		player.Play()
	}()
}
