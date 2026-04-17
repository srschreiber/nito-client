// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package main

import (
	"bytes"
	"encoding/json"
	"image/color"
	"io"
	"net/http"
	"net/url"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

const (
	ytCardW float32 = 280
	ytCardH float32 = ytCardW * 9 / 16 // 16:9 → 157.5
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

// ytOEmbedResp is the subset of the YouTube oEmbed response we care about.
type ytOEmbedResp struct {
	Title      string `json:"title"`
	AuthorName string `json:"author_name"`
}

// fetchYTOEmbed fetches oEmbed metadata for videoURL. Returns zero value on error.
func fetchYTOEmbed(videoURL string) ytOEmbedResp {
	apiURL := "https://www.youtube.com/oembed?url=" + url.QueryEscape(videoURL) + "&format=json"
	resp, err := http.Get(apiURL) //nolint:noctx
	if err != nil {
		return ytOEmbedResp{}
	}
	defer resp.Body.Close()
	var meta ytOEmbedResp
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return ytOEmbedResp{}
	}
	return meta
}

// ── ytCardWidget ──────────────────────────────────────────────────────────────

// ytCardWidget is a clickable, hoverable YouTube embed card. Tapping opens the
// video URL in the system browser; hovering highlights the card background.
type ytCardWidget struct {
	widget.BaseWidget
	bg       *canvas.Rectangle
	inner    fyne.CanvasObject
	videoURL string
}

func newYTCardWidget(bg *canvas.Rectangle, inner fyne.CanvasObject, videoURL string) *ytCardWidget {
	c := &ytCardWidget{bg: bg, inner: inner, videoURL: videoURL}
	c.ExtendBaseWidget(c)
	return c
}

func (c *ytCardWidget) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(container.NewStack(c.bg, c.inner))
}

func (c *ytCardWidget) Tapped(_ *fyne.PointEvent) {
	u, err := url.Parse(c.videoURL)
	if err == nil {
		_ = fyne.CurrentApp().OpenURL(u)
	}
}

func (c *ytCardWidget) TappedSecondary(_ *fyne.PointEvent) {}

func (c *ytCardWidget) MouseIn(_ *desktop.MouseEvent) {
	c.bg.FillColor = liveHover
	c.bg.Refresh()
}

func (c *ytCardWidget) MouseMoved(_ *desktop.MouseEvent) {}

func (c *ytCardWidget) MouseOut() {
	c.bg.FillColor = liveSurface2
	c.bg.Refresh()
}

func (c *ytCardWidget) Cursor() desktop.Cursor { return desktop.PointerCursor }

// ── buildYouTubeEmbed ─────────────────────────────────────────────────────────

// buildYouTubeEmbed returns a YouTube-style card for a video URL.
// The card is fixed-width (ytCardW); text labels truncate with "…" if too long.
// Clicking anywhere on the card opens the video in the system browser.
func buildYouTubeEmbed(videoURL string) fyne.CanvasObject {
	videoID := extractYouTubeID(videoURL)
	if videoID == "" {
		return nil
	}

	// ── Thumbnail ─────────────────────────────────────────────────────────────
	thumbPlaceholder := canvas.NewRectangle(liveSurface)
	thumbPlaceholder.SetMinSize(fyne.NewSize(ytCardW, ytCardH))
	thumbStack := container.NewStack(thumbPlaceholder)

	// ── Channel avatar circle ─────────────────────────────────────────────────
	avatarBg := canvas.NewRectangle(liveAccentDark)
	avatarBg.CornerRadius = 14
	avatarBg.SetMinSize(fyne.NewSize(28, 28))
	avatarInitial := txt("?", color.White, 12, true, false)
	avatarWidget := container.NewStack(
		avatarBg,
		container.NewCenter(avatarInitial),
	)

	// ── Text labels (widget.Label so truncation works) ────────────────────────
	titleLabel := widget.NewLabel("Loading…")
	titleLabel.Truncation = fyne.TextTruncateEllipsis

	channelLabel := widget.NewLabel("")
	channelLabel.Truncation = fyne.TextTruncateEllipsis
	channelLabel.Importance = widget.LowImportance

	ytRed := color.NRGBA{R: 0xff, G: 0x40, B: 0x40, A: 0xff}
	ytBadge := txt("▶ YouTube", ytRed, 10, true, true)

	metaRow := container.NewBorder(nil, nil, avatarWidget, nil,
		container.NewPadded(container.NewVBox(titleLabel, channelLabel)),
	)

	// ── Card shell ────────────────────────────────────────────────────────────
	bg := canvas.NewRectangle(liveSurface2)
	bg.CornerRadius = 8
	bg.StrokeColor = liveBorder
	bg.StrokeWidth = 1

	bottomSection := container.NewVBox(
		vspace(4),
		container.NewPadded(metaRow),
		container.NewPadded(ytBadge),
		vspace(2),
	)

	card := newYTCardWidget(bg, container.NewVBox(thumbStack, bottomSection), videoURL)

	// ── Async thumbnail ───────────────────────────────────────────────────────
	go func() {
		thumbURL := "https://img.youtube.com/vi/" + videoID + "/hqdefault.jpg"
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
		fyneImg.SetMinSize(fyne.NewSize(ytCardW, ytCardH))
		fyne.Do(func() {
			thumbStack.Objects = []fyne.CanvasObject{fyneImg}
			thumbStack.Refresh()
		})
	}()

	// ── Async oEmbed metadata ─────────────────────────────────────────────────
	go func() {
		meta := fetchYTOEmbed(videoURL)
		if meta.Title == "" && meta.AuthorName == "" {
			fyne.Do(func() { titleLabel.SetText(videoID) })
			return
		}
		fyne.Do(func() {
			if meta.Title != "" {
				titleLabel.SetText(meta.Title)
			}
			if meta.AuthorName != "" {
				channelLabel.SetText(meta.AuthorName)
				initial := string([]rune(meta.AuthorName)[0])
				avatarInitial.Text = strings.ToUpper(initial)
				avatarInitial.Refresh()
			}
		})
	}()

	return container.NewHBox(container.NewPadded(card))
}
