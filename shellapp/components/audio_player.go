// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package components

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	mp3 "github.com/hajimehoshi/go-mp3"
	"github.com/srschreiber/nito-client/shellapp/clientlog"
	"github.com/srschreiber/nito-client/shellapp/voice"
)

const audioMaxBytes = 5 << 20 // 5 MB

// PlayAudioFromURL returns a tea.Cmd that streams and plays the MP3 (or M3U
// playlist) at audioURL, but only if the user is currently in a voice call in
// roomID. ctx can be cancelled to abort download or stop playback early.
func PlayAudioFromURL(ctx context.Context, roomID, audioURL string) tea.Cmd {
	return func() tea.Msg {
		if voice.ActiveRoomID() != roomID {
			return nil
		}

		urls, err := resolveAudioURLs(ctx, audioURL)
		if err != nil {
			return audioErr("resolve", err)
		}

		for _, u := range urls {
			if ctx.Err() != nil {
				return nil
			}
			if msg := playOne(ctx, roomID, u); msg != nil {
				return msg
			}
		}
		return nil
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

// playOne downloads and plays a single MP3 URL. Returns a non-nil tea.Msg on
// error, nil on clean finish or context cancellation.
func playOne(ctx context.Context, roomID, audioURL string) tea.Msg {
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
			return nil
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

func audioErr(op string, err error) tea.Msg {
	if err != nil {
		clientlog.Error("audio_player: %s: %v", op, err)
		return ShowToastMsg{Text: "audio: " + op + ": " + err.Error()}
	}
	clientlog.Error("audio_player: %s: no tracks found in playlist", op)
	return ShowToastMsg{Text: "audio: " + op + ": no tracks found in playlist"}
}
