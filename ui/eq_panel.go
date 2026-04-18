// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package main

import (
	"image/color"
	"math"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/srschreiber/nito-client/engine/components"
	"github.com/srschreiber/nito-client/engine/sounds"
	"github.com/srschreiber/nito-client/ui/clientlog"
)

// ── SpectrumWidget ────────────────────────────────────────────────────────────

// SpectrumWidget renders the live 16-band EQ spectrum as a bar graph overlay.
// Call Tick() every ~50 ms to animate it. It reads sounds.GetTrackEQBandLevel
// directly so no state is stored here.
type SpectrumWidget struct {
	widget.BaseWidget
	raster *canvas.Raster
}

func NewSpectrumWidget() *SpectrumWidget {
	sw := &SpectrumWidget{}
	sw.raster = canvas.NewRasterWithPixels(func(px, py, pw, ph int) color.Color {
		return sw.pixelAt(px, py, pw, ph)
	})
	sw.raster.SetMinSize(fyne.NewSize(0, 60))
	sw.ExtendBaseWidget(sw)
	return sw
}

func (sw *SpectrumWidget) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(sw.raster)
}

// Tick refreshes the spectrum display. Call on Fyne thread at ~50 ms.
func (sw *SpectrumWidget) Tick() { sw.raster.Refresh() }

func (sw *SpectrumWidget) pixelAt(px, py, pw, ph int) color.Color {
	n := sounds.NumEQBands
	if pw <= 0 || ph <= 0 || n <= 0 {
		return colTransparent
	}
	bandW := pw / n
	if bandW <= 0 {
		return colTransparent
	}
	b := px / bandW
	if b >= n {
		return colTransparent
	}
	if px%bandW == bandW-1 { // 1px gap
		return colTransparent
	}

	level := sounds.GetTrackEQBandLevel(0, b)
	// Match bar-graph visual treatment: 3× scale, sqrt compression, wobble.
	scaled := math.Sqrt(float64(level) * 3.0)
	if level > 0 {
		wobble := 0.05 * math.Sin(float64(time.Now().UnixMilli()%628)/100.0)
		scaled += wobble
	}
	if scaled < 0 {
		scaled = 0
	} else if scaled > 1 {
		scaled = 1
	}
	barH := int(float64(ph) * scaled)
	if py < ph-barH {
		return colTransparent
	}

	// Gradient: teal at bottom, accent at top
	posInBar := float32(ph-py) / float32(ph)
	alpha := uint8(0x99) // semi-transparent overlay on EQ curve
	if posInBar > 0.7 {
		return color.NRGBA{R: 0x8b, G: 0x5c, B: 0xf6, A: alpha} // accent purple
	} else if posInBar > 0.4 {
		return color.NRGBA{R: 0x67, G: 0xe8, B: 0xf9, A: alpha} // cyan
	}
	return color.NRGBA{R: 0x34, G: 0xd3, B: 0x99, A: alpha} // green
}

// ── EQ panel ──────────────────────────────────────────────────────────────────

// showEQPopup opens the Track EQ panel as a floating popup with its own animation ticker.
func showEQPopup(w fyne.Window) {
	content, tick := buildEQContent(w)

	minSz := canvas.NewRectangle(colTransparent)
	minSz.SetMinSize(fyne.NewSize(700, 520))
	body := container.NewStack(minSz, content)

	pop := showNitoPopup("TRACK EQ", body, w)

	go func() {
		t := time.NewTicker(50 * time.Millisecond)
		defer t.Stop()
		for range t.C {
			if !pop.Visible() {
				return
			}
			fyne.Do(tick)
		}
	}()
}

// buildEQContent builds the TRACK EQ panel with real sounds settings.
// Returns the canvas object and a tick func for the spectrum widget.
func buildEQContent(w fyne.Window) (fyne.CanvasObject, func()) {
	// ── Spectrum / EQ graph ───────────────────────────────────────────────────
	graph := NewEQGraphWidget()
	spectrum := NewSpectrumWidget()

	// Reload graph from current settings
	syncGraph := func() {
		eq := sounds.GetPlaybackEQSettings()
		graph.UpdateSettings(eqGraphSettings{
			BassGain: float64(eq.BassGain), BassHz: float64(eq.BassHz),
			MidGain: float64(eq.MidGain), MidHz: float64(eq.MidHz), MidQ: float64(eq.MidQ),
			TrebGain: float64(eq.TrebleGain), TrebHz: float64(eq.TrebleHz),
			PresenceGain: float64(eq.PresenceGain), PresenceHz: float64(eq.PresenceHz),
			PresenceQ: float64(eq.PresenceQ),
		})
	}
	syncGraph()

	graphStack := container.NewStack(graph, spectrum)

	// ── Preset selector ───────────────────────────────────────────────────────
	infoLabel := txt("", liveDimMid, 11, false, true)

	allPresetNames := func() []string {
		var names []string
		for _, p := range components.ListBuiltinPresets() {
			names = append(names, p.Name)
		}
		custom, _ := sounds.LoadCustomPresets()
		for _, p := range custom {
			names = append(names, "★ "+p.Name)
		}
		return names
	}

	presetReady := false
	var presetSel *widget.Select
	presetSel = widget.NewSelect(allPresetNames(), func(selected string) {
		if !presetReady {
			return
		}
		// Strip the ★ prefix for custom presets
		name := strings.TrimPrefix(selected, "★ ")

		unfocus := func() { go func() { fyne.Do(w.Canvas().Unfocus) }() }
		// Try built-in first
		if tagline, tags, ok := components.ApplyPresetByName(name); ok {
			infoLabel.Text = tagline + "  ·  " + tags
			infoLabel.Refresh()
			syncGraph()
			showToast(w, "preset: "+name, toastInfo)
			unfocus()
			return
		}
		// Try custom
		custom, _ := sounds.LoadCustomPresets()
		for _, p := range custom {
			if p.Name == name {
				p.Apply()
				infoLabel.Text = "custom preset  ·  " + name
				infoLabel.Refresh()
				syncGraph()
				showToast(w, "preset: "+name, toastInfo)
				unfocus()
				return
			}
		}
	})

	// Set a sensible initial selection without triggering the toast.
	if eq := sounds.GetPlaybackEQSettings(); eq.BassGain == 0 && eq.MidGain == 0 {
		presetSel.SetSelected("Flat")
	}
	presetReady = true

	saveBtn := newBtn("Save as…", nil)
	saveBtn.Importance = widget.LowImportance
	saveBtn.OnTapped = func() {
		nameEntry := widget.NewEntry()
		nameEntry.SetPlaceHolder("preset name")
		confirmBtn := newBtn("Save", nil)
		cancelBtn := newBtn("Cancel", nil)
		cancelBtn.Importance = widget.LowImportance
		var pop *widget.PopUp
		confirmBtn.OnTapped = func() {
			name := strings.TrimSpace(nameEntry.Text)
			if pop != nil {
				pop.Hide()
			}
			if name == "" {
				return
			}
			if err := sounds.SaveCurrentAsPreset(name); err != nil {
				showToast(w, "save preset: "+err.Error(), toastError)
				return
			}
			clientlog.Info("saved preset: " + name)
			presetSel.Options = allPresetNames()
			presetSel.Refresh()
			showToast(w, "saved preset: "+name, toastSuccess)
		}
		cancelBtn.OnTapped = func() {
			if pop != nil {
				pop.Hide()
			}
		}
		body := container.NewVBox(
			monoTxt("preset name", liveDimMid), nameEntry, vspace(6),
			container.NewHBox(confirmBtn, cancelBtn),
		)
		pop = showNitoPopup("SAVE PRESET", body, w)
		w.Canvas().Focus(nameEntry)
	}

	presetRow := container.NewBorder(nil, nil, nil, saveBtn, withPointerCursor(presetSel))
	presetSection := container.NewVBox(presetRow, container.NewHBox(infoLabel))

	// ── Slider helpers ────────────────────────────────────────────────────────

	// slRow builds a labeled slider with a live value label.
	// onChange is called with the new value each time the slider moves.
	slRow := func(label, initVal string, lo, hi, cur float64, onChange func(float64)) fyne.CanvasObject {
		valLabel := monoTxt("  "+initVal, colText)
		sl := newNoFocusSlider(lo, hi)
		sl.Value = cur
		sl.OnChanged = func(v float64) {
			onChange(v)
			valLabel.Text = "  " + fmtSliderValue(label, v)
			valLabel.Refresh()
		}
		return container.NewVBox(
			container.NewHBox(monoTxt(label, liveDimMid), valLabel),
			withPointerCursorNoHover(sl),
		)
	}

	wrapCard := func(content fyne.CanvasObject) fyne.CanvasObject {
		_, card := panelStack(false, content)
		return card
	}

	// ── Read current settings ─────────────────────────────────────────────────
	eq := sounds.GetPlaybackEQSettings()
	del := sounds.GetDelaySettings()
	rev := sounds.GetReverbSettings()
	cho := sounds.GetChorusSettings()
	pit := sounds.GetPlaybackPitchSettings()
	pan := sounds.GetPannerSettings()
	vol := sounds.GetPlaybackEQVolume()

	// ── 4-band EQ cards ───────────────────────────────────────────────────────
	bassCard := wrapCard(NewCollapseSection("BASS", container.NewVBox(
		slRow("gain", fmtDB(eq.BassGain), -18, 18, float64(eq.BassGain), func(v float64) {
			s := sounds.GetPlaybackEQSettings()
			s.BassGain = float32(v)
			sounds.SetPlaybackEQSettings(s)
			sounds.SaveAudioSettings()
			syncGraph()
		}),
		slRow("freq", fmtHz(eq.BassHz), 40, 500, float64(eq.BassHz), func(v float64) {
			s := sounds.GetPlaybackEQSettings()
			s.BassHz = float32(v)
			sounds.SetPlaybackEQSettings(s)
			sounds.SaveAudioSettings()
			syncGraph()
		}),
	), false))

	midCard := wrapCard(NewCollapseSection("MID", container.NewVBox(
		slRow("gain", fmtDB(eq.MidGain), -18, 18, float64(eq.MidGain), func(v float64) {
			s := sounds.GetPlaybackEQSettings()
			s.MidGain = float32(v)
			sounds.SetPlaybackEQSettings(s)
			sounds.SaveAudioSettings()
			syncGraph()
		}),
		slRow("freq", fmtHz(eq.MidHz), 200, 8000, float64(eq.MidHz), func(v float64) {
			s := sounds.GetPlaybackEQSettings()
			s.MidHz = float32(v)
			sounds.SetPlaybackEQSettings(s)
			sounds.SaveAudioSettings()
			syncGraph()
		}),
		slRow("Q   ", fmtFloat(float64(eq.MidQ)), 0.3, 6.0, float64(eq.MidQ), func(v float64) {
			s := sounds.GetPlaybackEQSettings()
			s.MidQ = float32(v)
			sounds.SetPlaybackEQSettings(s)
			sounds.SaveAudioSettings()
			syncGraph()
		}),
	), false))

	trebCard := wrapCard(NewCollapseSection("TREBLE", container.NewVBox(
		slRow("gain", fmtDB(eq.TrebleGain), -18, 18, float64(eq.TrebleGain), func(v float64) {
			s := sounds.GetPlaybackEQSettings()
			s.TrebleGain = float32(v)
			sounds.SetPlaybackEQSettings(s)
			sounds.SaveAudioSettings()
			syncGraph()
		}),
		slRow("freq", fmtHz(eq.TrebleHz), 1000, 16000, float64(eq.TrebleHz), func(v float64) {
			s := sounds.GetPlaybackEQSettings()
			s.TrebleHz = float32(v)
			sounds.SetPlaybackEQSettings(s)
			sounds.SaveAudioSettings()
			syncGraph()
		}),
	), false))

	presCard := wrapCard(NewCollapseSection("PRESENCE", container.NewVBox(
		slRow("gain", fmtDB(eq.PresenceGain), -18, 18, float64(eq.PresenceGain), func(v float64) {
			s := sounds.GetPlaybackEQSettings()
			s.PresenceGain = float32(v)
			sounds.SetPlaybackEQSettings(s)
			sounds.SaveAudioSettings()
			syncGraph()
		}),
		slRow("freq", fmtHz(eq.PresenceHz), 2000, 5000, float64(eq.PresenceHz), func(v float64) {
			s := sounds.GetPlaybackEQSettings()
			s.PresenceHz = float32(v)
			sounds.SetPlaybackEQSettings(s)
			sounds.SaveAudioSettings()
			syncGraph()
		}),
		slRow("Q   ", fmtFloat(float64(eq.PresenceQ)), 0.3, 6.0, float64(eq.PresenceQ), func(v float64) {
			s := sounds.GetPlaybackEQSettings()
			s.PresenceQ = float32(v)
			sounds.SetPlaybackEQSettings(s)
			sounds.SaveAudioSettings()
			syncGraph()
		}),
	), false))

	bandsRow := newResponsiveGrid(4, 140, bassCard, midCard, trebCard, presCard)

	// ── Effects cards ─────────────────────────────────────────────────────────

	delayEnabled := widget.NewCheck("enabled", func(b bool) {
		s := sounds.GetDelaySettings()
		s.Enabled = b
		sounds.SetDelaySettings(s)
		sounds.SaveAudioSettings()
	})
	delayEnabled.SetChecked(del.Enabled)

	delayCard := wrapCard(NewCollapseSection("DELAY", container.NewVBox(
		withPointerCursor(delayEnabled),
		slRow("delay   ", fmtMs(del.DelayMs), 1, 500, float64(del.DelayMs), func(v float64) {
			s := sounds.GetDelaySettings()
			s.DelayMs = float32(v)
			sounds.SetDelaySettings(s)
			sounds.SaveAudioSettings()
		}),
		slRow("feedback", fmtFloat(float64(del.Feedback)), 0, 0.95, float64(del.Feedback), func(v float64) {
			s := sounds.GetDelaySettings()
			s.Feedback = float32(v)
			sounds.SetDelaySettings(s)
			sounds.SaveAudioSettings()
		}),
	), false))

	reverbEnabled := widget.NewCheck("enabled", func(b bool) {
		s := sounds.GetReverbSettings()
		s.Enabled = b
		sounds.SetReverbSettings(s)
		sounds.SaveAudioSettings()
	})
	reverbEnabled.SetChecked(rev.Enabled)

	reverbCard := wrapCard(NewCollapseSection("REVERB", container.NewVBox(
		withPointerCursor(reverbEnabled),
		slRow("mix  ", fmtFloat(float64(rev.Mix)), 0, 1, float64(rev.Mix), func(v float64) {
			s := sounds.GetReverbSettings()
			s.Mix = float32(v)
			sounds.SetReverbSettings(s)
			sounds.SaveAudioSettings()
		}),
		slRow("size ", fmtFloat(float64(rev.Size)), 0.5, 2.0, float64(rev.Size), func(v float64) {
			s := sounds.GetReverbSettings()
			s.Size = float32(v)
			sounds.SetReverbSettings(s)
			sounds.SaveAudioSettings()
		}),
		slRow("decay", fmtFloat(float64(rev.Decay)), 0, 1, float64(rev.Decay), func(v float64) {
			s := sounds.GetReverbSettings()
			s.Decay = float32(v)
			sounds.SetReverbSettings(s)
			sounds.SaveAudioSettings()
		}),
		slRow("tone ", fmtFloat(float64(rev.Tone)), 0, 1, float64(rev.Tone), func(v float64) {
			s := sounds.GetReverbSettings()
			s.Tone = float32(v)
			sounds.SetReverbSettings(s)
			sounds.SaveAudioSettings()
		}),
	), false))

	chorusEnabled := widget.NewCheck("enabled", func(b bool) {
		s := sounds.GetChorusSettings()
		s.Enabled = b
		sounds.SetChorusSettings(s)
		sounds.SaveAudioSettings()
	})
	chorusEnabled.SetChecked(cho.Enabled)

	chorusCard := wrapCard(NewCollapseSection("CHORUS", container.NewVBox(
		withPointerCursor(chorusEnabled),
		slRow("delay", fmtMs(cho.BaseDelayMs), 5, 30, float64(cho.BaseDelayMs), func(v float64) {
			s := sounds.GetChorusSettings()
			s.BaseDelayMs = float32(v)
			sounds.SetChorusSettings(s)
			sounds.SaveAudioSettings()
		}),
		slRow("rate ", fmtHz(cho.RateHz), 0.1, 5.0, float64(cho.RateHz), func(v float64) {
			s := sounds.GetChorusSettings()
			s.RateHz = float32(v)
			sounds.SetChorusSettings(s)
			sounds.SaveAudioSettings()
		}),
		slRow("depth", fmtMs(cho.DepthMs), 0, 15, float64(cho.DepthMs), func(v float64) {
			s := sounds.GetChorusSettings()
			s.DepthMs = float32(v)
			sounds.SetChorusSettings(s)
			sounds.SaveAudioSettings()
		}),
		slRow("mix  ", fmtFloat(float64(cho.Mix)), 0, 1, float64(cho.Mix), func(v float64) {
			s := sounds.GetChorusSettings()
			s.Mix = float32(v)
			sounds.SetChorusSettings(s)
			sounds.SaveAudioSettings()
		}),
	), false))

	effectsRow := newResponsiveGrid(4, 140, delayCard, reverbCard, chorusCard)

	// ── Pitch / Pan / Output cards ────────────────────────────────────────────

	pitchEnabled := widget.NewCheck("enabled", func(b bool) {
		s := sounds.GetPlaybackPitchSettings()
		s.Enabled = b
		sounds.SetPlaybackPitchSettings(s)
		sounds.SaveAudioSettings()
	})
	pitchEnabled.SetChecked(pit.Enabled)

	pitchCard := wrapCard(NewCollapseSection("PITCH", container.NewVBox(
		withPointerCursor(pitchEnabled),
		slRow("semitones", fmtFloat(float64(pit.Semitones))+" st", -12, 12, float64(pit.Semitones), func(v float64) {
			s := sounds.GetPlaybackPitchSettings()
			s.Semitones = float32(v)
			sounds.SetPlaybackPitchSettings(s)
			sounds.SaveAudioSettings()
		}),
	), false))

	autoPanCheck := widget.NewCheck("auto pan", func(b bool) {
		s := sounds.GetPannerSettings()
		s.AutoPanEnabled = b
		sounds.SetPannerSettings(s)
		sounds.SaveAudioSettings()
	})
	autoPanCheck.SetChecked(pan.AutoPanEnabled)

	panCard := wrapCard(NewCollapseSection("PAN", container.NewVBox(
		slRow("balance", fmtPan(pan.Balance), -1, 1, float64(pan.Balance), func(v float64) {
			s := sounds.GetPannerSettings()
			s.Balance = float32(v)
			sounds.SetPannerSettings(s)
			sounds.SaveAudioSettings()
		}),
		withPointerCursor(autoPanCheck),
		slRow("rate ", fmtFloat(float64(pan.AutoPanRate))+" Hz", 0.05, 5.0, float64(pan.AutoPanRate), func(v float64) {
			s := sounds.GetPannerSettings()
			s.AutoPanRate = float32(v)
			sounds.SetPannerSettings(s)
			sounds.SaveAudioSettings()
		}),
		slRow("depth", fmtPct(pan.AutoPanDepth), 0, 1, float64(pan.AutoPanDepth), func(v float64) {
			s := sounds.GetPannerSettings()
			s.AutoPanDepth = float32(v)
			sounds.SetPannerSettings(s)
			sounds.SaveAudioSettings()
		}),
	), false))

	outputCard := wrapCard(NewCollapseSection("OUTPUT", container.NewVBox(
		slRow("volume", fmtPct(float32(vol)/100), 0, 800, float64(vol), func(v float64) {
			sounds.SetPlaybackEQVolume(int(v))
			sounds.SaveAudioSettings()
		}),
	), false))

	bottomRow := newResponsiveGrid(4, 140, pitchCard, panCard, outputCard)

	sep := func() fyne.CanvasObject { return container.NewVBox(vspace(8), hline(), vspace(8)) }

	content := container.NewVScroll(container.NewVBox(
		presetSection, sep(),
		graphStack, sep(),
		bandsRow, sep(),
		effectsRow, sep(),
		bottomRow, vspace(8),
	))

	spectrumTick := func() {
		spectrum.Tick()
		graph.Tick()
	}
	return content, spectrumTick
}

// ── Slider value formatters ───────────────────────────────────────────────────

func fmtSliderValue(label string, v float64) string {
	switch {
	case strings.Contains(label, "gain"):
		return fmtDB(float32(v))
	case strings.Contains(label, "freq"):
		return fmtHz(float32(v))
	case strings.Contains(label, "delay"), strings.Contains(label, "depth") && !strings.Contains(label, "auto"):
		return fmtMs(float32(v))
	case strings.Contains(label, "semitone"):
		return fmtFloat(v) + " st"
	case strings.Contains(label, "balance"):
		return fmtPan(float32(v))
	case strings.Contains(label, "volume"):
		return fmtPct(float32(v) / 100)
	case strings.Contains(label, "depth"):
		return fmtPct(float32(v))
	default:
		return fmtFloat(v)
	}
}

func fmtDB(v float32) string {
	if v >= 0 {
		return "+" + fmtFloat(float64(v)) + " dB"
	}
	return fmtFloat(float64(v)) + " dB"
}

func fmtHz(v float32) string {
	if v >= 1000 {
		return fmtFloat(float64(v)/1000) + " kHz"
	}
	return itoa(int(v)) + " Hz"
}

func fmtMs(v float32) string { return fmtFloat(float64(v)) + " ms" }

func fmtPan(v float32) string {
	if v < -0.05 {
		return fmtFloat(float64(-v*100)) + "% L"
	} else if v > 0.05 {
		return fmtFloat(float64(v*100)) + "% R"
	}
	return "center"
}

func fmtPct(v float32) string { return itoa(int(v*100)) + "%" }
