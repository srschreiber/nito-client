// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package main

import (
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/srschreiber/nito-client/shellapp/components"
	"github.com/srschreiber/nito-client/shellapp/voice"
)

// ── SpectrumWidget ────────────────────────────────────────────────────────────

// SpectrumWidget renders the live 16-band EQ spectrum as a bar graph overlay.
// Call Tick() every ~50 ms to animate it. It reads voice.GetTrackEQBandLevel
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
	n := voice.NumEQBands
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

	level := voice.GetTrackEQBandLevel(0, b)
	barH := int(float32(ph) * level)
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

// buildEQTab builds the TRACK EQ tab with real voice settings.
// Returns the tab canvas object and a tick func for the spectrum widget.
func buildEQTab(w fyne.Window) (fyne.CanvasObject, func()) {
	// ── Spectrum / EQ graph ───────────────────────────────────────────────────
	graph := NewEQGraphWidget()
	spectrum := NewSpectrumWidget()

	// Reload graph from current settings
	syncGraph := func() {
		eq := voice.GetPlaybackEQSettings()
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
	infoLabel := txt("", colDimMid, 11, false, true)

	allPresetNames := func() []string {
		var names []string
		for _, p := range components.ListBuiltinPresets() {
			names = append(names, p.Name)
		}
		custom, _ := voice.LoadCustomPresets()
		for _, p := range custom {
			names = append(names, "★ "+p.Name)
		}
		return names
	}

	var presetSel *widget.Select
	presetSel = widget.NewSelect(allPresetNames(), func(selected string) {
		// Strip the ★ prefix for custom presets
		name := strings.TrimPrefix(selected, "★ ")

		// Try built-in first
		if tagline, tags, ok := components.ApplyPresetByName(name); ok {
			infoLabel.Text = tagline + "  ·  " + tags
			infoLabel.Refresh()
			syncGraph()
			showToast(w, "preset: "+name, toastInfo)
			return
		}
		// Try custom
		custom, _ := voice.LoadCustomPresets()
		for _, p := range custom {
			if p.Name == name {
				p.Apply()
				infoLabel.Text = "custom preset  ·  " + name
				infoLabel.Refresh()
				syncGraph()
				showToast(w, "preset: "+name, toastInfo)
				return
			}
		}
	})

	// Set a sensible initial selection
	if eq := voice.GetPlaybackEQSettings(); eq.BassGain == 0 && eq.MidGain == 0 {
		presetSel.SetSelected("Flat")
	}

	saveBtn := widget.NewButton("Save as…", nil)
	saveBtn.Importance = widget.LowImportance
	saveBtn.OnTapped = func() {
		nameEntry := widget.NewEntry()
		nameEntry.SetPlaceHolder("preset name")
		confirmBtn := widget.NewButton("Save", nil)
		cancelBtn := widget.NewButton("Cancel", nil)
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
			if err := voice.SaveCurrentAsPreset(name); err != nil {
				showToast(w, "save preset: "+err.Error(), toastError)
				return
			}
			nitoLog("saved preset: " + name)
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
			monoTxt("preset name", colDimMid), nameEntry, vspace(6),
			container.NewHBox(confirmBtn, cancelBtn),
		)
		pop = showNitoPopup("SAVE PRESET", body, w)
		w.Canvas().Focus(nameEntry)
	}

	presetRow := container.NewBorder(nil, nil, nil, saveBtn, presetSel)
	presetSection := container.NewVBox(presetRow, container.NewHBox(infoLabel))

	// ── Slider helpers ────────────────────────────────────────────────────────

	// slRow builds a labeled slider with a live value label.
	// onChange is called with the new value each time the slider moves.
	slRow := func(label, initVal string, lo, hi, cur float64, onChange func(float64)) fyne.CanvasObject {
		valLabel := monoTxt("  "+initVal, colText)
		sl := widget.NewSlider(lo, hi)
		sl.Value = cur
		sl.OnChanged = func(v float64) {
			onChange(v)
			valLabel.Text = "  " + fmtSliderValue(label, v)
			valLabel.Refresh()
		}
		return container.NewVBox(
			container.NewHBox(monoTxt(label, colDimMid), valLabel),
			sl,
		)
	}

	wrapCard := func(content fyne.CanvasObject) fyne.CanvasObject {
		_, card := panelStack(false, content)
		return card
	}

	// ── Read current settings ─────────────────────────────────────────────────
	eq := voice.GetPlaybackEQSettings()
	del := voice.GetDelaySettings()
	rev := voice.GetReverbSettings()
	cho := voice.GetChorusSettings()
	pit := voice.GetPlaybackPitchSettings()
	pan := voice.GetPannerSettings()
	vol := voice.GetPlaybackEQVolume()

	// ── 4-band EQ cards ───────────────────────────────────────────────────────
	bassCard := wrapCard(container.NewVBox(
		sectionBadge("BASS"),
		slRow("gain", fmtDB(eq.BassGain), -18, 18, float64(eq.BassGain), func(v float64) {
			s := voice.GetPlaybackEQSettings()
			s.BassGain = float32(v)
			voice.SetPlaybackEQSettings(s)
			voice.SaveAudioSettings()
			syncGraph()
		}),
		slRow("freq", fmtHz(eq.BassHz), 40, 500, float64(eq.BassHz), func(v float64) {
			s := voice.GetPlaybackEQSettings()
			s.BassHz = float32(v)
			voice.SetPlaybackEQSettings(s)
			voice.SaveAudioSettings()
			syncGraph()
		}),
	))

	midCard := wrapCard(container.NewVBox(
		sectionBadge("MID"),
		slRow("gain", fmtDB(eq.MidGain), -18, 18, float64(eq.MidGain), func(v float64) {
			s := voice.GetPlaybackEQSettings()
			s.MidGain = float32(v)
			voice.SetPlaybackEQSettings(s)
			voice.SaveAudioSettings()
			syncGraph()
		}),
		slRow("freq", fmtHz(eq.MidHz), 200, 8000, float64(eq.MidHz), func(v float64) {
			s := voice.GetPlaybackEQSettings()
			s.MidHz = float32(v)
			voice.SetPlaybackEQSettings(s)
			voice.SaveAudioSettings()
			syncGraph()
		}),
		slRow("Q   ", fmtFloat(float64(eq.MidQ)), 0.3, 6.0, float64(eq.MidQ), func(v float64) {
			s := voice.GetPlaybackEQSettings()
			s.MidQ = float32(v)
			voice.SetPlaybackEQSettings(s)
			voice.SaveAudioSettings()
			syncGraph()
		}),
	))

	trebCard := wrapCard(container.NewVBox(
		sectionBadge("TREBLE"),
		slRow("gain", fmtDB(eq.TrebleGain), -18, 18, float64(eq.TrebleGain), func(v float64) {
			s := voice.GetPlaybackEQSettings()
			s.TrebleGain = float32(v)
			voice.SetPlaybackEQSettings(s)
			voice.SaveAudioSettings()
			syncGraph()
		}),
		slRow("freq", fmtHz(eq.TrebleHz), 1000, 16000, float64(eq.TrebleHz), func(v float64) {
			s := voice.GetPlaybackEQSettings()
			s.TrebleHz = float32(v)
			voice.SetPlaybackEQSettings(s)
			voice.SaveAudioSettings()
			syncGraph()
		}),
	))

	presCard := wrapCard(container.NewVBox(
		sectionBadge("PRESENCE"),
		slRow("gain", fmtDB(eq.PresenceGain), -18, 18, float64(eq.PresenceGain), func(v float64) {
			s := voice.GetPlaybackEQSettings()
			s.PresenceGain = float32(v)
			voice.SetPlaybackEQSettings(s)
			voice.SaveAudioSettings()
			syncGraph()
		}),
		slRow("freq", fmtHz(eq.PresenceHz), 2000, 5000, float64(eq.PresenceHz), func(v float64) {
			s := voice.GetPlaybackEQSettings()
			s.PresenceHz = float32(v)
			voice.SetPlaybackEQSettings(s)
			voice.SaveAudioSettings()
			syncGraph()
		}),
		slRow("Q   ", fmtFloat(float64(eq.PresenceQ)), 0.3, 6.0, float64(eq.PresenceQ), func(v float64) {
			s := voice.GetPlaybackEQSettings()
			s.PresenceQ = float32(v)
			voice.SetPlaybackEQSettings(s)
			voice.SaveAudioSettings()
			syncGraph()
		}),
	))

	bandsRow := container.NewGridWithColumns(4, bassCard, midCard, trebCard, presCard)

	// ── Effects cards ─────────────────────────────────────────────────────────

	delayEnabled := widget.NewCheck("enabled", func(b bool) {
		s := voice.GetDelaySettings()
		s.Enabled = b
		voice.SetDelaySettings(s)
		voice.SaveAudioSettings()
	})
	delayEnabled.SetChecked(del.Enabled)

	delayCard := wrapCard(container.NewVBox(
		container.NewHBox(sectionBadge("DELAY"), delayEnabled),
		slRow("delay   ", fmtMs(del.DelayMs), 1, 500, float64(del.DelayMs), func(v float64) {
			s := voice.GetDelaySettings()
			s.DelayMs = float32(v)
			voice.SetDelaySettings(s)
			voice.SaveAudioSettings()
		}),
		slRow("feedback", fmtFloat(float64(del.Feedback)), 0, 0.95, float64(del.Feedback), func(v float64) {
			s := voice.GetDelaySettings()
			s.Feedback = float32(v)
			voice.SetDelaySettings(s)
			voice.SaveAudioSettings()
		}),
	))

	reverbEnabled := widget.NewCheck("enabled", func(b bool) {
		s := voice.GetReverbSettings()
		s.Enabled = b
		voice.SetReverbSettings(s)
		voice.SaveAudioSettings()
	})
	reverbEnabled.SetChecked(rev.Enabled)

	reverbCard := wrapCard(container.NewVBox(
		container.NewHBox(sectionBadge("REVERB"), reverbEnabled),
		slRow("mix  ", fmtFloat(float64(rev.Mix)), 0, 1, float64(rev.Mix), func(v float64) {
			s := voice.GetReverbSettings()
			s.Mix = float32(v)
			voice.SetReverbSettings(s)
			voice.SaveAudioSettings()
		}),
		slRow("size ", fmtFloat(float64(rev.Size)), 0.5, 2.0, float64(rev.Size), func(v float64) {
			s := voice.GetReverbSettings()
			s.Size = float32(v)
			voice.SetReverbSettings(s)
			voice.SaveAudioSettings()
		}),
		slRow("decay", fmtFloat(float64(rev.Decay)), 0, 1, float64(rev.Decay), func(v float64) {
			s := voice.GetReverbSettings()
			s.Decay = float32(v)
			voice.SetReverbSettings(s)
			voice.SaveAudioSettings()
		}),
		slRow("tone ", fmtFloat(float64(rev.Tone)), 0, 1, float64(rev.Tone), func(v float64) {
			s := voice.GetReverbSettings()
			s.Tone = float32(v)
			voice.SetReverbSettings(s)
			voice.SaveAudioSettings()
		}),
	))

	chorusEnabled := widget.NewCheck("enabled", func(b bool) {
		s := voice.GetChorusSettings()
		s.Enabled = b
		voice.SetChorusSettings(s)
		voice.SaveAudioSettings()
	})
	chorusEnabled.SetChecked(cho.Enabled)

	chorusCard := wrapCard(container.NewVBox(
		container.NewHBox(sectionBadge("CHORUS"), chorusEnabled),
		slRow("delay", fmtMs(cho.BaseDelayMs), 5, 30, float64(cho.BaseDelayMs), func(v float64) {
			s := voice.GetChorusSettings()
			s.BaseDelayMs = float32(v)
			voice.SetChorusSettings(s)
			voice.SaveAudioSettings()
		}),
		slRow("rate ", fmtHz(cho.RateHz), 0.1, 5.0, float64(cho.RateHz), func(v float64) {
			s := voice.GetChorusSettings()
			s.RateHz = float32(v)
			voice.SetChorusSettings(s)
			voice.SaveAudioSettings()
		}),
		slRow("depth", fmtMs(cho.DepthMs), 0, 15, float64(cho.DepthMs), func(v float64) {
			s := voice.GetChorusSettings()
			s.DepthMs = float32(v)
			voice.SetChorusSettings(s)
			voice.SaveAudioSettings()
		}),
		slRow("mix  ", fmtFloat(float64(cho.Mix)), 0, 1, float64(cho.Mix), func(v float64) {
			s := voice.GetChorusSettings()
			s.Mix = float32(v)
			voice.SetChorusSettings(s)
			voice.SaveAudioSettings()
		}),
	))

	effectsRow := container.NewGridWithColumns(3, delayCard, reverbCard, chorusCard)

	// ── Pitch / Pan / Output cards ────────────────────────────────────────────

	pitchEnabled := widget.NewCheck("enabled", func(b bool) {
		s := voice.GetPlaybackPitchSettings()
		s.Enabled = b
		voice.SetPlaybackPitchSettings(s)
		voice.SaveAudioSettings()
	})
	pitchEnabled.SetChecked(pit.Enabled)

	pitchCard := wrapCard(container.NewVBox(
		container.NewHBox(sectionBadge("PITCH"), pitchEnabled),
		slRow("semitones", fmtFloat(float64(pit.Semitones))+" st", -12, 12, float64(pit.Semitones), func(v float64) {
			s := voice.GetPlaybackPitchSettings()
			s.Semitones = float32(v)
			voice.SetPlaybackPitchSettings(s)
			voice.SaveAudioSettings()
		}),
	))

	autoPanCheck := widget.NewCheck("auto pan", func(b bool) {
		s := voice.GetPannerSettings()
		s.AutoPanEnabled = b
		voice.SetPannerSettings(s)
		voice.SaveAudioSettings()
	})
	autoPanCheck.SetChecked(pan.AutoPanEnabled)

	panCard := wrapCard(container.NewVBox(
		sectionBadge("PAN"),
		slRow("balance", fmtPan(pan.Balance), -1, 1, float64(pan.Balance), func(v float64) {
			s := voice.GetPannerSettings()
			s.Balance = float32(v)
			voice.SetPannerSettings(s)
			voice.SaveAudioSettings()
		}),
		autoPanCheck,
		slRow("rate ", fmtFloat(float64(pan.AutoPanRate))+" Hz", 0.05, 5.0, float64(pan.AutoPanRate), func(v float64) {
			s := voice.GetPannerSettings()
			s.AutoPanRate = float32(v)
			voice.SetPannerSettings(s)
			voice.SaveAudioSettings()
		}),
		slRow("depth", fmtPct(pan.AutoPanDepth), 0, 1, float64(pan.AutoPanDepth), func(v float64) {
			s := voice.GetPannerSettings()
			s.AutoPanDepth = float32(v)
			voice.SetPannerSettings(s)
			voice.SaveAudioSettings()
		}),
	))

	outputCard := wrapCard(container.NewVBox(
		sectionBadge("OUTPUT"),
		slRow("volume", fmtPct(float32(vol)/100), 0, 800, float64(vol), func(v float64) {
			voice.SetPlaybackEQVolume(int(v))
			voice.SaveAudioSettings()
		}),
	))

	bottomRow := container.NewGridWithColumns(3, pitchCard, panCard, outputCard)

	sep := func() fyne.CanvasObject { return container.NewVBox(vspace(8), hline(), vspace(8)) }

	content := container.NewVScroll(container.NewVBox(
		presetSection, sep(),
		graphStack, sep(),
		bandsRow, sep(),
		effectsRow, sep(),
		bottomRow, vspace(8),
	))

	spectrumTick := func() { spectrum.Tick() }
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
