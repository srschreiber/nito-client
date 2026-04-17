// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package main

import (
	"context"
	"image/color"
	"math"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/srschreiber/nito-client/shellapp/components"
	"github.com/srschreiber/nito-client/shellapp/voice"
)

// ── Package-level track cancel management ─────────────────────────────────────

var (
	trackMu    sync.Mutex
	trackState [3]struct {
		cancel context.CancelFunc
		gen    int // incremented each startTrackLocal; goroutine clears only its own gen
	}
)

func stopTrack(idx int) {
	trackMu.Lock()
	if trackState[idx].cancel != nil {
		trackState[idx].cancel()
		trackState[idx].cancel = nil
	}
	trackMu.Unlock()
}

func stopAllTracks() {
	for i := 0; i < 3; i++ {
		stopTrack(i)
	}
}

// startTrackLocal starts local playback on track idx. Does not broadcast.
// The SoundClip notif handler calls this; so does the "local only" play button.
func startTrackLocal(idx int, audioURL string) {
	stopTrack(idx)
	ctx, cancel := context.WithCancel(context.Background())
	trackMu.Lock()
	trackState[idx].cancel = cancel
	trackState[idx].gen++
	myGen := trackState[idx].gen
	trackMu.Unlock()

	go func() {
		fn := components.PlayAudioFromURL(ctx, voice.SelfRoomID, audioURL, idx)
		fn() // blocks until playback ends or ctx is cancelled
		trackMu.Lock()
		if trackState[idx].gen == myGen {
			trackState[idx].cancel = nil
		}
		trackMu.Unlock()
	}()
}

// isTrackPlaying reports whether track idx has an active cancel func.
func isTrackPlaying(idx int) bool {
	trackMu.Lock()
	defer trackMu.Unlock()
	return trackState[idx].cancel != nil
}

// ── TrackMeterWidget ──────────────────────────────────────────────────────────

const (
	meterBandCount = 6
	meterH         = 40.0
)

// TrackMeterWidget renders an animated N-band amplitude meter for one track.
// Call Tick() every 50 ms (on the Fyne thread) to animate it.
type TrackMeterWidget struct {
	widget.BaseWidget
	trackIdx int
	raster   *canvas.Raster
	tick     int
}

func NewTrackMeterWidget(trackIdx int) *TrackMeterWidget {
	m := &TrackMeterWidget{trackIdx: trackIdx}
	m.raster = canvas.NewRasterWithPixels(func(px, py, pw, ph int) color.Color {
		return m.pixelAt(px, py, pw, ph)
	})
	m.raster.SetMinSize(fyne.NewSize(float32(meterBandCount)*10, meterH))
	m.ExtendBaseWidget(m)
	return m
}

func (m *TrackMeterWidget) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(m.raster)
}

// Tick advances the animation and redraws. Call on the Fyne thread every 50 ms.
func (m *TrackMeterWidget) Tick() {
	m.tick++
	m.raster.Refresh()
}

func (m *TrackMeterWidget) pixelAt(px, py, pw, ph int) color.Color {
	n := meterBandCount
	if pw <= 0 || ph <= 0 {
		return colSurface
	}
	bandW := pw / n
	if bandW <= 0 {
		return colSurface
	}
	b := px / bandW
	if b >= n {
		return colSurface
	}
	if px%bandW == bandW-1 { // 1px gap between bands
		return colSurface
	}

	level := voice.GetTrackBandLevel(m.trackIdx, b)

	// Wobble adds ±8% visual animation even at sustained levels
	wobble := 0.08 * math.Sin(float64(time.Now().UnixMilli()%628)/100.0)
	scaled := math.Sqrt(float64(level)) + wobble
	if scaled < 0 {
		scaled = 0
	} else if scaled > 1 {
		scaled = 1
	}

	barH := int(scaled * float64(ph))
	if py < ph-barH {
		return colSurface
	}

	// Gradient: green (bottom) → amber (mid) → accent (top)
	posInBar := float32(ph-py) / float32(ph)
	switch {
	case posInBar > 0.75:
		return colAccent
	case posInBar > 0.45:
		return colAmber
	default:
		return colGreen
	}
}

// ── Live track row bundle ─────────────────────────────────────────────────────

// trackRowBundle holds the stable widget refs for one track row so we can
// update text/visibility each tick without rebuilding widgets.
type trackRowBundle struct {
	trackIdx    int
	idleLayer   *HoverRow
	activeLayer *fyne.Container
	statusLabel *canvas.Text
	meter       *TrackMeterWidget
	wrapper     *fyne.Container // Stack(idleLayer, activeLayer)
}

var spinnerFrames = []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}

// tick updates visibility and label content. Call on Fyne thread at 50 ms.
func (r *trackRowBundle) tick(tickN int) {
	playing := isTrackPlaying(r.trackIdx)
	if playing {
		r.idleLayer.Hide()
		r.activeLayer.Show()

		if voice.IsTrackBuffering(r.trackIdx) {
			frame := spinnerFrames[tickN%len(spinnerFrames)]
			r.statusLabel.Text = frame + " BUFFERING"
			r.statusLabel.Color = colAmber
		} else if voice.IsTrackLive(r.trackIdx) {
			if (tickN/5)%2 == 0 {
				r.statusLabel.Text = "◉ LIVE"
			} else {
				r.statusLabel.Text = "○ LIVE"
			}
			r.statusLabel.Color = color.NRGBA{R: 0xff, G: 0x60, B: 0x60, A: 0xff}
		} else {
			title := voice.GetTrackTitle(r.trackIdx)
			if title == "" {
				title = "playing…"
			}
			r.statusLabel.Text = title
			r.statusLabel.Color = colText
		}
		r.statusLabel.Refresh()
		r.meter.Tick()
	} else {
		r.idleLayer.Show()
		r.activeLayer.Hide()
	}
}

func newTrackRowBundle(idx int, w fyne.Window) *trackRowBundle {
	meter := NewTrackMeterWidget(idx)
	statusLabel := txt("", colText, 11, false, true)
	stopBtn := newCircleStop(func() {
		stopTrack(idx)
		nitoLog("stopped track " + itoa(idx))
	})

	noteIcon := txt("♪ ", colAccent, 11, false, true)
	activeLayer := container.NewPadded(container.NewHBox(
		stopBtn,
		container.NewPadded(noteIcon),
		meter,
		container.NewPadded(statusLabel),
	))

	idleLayer := NewHoverRow(
		container.NewPadded(container.NewHBox(
			txt("• ", colDimMid, 13, false, true),
			monoTxt("track "+itoa(idx+1)+" — idle", colDim),
		)),
		func() { showPlayPopup(w, idx) },
	)

	// activeLayer starts hidden; idleLayer shown by default
	activeLayer.Hide()
	wrapper := container.NewStack(idleLayer, activeLayer)

	return &trackRowBundle{
		trackIdx:    idx,
		idleLayer:   idleLayer,
		activeLayer: activeLayer,
		statusLabel: statusLabel,
		meter:       meter,
		wrapper:     wrapper,
	}
}

// ── Play popup ────────────────────────────────────────────────────────────────

func showPlayPopup(w fyne.Window, idx int) {
	aliases, _ := voice.ListAudioAliases()

	urlEntry := widget.NewEntry()
	urlEntry.SetPlaceHolder("https://...  or  archive.org url")

	// Build alias options (sorted by name for consistency)
	var aliasNames []string
	for name := range aliases {
		aliasNames = append(aliasNames, name)
	}

	var aliasSel *widget.Select
	if len(aliasNames) > 0 {
		aliasSel = widget.NewSelect(aliasNames, nil)
	}

	playBtn := widget.NewButton("Play", nil)
	playBtn.Importance = widget.HighImportance
	cancelBtn := widget.NewButton("Cancel", nil)
	cancelBtn.Importance = widget.LowImportance

	var pop *widget.PopUp
	playBtn.OnTapped = func() {
		target := strings.TrimSpace(urlEntry.Text)
		if target == "" && aliasSel != nil && aliasSel.Selected != "" {
			target = aliases[aliasSel.Selected]
		}
		if pop != nil {
			pop.Hide()
		}
		if target == "" {
			showToast(w, "no URL or alias selected", toastWarn)
			return
		}
		startTrackLocal(idx, target)
		nitoLog("play track " + itoa(idx) + ": " + target)
	}
	cancelBtn.OnTapped = func() {
		if pop != nil {
			pop.Hide()
		}
	}

	var bodyItems []fyne.CanvasObject
	bodyItems = append(bodyItems,
		monoTxt("URL", colDimMid), urlEntry, vspace(4),
	)
	if len(aliasNames) > 0 {
		bodyItems = append(bodyItems,
			monoTxt("or alias", colDimMid), aliasSel, vspace(4),
		)
	}
	bodyItems = append(bodyItems,
		container.NewHBox(playBtn, cancelBtn),
	)

	body := container.NewVBox(bodyItems...)
	pop = showNitoPopup("TRACK "+itoa(idx+1), body, w)
	w.Canvas().Focus(urlEntry)
}

// ── Alias management popup ────────────────────────────────────────────────────

func showAddAliasPopup(w fyne.Window, onAdded func()) {
	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("alias name")
	urlEntry := widget.NewEntry()
	urlEntry.SetPlaceHolder("https://...")

	saveBtn := widget.NewButton("Save", nil)
	saveBtn.Importance = widget.HighImportance
	cancelBtn := widget.NewButton("Cancel", nil)
	cancelBtn.Importance = widget.LowImportance

	var pop *widget.PopUp
	saveBtn.OnTapped = func() {
		name := strings.TrimSpace(nameEntry.Text)
		url := strings.TrimSpace(urlEntry.Text)
		if pop != nil {
			pop.Hide()
		}
		if name == "" || url == "" {
			showToast(w, "name and URL required", toastWarn)
			return
		}
		if err := voice.SaveAudioAlias(name, url); err != nil {
			showToast(w, "save alias: "+err.Error(), toastError)
			return
		}
		nitoLog("saved alias: " + name)
		showToast(w, "alias saved: "+name, toastSuccess)
		if onAdded != nil {
			onAdded()
		}
	}
	cancelBtn.OnTapped = func() {
		if pop != nil {
			pop.Hide()
		}
	}

	body := container.NewVBox(
		monoTxt("alias name", colDimMid), nameEntry, vspace(4),
		monoTxt("URL", colDimMid), urlEntry, vspace(6),
		container.NewHBox(saveBtn, cancelBtn),
	)
	pop = showNitoPopup("ADD ALIAS", body, w)
	w.Canvas().Focus(nameEntry)
}

// ── buildTracksTab ────────────────────────────────────────────────────────────

// buildTracksTab builds the TRACKS tab. Returns the tab content and a tick func
// that must be called on the Fyne thread every ~50 ms to animate meters.
func buildTracksTab(w fyne.Window) (fyne.CanvasObject, func()) {
	// ── 3 track row bundles ───────────────────────────────────────────────────
	rows := [3]*trackRowBundle{
		newTrackRowBundle(0, w),
		newTrackRowBundle(1, w),
		newTrackRowBundle(2, w),
	}

	trackBox := container.NewVBox(
		rows[0].wrapper,
		hline(),
		rows[1].wrapper,
		hline(),
		rows[2].wrapper,
	)

	// ── Alias list ────────────────────────────────────────────────────────────
	aliasBox := container.NewVBox()

	var rebuildAliases func()
	rebuildAliases = func() {
		aliases, _ := voice.ListAudioAliases()
		var aliasRows []fyne.CanvasObject
		for name, url := range aliases {
			n, u := name, url // capture
			removeBtn := widget.NewButton("🗑", func() {
				if err := voice.DeleteAudioAlias(n); err != nil {
					showToast(w, "remove alias: "+err.Error(), toastError)
				} else {
					nitoLog("deleted alias: " + n)
					fyne.Do(rebuildAliases)
				}
			})
			removeBtn.Importance = widget.LowImportance
			playBtn := widget.NewButton("▶", func() {
				showToast(w, "playing alias: "+n, toastInfo)
				startTrackLocal(0, u)
				nitoLog("alias play: " + n)
			})
			playBtn.Importance = widget.LowImportance
			row := container.NewBorder(nil, nil,
				playBtn, removeBtn,
				container.NewPadded(monoTxt(n+" → "+truncURL(u, 30), colText)),
			)
			aliasRows = append(aliasRows, row)
		}
		if len(aliasRows) == 0 {
			aliasRows = append(aliasRows, container.NewPadded(dimTxt("no aliases saved")))
		}
		// Add button at the end
		addBtn := widget.NewButton("+ add alias", func() {
			showAddAliasPopup(w, func() { fyne.Do(rebuildAliases) })
		})
		addBtn.Importance = widget.LowImportance
		aliasRows = append(aliasRows, container.NewPadded(addBtn))
		aliasBox.Objects = aliasRows
		aliasBox.Refresh()
	}
	rebuildAliases()

	accordion := NewCollapseSection("PLAY ALIASES", aliasBox, false)

	// ── Stop All footer ───────────────────────────────────────────────────────
	stopAllBtn := widget.NewButton("■  Stop All", func() {
		stopAllTracks()
		nitoLog("stopped all tracks")
	})
	stopAllBtn.Importance = widget.LowImportance
	_, footerCard := panelStack(false, stopAllBtn)
	footer := container.NewVBox(vspace(6), footerCard, vspace(4))

	scrollBody := container.NewVScroll(container.NewVBox(
		vspace(4),
		trackBox,
		vspace(8),
		accordion,
	))

	tickN := 0
	tickFn := func() {
		for _, r := range rows {
			r.tick(tickN)
		}
		tickN++
	}

	return container.NewBorder(nil, footer, nil, nil, scrollBody), tickFn
}

// truncURL shortens a URL to maxLen characters for display.
func truncURL(u string, maxLen int) string {
	if len(u) <= maxLen {
		return u
	}
	return u[:maxLen-1] + "…"
}
