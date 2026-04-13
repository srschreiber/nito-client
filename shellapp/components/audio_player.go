// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package components

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	mp3 "github.com/hajimehoshi/go-mp3"
	"github.com/srschreiber/nito-client/shellapp/clientlog"
	"github.com/srschreiber/nito-client/shellapp/voice"
)

// eofTracker wraps an io.ReadCloser and records when the underlying reader
// returns io.EOF. Used to distinguish a natural end-of-stream from a CoreAudio
// context suspension (which also causes oto's IsPlaying to return false).
type eofTracker struct {
	io.ReadCloser
	reached atomic.Bool
}

func (e *eofTracker) Read(p []byte) (int, error) {
	n, err := e.ReadCloser.Read(p)
	if err == io.EOF {
		e.reached.Store(true)
	}
	return n, err
}

// PlayAudioFromURL returns a tea.Cmd that streams and plays the MP3 (or M3U
// playlist) at audioURL on the given track slot, but only if the user is
// currently in a voice call in roomID. ctx can be cancelled to abort early.
// On natural completion or error it returns AudioTrackDoneMsg or
// AudioPlaybackErrorMsg so the caller can free the track slot; on
// cancellation it returns nil.
func PlayAudioFromURL(ctx context.Context, roomID, audioURL string, track int) tea.Cmd {
	return func() tea.Msg {
		if roomID != voice.SelfRoomID && voice.ActiveRoomID() != roomID {
			return AudioTrackDoneMsg{Track: track}
		}

		urls, err := resolveAudioURLs(ctx, audioURL)
		if err != nil {
			return audioPlaybackErr(track, "resolve", err)
		}

		for _, u := range urls {
			if ctx.Err() != nil {
				return nil
			}
			if msg := playOne(ctx, roomID, u, track); msg != nil {
				return msg
			}
			if ctx.Err() != nil {
				return nil
			}
		}
		return AudioTrackDoneMsg{Track: track}
	}
}

// resolveAudioURLs fetches audioURL and returns a slice of MP3 URLs to play.
// For a plain MP3 URL it returns a single-element slice. For an M3U/M3U8
// playlist it parses and returns all track URLs in order.
func resolveAudioURLs(ctx context.Context, audioURL string) ([]string, error) {
	if isM3U(audioURL) {
		return fetchAndParseM3U(ctx, audioURL)
	}
	// Check Content-Type in case the URL has no recognisable extension.
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, audioURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err == nil {
		resp.Body.Close()
		ct := resp.Header.Get("Content-Type")
		if strings.Contains(ct, "mpegurl") || strings.Contains(ct, "x-scpls") {
			return fetchAndParseM3U(ctx, audioURL)
		}
	}
	return []string{audioURL}, nil
}

// isM3U reports whether the URL path ends with a playlist extension.
func isM3U(u string) bool {
	lower := strings.ToLower(u)
	// Strip any query string before checking the extension.
	if i := strings.Index(lower, "?"); i != -1 {
		lower = lower[:i]
	}
	return strings.HasSuffix(lower, ".m3u") || strings.HasSuffix(lower, ".m3u8")
}

// fetchAndParseM3U downloads the playlist at url and returns all non-comment,
// non-empty lines as track URLs. Relative URLs are not resolved (archive.org
// and most public sources use absolute URLs).
func fetchAndParseM3U(ctx context.Context, url string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var tracks []string
	scanner := bufio.NewScanner(io.LimitReader(resp.Body, 1<<20)) // 1 MB max for playlist
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		tracks = append(tracks, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(tracks) == 0 {
		return nil, fmt.Errorf("no tracks found in playlist")
	}
	clientlog.Info("audio_player: m3u resolved %d track(s) from %s", len(tracks), url)
	return tracks, nil
}

// playOne streams and plays a single MP3 URL. The HTTP response body is piped
// directly to the MP3 decoder — no intermediate buffer, no size limit. Returns
// a non-nil tea.Msg on error, nil on clean finish or context cancellation.
// If roomID is voice.SelfRoomID the active-room guard is skipped so the user
// can play audio locally without being in a voice call.
func playOne(ctx context.Context, roomID, audioURL string, track int) tea.Msg {
	if roomID != voice.SelfRoomID && voice.ActiveRoomID() != roomID {
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, audioURL, nil)
	if err != nil {
		return audioPlaybackErr(track, "build request", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return audioPlaybackErr(track, "fetch", err)
	}
	body := &eofTracker{ReadCloser: resp.Body}
	defer body.Close()

	otoCtx, err := voice.GetOtoCtx()
	if err != nil {
		return audioPlaybackErr(track, "oto init", err)
	}

	dec, err := mp3.NewDecoder(body)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return audioPlaybackErr(track, "mp3 decode", err)
	}

	player := otoCtx.NewPlayer(dec)
	player.SetVolume(voice.EffectivePlaybackVolume())
	defer player.Close()
	player.Play()

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			if !player.IsPlaying() {
				// Only treat as end-of-stream if the HTTP body is exhausted.
				// If EOF hasn't been reached, CoreAudio suspended the player
				// (e.g. voice session started/stopped) — re-call Play() to resume.
				if body.reached.Load() {
					return nil
				}
				player.Play()
			}
			player.SetVolume(voice.EffectivePlaybackVolume())
			time.Sleep(20 * time.Millisecond)
		}
	}
}

func audioPlaybackErr(track int, op string, err error) AudioPlaybackErrorMsg {
	if err != nil {
		clientlog.Error("audio_player: %s: %v", op, err)
		return AudioPlaybackErrorMsg{Track: track, Text: "audio: " + op + ": " + err.Error()}
	}
	clientlog.Error("audio_player: %s: unknown error", op)
	return AudioPlaybackErrorMsg{Track: track, Text: "audio: " + op + ": unknown error"}
}
