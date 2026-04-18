// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

// Package sounds plays embedded UI sound effects using the shared oto audio context.
package sounds

import (
	"bytes"
	_ "embed"
	"time"

	"github.com/hajimehoshi/go-mp3"
	"github.com/srschreiber/nito-client/engine/voice"
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
		player.SetVolume(voice.EffectiveChatSFXVolume())
		player.Play()
		for player.IsPlaying() {
			time.Sleep(10 * time.Millisecond)
		}
		player.Close()
	}()
}

// PlayEnter plays the room-entry notification sound. Non-blocking.
func PlayEnter() { playSound("enter.mp3", enterMP3) }

// PlayExit plays the room-exit notification sound. Non-blocking.
func PlayExit() { playSound("exit.mp3", exitMP3) }

func PlayMessageReceived() { playSound("message_received.mp3", messageReceived) }

// PlayPreview plays the entry sound at an explicit volume [0,1]. Used by the
// Audio Settings sliders so the preview always reflects the slider being moved,
// regardless of the effective-volume override hierarchy.
func PlayPreview(vol float64) {
	go func() {
		ctx, err := voice.GetOtoCtx()
		if err != nil {
			clientlog.Error("sounds: oto init: %v", err)
			return
		}
		dec, err := mp3.NewDecoder(bytes.NewReader(boopMP3))
		if err != nil {
			clientlog.Error("sounds: decode preview: %v", err)
			return
		}
		player := ctx.NewPlayer(dec)
		player.SetVolume(vol)
		player.Play()
		for player.IsPlaying() {
			time.Sleep(10 * time.Millisecond)
		}
		player.Close()
	}()
}
