// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package components

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"time"

	tea "charm.land/bubbletea/v2"
	mp3 "github.com/hajimehoshi/go-mp3"
	"github.com/srschreiber/nito-client/shellapp/clientlog"
	"github.com/srschreiber/nito-client/shellapp/voice"
)

const audioMaxBytes = 5 << 20 // 5 MB

// PlayAudioFromURL returns a tea.Cmd that streams and plays the MP3 at audioURL,
// but only if the user is currently in a voice call in roomID.
// ctx can be cancelled to abort the download or stop playback early.
func PlayAudioFromURL(ctx context.Context, roomID, audioURL string) tea.Cmd {
	return func() tea.Msg {
		if voice.ActiveRoomID() != roomID {
			return nil
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, audioURL, nil)
		if err != nil {
			return audioErr("build request", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil // cancelled, not an error
			}
			return audioErr("fetch", err)
		}
		defer resp.Body.Close()

		data, err := io.ReadAll(io.LimitReader(resp.Body, audioMaxBytes))
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return audioErr("read body", err)
		}

		otoCtx, err := voice.GetOtoCtx()
		if err != nil {
			return audioErr("oto init", err)
		}

		dec, err := mp3.NewDecoder(bytes.NewReader(data))
		if err != nil {
			return audioErr("mp3 decode", err)
		}

		player := otoCtx.NewPlayer(dec)
		defer player.Close()
		player.Play()

		for {
			select {
			case <-ctx.Done():
				return nil
			default:
				if !player.IsPlaying() {
					return nil
				}
				time.Sleep(20 * time.Millisecond)
			}
		}
	}
}

func audioErr(op string, err error) tea.Msg {
	clientlog.Error("audio_player: %s: %v", op, err)
	return ShowToastMsg{Text: "audio: " + op + ": " + err.Error()}
}
