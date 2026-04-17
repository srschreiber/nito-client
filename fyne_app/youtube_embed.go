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

// extractYouTubeID returns the video ID from a YouTube URL, or "" if the URL
// is not a recognised YouTube link.
//
// Handles:
//   - https://www.youtube.com/watch?v=ID
//   - https://youtu.be/ID
//   - https://youtube.com/shorts/ID
func extractYouTubeID(raw string) string {
	if !strings.Contains(raw, "youtu") {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	host := strings.TrimPrefix(u.Host, "www.")
	switch host {
	case "youtube.com":
		if strings.HasPrefix(u.Path, "/shorts/") {
			id := strings.TrimPrefix(u.Path, "/shorts/")
			id = strings.Split(id, "/")[0]
			if isValidVideoID(id) {
				return id
			}
		}
		if id := u.Query().Get("v"); isValidVideoID(id) {
			return id
		}
	case "youtu.be":
		id := strings.TrimPrefix(u.Path, "/")
		id = strings.Split(id, "?")[0]
		if isValidVideoID(id) {
			return id
		}
	}
	return ""
}

func isValidVideoID(id string) bool {
	if len(id) < 5 || len(id) > 16 {
		return false
	}
	for _, c := range id {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// findYouTubeURLs returns all YouTube URLs found in a text string.
func findYouTubeURLs(text string) []string {
	var found []string
	for _, word := range strings.Fields(text) {
		if extractYouTubeID(word) != "" {
			found = append(found, word)
		}
	}
	return found
}

// buildYouTubeEmbed returns a compact embed card for a YouTube video.
// The thumbnail JPEG is loaded asynchronously from the YouTube CDN.
func buildYouTubeEmbed(videoURL string) fyne.CanvasObject {
	videoID := extractYouTubeID(videoURL)
	if videoID == "" {
		return nil
	}

	// ── Thumbnail placeholder ─────────────────────────────────────────────────
	placeholder := canvas.NewRectangle(color.NRGBA{R: 0x1a, G: 0x1a, B: 0x2e, A: 0xff})
	placeholder.SetMinSize(fyne.NewSize(160, 90))

	// We use a Stack so the placeholder and the loaded image occupy the same slot.
	thumbStack := container.NewStack(placeholder)

	// ── Info column ───────────────────────────────────────────────────────────
	ytRed := color.NRGBA{R: 0xff, G: 0x40, B: 0x40, A: 0xff}
	ytLabel := txt("▶ YouTube", ytRed, 11, false, true)
	openBtn := widget.NewButton("Open", func() {
		u, err := url.Parse(videoURL)
		if err == nil {
			_ = fyne.CurrentApp().OpenURL(u)
		}
	})
	openBtn.Importance = widget.LowImportance

	info := container.NewVBox(ytLabel, openBtn)

	// ── Card shell ────────────────────────────────────────────────────────────
	bg := canvas.NewRectangle(colSurface2)
	bg.CornerRadius = 6
	bg.StrokeColor = colBorder
	bg.StrokeWidth = 1

	card := container.NewStack(bg,
		container.NewPadded(container.NewHBox(thumbStack, container.NewPadded(info))),
	)

	// ── Async thumbnail fetch ─────────────────────────────────────────────────
	go func() {
		thumbURL := "https://img.youtube.com/vi/" + videoID + "/mqdefault.jpg"
		resp, err := http.Get(thumbURL) //nolint:noctx
		if err != nil {
			return
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(resp.Body)
		if err != nil || len(data) == 0 {
			return
		}
		fyneImg := canvas.NewImageFromReader(bytes.NewReader(data), "thumb_"+videoID+".jpg")
		fyneImg.FillMode = canvas.ImageFillContain
		fyneImg.SetMinSize(fyne.NewSize(160, 90))
		fyne.Do(func() {
			thumbStack.Objects = []fyne.CanvasObject{fyneImg}
			thumbStack.Refresh()
		})
	}()

	return container.NewPadded(card)
}
