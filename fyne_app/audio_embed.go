// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package main

import (
	"bytes"
	"image/color"
	"io"
	"net/http"
	"net/url"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

const audioCardW float32 = 280

// ── URL detection ─────────────────────────────────────────────────────────────

func isAudioURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	low := strings.ToLower(u.Path)
	return strings.HasSuffix(low, ".mp3") ||
		strings.HasSuffix(low, ".m3u") ||
		strings.HasSuffix(low, ".m3u8")
}

func findAudioURLs(text string) []string {
	var found []string
	for _, word := range strings.Fields(text) {
		if isAudioURL(word) {
			found = append(found, word)
		}
	}
	return found
}

// ── Metadata ──────────────────────────────────────────────────────────────────

type audioMeta struct {
	Title     string
	Artist    string
	Album     string
	ThumbData []byte
}

// titleFromURL extracts a human-readable title from the URL filename.
func titleFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	parts := strings.Split(u.Path, "/")
	filename := parts[len(parts)-1]
	if idx := strings.LastIndex(filename, "."); idx > 0 {
		filename = filename[:idx]
	}
	if dec, err := url.QueryUnescape(filename); err == nil {
		filename = dec
	}
	filename = strings.ReplaceAll(filename, "_", " ")
	return strings.TrimSpace(filename)
}

func fetchAudioMeta(rawURL string) audioMeta {
	low := strings.ToLower(rawURL)
	if strings.HasSuffix(low, ".m3u") || strings.HasSuffix(low, ".m3u8") {
		return fetchM3UMeta(rawURL)
	}
	return fetchMP3Meta(rawURL)
}

func fetchMP3Meta(rawURL string) audioMeta {
	req, err := http.NewRequest("GET", rawURL, nil) //nolint:noctx
	if err != nil {
		return audioMeta{}
	}
	req.Header.Set("Range", "bytes=0-262143") // 256 KB — enough for ID3 tags + album art
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return audioMeta{}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil || len(data) < 10 {
		return audioMeta{}
	}
	return parseID3v2(data)
}

func fetchM3UMeta(rawURL string) audioMeta {
	resp, err := http.Get(rawURL) //nolint:noctx
	if err != nil {
		return audioMeta{}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return audioMeta{}
	}
	meta := audioMeta{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "#EXTINF:") {
			continue
		}
		rest := strings.TrimPrefix(line, "#EXTINF:")
		if idx := strings.Index(rest, ","); idx >= 0 {
			info := rest[idx+1:]
			if d := strings.Index(info, " - "); d >= 0 {
				meta.Artist = strings.TrimSpace(info[:d])
				meta.Title = strings.TrimSpace(info[d+3:])
			} else {
				meta.Title = strings.TrimSpace(info)
			}
		}
		break
	}
	return meta
}

// resolvePlayURL returns the actual audio URL to stream.
// For M3U playlists it fetches and returns the first track URL.
func resolvePlayURL(rawURL string) string {
	low := strings.ToLower(rawURL)
	if !strings.HasSuffix(low, ".m3u") && !strings.HasSuffix(low, ".m3u8") {
		return rawURL
	}
	resp, err := http.Get(rawURL) //nolint:noctx
	if err != nil {
		return rawURL
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return rawURL
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			return line
		}
	}
	return rawURL
}

// ── ID3v2 parser ──────────────────────────────────────────────────────────────

func id3Syncsafe(b []byte) int {
	return int(b[0])<<21 | int(b[1])<<14 | int(b[2])<<7 | int(b[3])
}

func parseID3v2(data []byte) audioMeta {
	if len(data) < 10 || string(data[:3]) != "ID3" {
		return audioMeta{}
	}
	majorVer := int(data[3])
	flags := data[5]
	tagSize := id3Syncsafe(data[6:10]) + 10
	if tagSize > len(data) {
		tagSize = len(data)
	}

	pos := 10
	// Skip extended header (v2.3/v2.4)
	if flags&0x40 != 0 && majorVer >= 3 {
		if pos+4 > tagSize {
			return audioMeta{}
		}
		var extSize int
		if majorVer == 4 {
			extSize = id3Syncsafe(data[pos : pos+4])
		} else {
			extSize = int(data[pos])<<24 | int(data[pos+1])<<16 | int(data[pos+2])<<8 | int(data[pos+3])
		}
		pos += extSize
	}

	meta := audioMeta{}
	for pos+10 <= tagSize {
		if data[pos] == 0 {
			break // padding
		}
		frameID := string(data[pos : pos+4])
		var frameSize int
		if majorVer == 4 {
			frameSize = id3Syncsafe(data[pos+4 : pos+8])
		} else {
			frameSize = int(data[pos+4])<<24 | int(data[pos+5])<<16 | int(data[pos+6])<<8 | int(data[pos+7])
		}
		pos += 10 // advance past 4-char ID + 4-byte size + 2-byte flags
		if frameSize <= 0 || pos+frameSize > tagSize {
			break
		}
		fd := data[pos : pos+frameSize]
		pos += frameSize

		switch frameID {
		case "TIT2":
			meta.Title = id3Text(fd)
		case "TPE1":
			meta.Artist = id3Text(fd)
		case "TALB":
			meta.Album = id3Text(fd)
		case "APIC":
			meta.ThumbData = id3APIC(fd)
		}
	}
	return meta
}

func id3Text(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	enc := data[0]
	text := data[1:]
	switch enc {
	case 0: // ISO-8859-1
		r := make([]rune, 0, len(text))
		for _, b := range text {
			if b == 0 {
				break
			}
			r = append(r, rune(b))
		}
		return strings.TrimSpace(string(r))
	case 1: // UTF-16 with BOM
		if len(text) < 2 {
			return ""
		}
		bigEndian := true
		start := 0
		if text[0] == 0xff && text[1] == 0xfe {
			bigEndian = false
			start = 2
		} else if text[0] == 0xfe && text[1] == 0xff {
			start = 2
		}
		return id3UTF16(text[start:], bigEndian)
	case 3: // UTF-8
		s := string(text)
		if i := strings.IndexByte(s, 0); i >= 0 {
			s = s[:i]
		}
		return strings.TrimSpace(s)
	default:
		s := string(text)
		if i := strings.IndexByte(s, 0); i >= 0 {
			s = s[:i]
		}
		return strings.TrimSpace(s)
	}
}

func id3UTF16(data []byte, bigEndian bool) string {
	if len(data)%2 != 0 {
		data = data[:len(data)-1]
	}
	runes := make([]rune, 0, len(data)/2)
	for i := 0; i+1 < len(data); i += 2 {
		var c uint16
		if bigEndian {
			c = uint16(data[i])<<8 | uint16(data[i+1])
		} else {
			c = uint16(data[i]) | uint16(data[i+1])<<8
		}
		if c == 0 {
			break
		}
		runes = append(runes, rune(c))
	}
	return strings.TrimSpace(string(runes))
}

func id3APIC(data []byte) []byte {
	if len(data) < 3 {
		return nil
	}
	enc := data[0]
	pos := 1
	// Skip MIME type (null-terminated ASCII)
	for pos < len(data) && data[pos] != 0 {
		pos++
	}
	pos++ // skip null terminator
	if pos >= len(data) {
		return nil
	}
	pos++ // skip picture type byte
	// Skip description (encoding-dependent null terminator)
	if enc == 1 || enc == 2 { // UTF-16: double null
		for pos+1 < len(data) {
			if data[pos] == 0 && data[pos+1] == 0 {
				pos += 2
				break
			}
			pos += 2
		}
	} else {
		for pos < len(data) && data[pos] != 0 {
			pos++
		}
		pos++
	}
	if pos >= len(data) {
		return nil
	}
	return data[pos:]
}

// ── buildAudioEmbed ───────────────────────────────────────────────────────────

// buildAudioEmbed returns an audio player card for an mp3 or m3u URL.
// Metadata (title, artist, album art) loads asynchronously via ID3v2.
// The play button streams track 0; the alias button saves the URL.
func buildAudioEmbed(audioURL string) fyne.CanvasObject {
	// ── Thumb / icon ──────────────────────────────────────────────────────────
	thumbBg := canvas.NewRectangle(colAccentDark)
	thumbBg.CornerRadius = 6
	thumbBg.SetMinSize(fyne.NewSize(48, 48))
	noteIcon := txt("♪", color.White, 22, true, false)
	thumbStack := container.NewStack(thumbBg, container.NewCenter(noteIcon))

	// ── Labels ────────────────────────────────────────────────────────────────
	titleLabel := widget.NewLabel("Loading…")
	titleLabel.Truncation = fyne.TextTruncateEllipsis

	subtitleLabel := widget.NewLabel("")
	subtitleLabel.Truncation = fyne.TextTruncateEllipsis
	subtitleLabel.Importance = widget.LowImportance

	// ── Play / stop button ────────────────────────────────────────────────────
	isThisPlaying := func() bool {
		return isTrackPlaying(0) && getTrackURL(0) == audioURL
	}
	playBtnIcon := func() string {
		if isThisPlaying() {
			return "■"
		}
		return "▶"
	}

	var playBtn *PillPlayBtn
	playBtn = newPillPlayBtn(playBtnIcon(), func() {
		if isThisPlaying() {
			stopTrack(0)
		} else {
			go func() {
				resolved := resolvePlayURL(audioURL)
				startTrackLocal(0, resolved)
			}()
		}
		playBtn.SetIcon(playBtnIcon())
	})

	// ── Alias button ──────────────────────────────────────────────────────────
	aliasBtn := newBtn("+ Alias", func() {
		name := titleLabel.Text
		if name == "Loading…" || name == "" {
			name = titleFromURL(audioURL)
		}
		if appWindow != nil {
			showAddAliasPopupPrefilled(appWindow, name, audioURL, nil)
		}
	})
	aliasBtn.Importance = widget.LowImportance

	// ── Card shell ────────────────────────────────────────────────────────────
	bg := canvas.NewRectangle(colSurface2)
	bg.CornerRadius = 8
	bg.StrokeColor = colBorder
	bg.StrokeWidth = 1
	bg.SetMinSize(fyne.NewSize(audioCardW, 0))

	audioBadge := txt("♫  audio", colDimMid, 10, true, true)

	rightCol := container.NewVBox(
		titleLabel,
		subtitleLabel,
		vspace(2),
		container.NewHBox(playBtn, aliasBtn),
		vspace(2),
		audioBadge,
	)

	inner := container.NewBorder(nil, nil,
		container.NewPadded(thumbStack), nil,
		container.NewPadded(rightCol),
	)

	card := container.NewStack(bg, container.NewPadded(inner))

	// ── Keep play button in sync with track state ─────────────────────────────
	registerTrackWatcher(func() {
		playBtn.SetIcon(playBtnIcon())
	})

	// ── Async metadata fetch ──────────────────────────────────────────────────
	go func() {
		meta := fetchAudioMeta(audioURL)
		fyne.Do(func() {
			title := meta.Title
			if title == "" {
				title = titleFromURL(audioURL)
			}
			titleLabel.SetText(title)

			switch {
			case meta.Artist != "" && meta.Album != "":
				subtitleLabel.SetText(meta.Artist + "  ·  " + meta.Album)
			case meta.Artist != "":
				subtitleLabel.SetText(meta.Artist)
			case meta.Album != "":
				subtitleLabel.SetText(meta.Album)
			}

			if len(meta.ThumbData) > 0 {
				img := canvas.NewImageFromReader(bytes.NewReader(meta.ThumbData), "thumb_"+audioURL+".jpg")
				img.FillMode = canvas.ImageFillContain
				img.SetMinSize(fyne.NewSize(48, 48))
				thumbStack.Objects = []fyne.CanvasObject{img}
				thumbStack.Refresh()
			}
		})
	}()

	return container.NewHBox(container.NewPadded(card))
}
