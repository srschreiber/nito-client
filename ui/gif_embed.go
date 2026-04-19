// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package main

// GIF URL detection + inline embed. Chat messages are plain text on the wire;
// any URL that looks like a GIF (either .gif path or a GIPHY domain) gets
// rendered with an animated player so every peer sees the same thing the
// sender saw.

import (
	"io"
	"net/http"
	"net/url"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	xwidget "fyne.io/x/fyne/widget"
)

const gifEmbedMaxW float32 = 280
const gifEmbedMaxH float32 = 280

// isGifURL is true for http(s) URLs that look like a GIF resource. Accepts
// either a .gif path suffix or a giphy.com host (GIPHY media URLs aren't
// always .gif-suffixed but are always on media*.giphy.com).
func isGifURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	if strings.HasSuffix(strings.ToLower(u.Path), ".gif") {
		return true
	}
	if strings.Contains(strings.ToLower(u.Host), "giphy.com") {
		return true
	}
	return false
}

// findGifURLs returns the GIF URLs in text, in order.
func findGifURLs(text string) []string {
	var found []string
	for _, word := range strings.Fields(text) {
		if isGifURL(word) {
			found = append(found, word)
		}
	}
	return found
}

// buildGifEmbed returns an inline animated-GIF player for gifURL. The fetch
// runs in a goroutine and swaps the loading placeholder for the real widget
// when bytes arrive, so rendering isn't blocked on network.
func buildGifEmbed(gifURL string) fyne.CanvasObject {
	placeholder := container.NewCenter(monoTxt("loading gif…", liveDim))
	wrapper := container.NewStack(placeholder)

	go func() {
		resp, err := http.Get(gifURL)
		if err != nil {
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return
		}
		// Cap download size — animated GIFs can be surprisingly large and we
		// render them inline in chat so 4MB is a reasonable upper bound.
		data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		if err != nil {
			return
		}
		res := fyne.NewStaticResource("chat.gif", data)
		gif, err := xwidget.NewAnimatedGifFromResource(res)
		if err != nil {
			return
		}
		gif.SetMinSize(fyne.NewSize(gifEmbedMaxW, gifEmbedMaxH))
		gif.Start()

		// Border + rounded background to match the rest of the chat embeds.
		bg := canvas.NewRectangle(liveSurface2)
		bg.CornerRadius = 6
		bg.StrokeColor = liveBorder
		bg.StrokeWidth = 1
		card := container.NewStack(bg, container.NewPadded(gif))

		fyne.Do(func() {
			wrapper.Objects = []fyne.CanvasObject{card}
			wrapper.Refresh()
		})
	}()

	return wrapper
}
