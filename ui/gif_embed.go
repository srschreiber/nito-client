// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package main

// GIF URL detection + inline embed. Chat messages are plain text on the wire;
// any URL that looks like a GIF (either .gif path or a GIPHY domain) gets
// rendered with an animated player so every peer sees the same thing the
// sender saw.

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	xwidget "fyne.io/x/fyne/widget"

	"github.com/srschreiber/nito-client/ui/clientlog"
)

// gifHTTPClient downloads embedded GIFs with a bounded timeout so a slow or
// hung media host can't leak goroutines indefinitely. GIFs are usually small
// enough that 20s is a comfortable upper bound.
var gifHTTPClient = &http.Client{Timeout: 20 * time.Second}

// ── Per-URL cache + singleflight for GIF downloads ────────────────────────────
//
// Without this, every buildGifEmbed call for the same URL fires its own HTTP
// request. Re-rendering chat history (on join, room switch, theme change)
// multiplies that across every message containing the URL. A single gif can
// easily end up downloaded 5+ times in a second. Cache keeps each URL's
// bytes after the first fetch; sync.Once ensures concurrent callers share
// one download instead of racing.

type gifCacheEntry struct {
	once sync.Once
	data []byte
	err  error
}

var (
	gifCacheMu sync.Mutex
	gifCache   = map[string]*gifCacheEntry{}
)

// fetchGifOnce downloads gifURL at most once per process. Subsequent calls
// return the cached bytes (or cached error) without hitting the network.
func fetchGifOnce(gifURL string) ([]byte, error) {
	gifCacheMu.Lock()
	e, ok := gifCache[gifURL]
	if !ok {
		e = &gifCacheEntry{}
		gifCache[gifURL] = e
	}
	gifCacheMu.Unlock()

	e.once.Do(func() {
		clientlog.Info("gif: fetching %s", gifURL)
		resp, err := gifHTTPClient.Get(gifURL)
		if err != nil {
			e.err = fmt.Errorf("http.Get: %w", err)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			e.err = fmt.Errorf("HTTP %d", resp.StatusCode)
			return
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		if err != nil {
			e.err = fmt.Errorf("read body: %w", err)
			return
		}
		clientlog.Info("gif: downloaded %d bytes from %s", len(data), gifURL)
		e.data = data
	})
	return e.data, e.err
}

const gifEmbedMaxW float32 = 280
const gifEmbedMaxH float32 = 280

// gifLoadedHook is invoked on the Fyne thread each time a GIF embed finishes
// loading its bytes and swaps in the animated widget. The ChatPanel sets this
// so it can re-scroll-to-bottom when the row grows — without a hook we'd have
// scrolled to the bottom of the placeholder, missing the actual GIF.
var gifLoadedHook func()

func setGifLoadedHook(fn func()) { gifLoadedHook = fn }

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

// isGifOnlyText is true if text (after trimming whitespace) is exactly one
// GIF URL with no other content. Used by the message renderer to suppress
// the URL text and show just the animated embed.
func isGifOnlyText(text string) bool {
	fields := strings.Fields(text)
	return len(fields) == 1 && isGifURL(fields[0])
}

// buildGifEmbed returns an inline, collapsible, left-justified animated-GIF
// player for gifURL. The fetch runs in a goroutine; the loading placeholder is
// swapped for the real widget when bytes arrive. Users can click the caret to
// collapse the GIF down to a compact pill and click again to re-expand.
func buildGifEmbed(gifURL string) fyne.CanvasObject {
	placeholder := container.NewHBox(container.NewPadded(monoTxt("loading gif…", liveDim)))
	// The outer wrapper is an HBox so the child takes its natural width and
	// sits at the left of the message row instead of stretching to fill.
	wrapper := container.NewHBox(placeholder)

	go func() {
		data, err := fetchGifOnce(gifURL)
		if err != nil {
			clientlog.Error("gif: fetch %s failed: %v", gifURL, err)
			return
		}
		res := fyne.NewStaticResource("chat.gif", data)
		gif, err := xwidget.NewAnimatedGifFromResource(res)
		if err != nil {
			clientlog.Error("gif: parse %s failed (%d bytes): %v", gifURL, len(data), err)
			return
		}
		gif.SetMinSize(fyne.NewSize(gifEmbedMaxW, gifEmbedMaxH))
		gif.Start()
		clientlog.Info("gif: rendering %s", gifURL)

		card := newCollapsibleGifCard(gif)

		fyne.Do(func() {
			wrapper.Objects = []fyne.CanvasObject{card}
			wrapper.Refresh()
			if gifLoadedHook != nil {
				gifLoadedHook()
			}
		})
	}()

	return wrapper
}

// newCollapsibleGifCard wraps an animated GIF in a card with a toggle that
// swaps between the full GIF and a compact "▶ GIF" pill. The whole card is
// sized to the content (via the outer HBox on the parent) so it stays left-
// justified regardless of the chat width.
func newCollapsibleGifCard(gif *xwidget.AnimatedGif) fyne.CanvasObject {
	bg := canvas.NewRectangle(liveSurface2)
	bg.CornerRadius = 6
	bg.StrokeColor = liveBorder
	bg.StrokeWidth = 1

	// The card holds both states; we Show/Hide to toggle instead of rebuilding.
	expandedBody := container.NewPadded(gif)
	collapsedLabel := monoTxt(" GIF (click to expand) ", liveDimMid)
	collapsedBody := container.NewPadded(collapsedLabel)
	collapsedBody.Hide()

	toggleIcon := monoTxt("▼", liveAccent)
	var toggle *Tappable
	toggle = NewTappable(container.NewPadded(toggleIcon), func() {
		if expandedBody.Visible() {
			expandedBody.Hide()
			collapsedBody.Show()
			toggleIcon.Text = "▶"
			gif.Stop()
		} else {
			collapsedBody.Hide()
			expandedBody.Show()
			toggleIcon.Text = "▼"
			gif.Start()
		}
		toggleIcon.Refresh()
		toggle.Refresh()
	})

	// Header row: [toggle ▼]  (nothing else — keeps the pill narrow).
	header := container.NewHBox(toggle)
	body := container.NewVBox(header, expandedBody, collapsedBody)
	return container.NewStack(bg, body)
}
