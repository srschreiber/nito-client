// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package main

import (
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// ── Mock data ─────────────────────────────────────────────────────────────────

type mockTrack struct {
	idx       int
	playing   bool
	title     string
	startedBy string
	broadcast bool
}

var mockTracks = []mockTrack{
	{0, true, "I♥CHILLHOP", "you", true},
	{1, false, "", "", false},
	{2, false, "", "", false},
}

var mockAliases = []string{"radiohead", "lofi-beats", "", "", ""}

// ── EQ preset mock data ───────────────────────────────────────────────────────

type mockPresetDef struct {
	name    string
	tagline string
	tags    string
	custom  bool
}

var mockBuiltinPresets = []mockPresetDef{
	{"Airy", "Light & ethereal", "EQ · Chorus", false},
	{"Auto Pan", "Rhythmic stereo sweep", "Auto-Pan", false},
	{"Balanced", "Gentle V-curve", "EQ only", false},
	{"Bass Boost", "Heavy low end", "EQ only", false},
	{"Bright", "Crispy highs", "EQ only", false},
	{"Chill", "Warm & relaxed", "EQ · Reverb", false},
	{"Delay Echo", "Bouncy echo repeats", "EQ · Delay", false},
	{"Flat", "No processing", "Clean slate", false},
	{"Immersive", "Spacious & enveloping", "EQ · Reverb · Chorus", false},
	{"Lo-Fi", "Warm, dusty & nostalgic", "EQ · Reverb · Delay", false},
	{"Punchy", "Tight bass, forward mids", "EQ only", false},
	{"Techno", "Full FX chain, high energy", "EQ · Reverb · Chorus · Delay · Pan", false},
	{"Vocal Boost", "Forward mids & presence", "EQ only", false},
	{"Warm", "Full bass, rolled highs", "EQ only", false},
}

var mockCustomPresets []mockPresetDef

// ── CircleStopBtn ─────────────────────────────────────────────────────────────

type CircleStopBtn struct {
	widget.BaseWidget
	circ  *canvas.Rectangle
	onTap func()
}

func newCircleStop(onTap func()) *CircleStopBtn {
	b := &CircleStopBtn{onTap: onTap}
	b.circ = canvas.NewRectangle(colAccentDark)
	b.circ.CornerRadius = 9
	b.circ.SetMinSize(fyne.NewSize(18, 18))
	b.ExtendBaseWidget(b)
	return b
}

func (b *CircleStopBtn) CreateRenderer() fyne.WidgetRenderer {
	label := txt("■", colAccent, 9, false, false)
	return widget.NewSimpleRenderer(
		container.NewStack(b.circ, container.NewCenter(label)),
	)
}

func (b *CircleStopBtn) Tapped(_ *fyne.PointEvent) {
	b.circ.FillColor = colAccent
	b.circ.Refresh()
	go func() {
		time.Sleep(120 * time.Millisecond)
		b.circ.FillColor = colAccentDark
		b.circ.Refresh()
		if b.onTap != nil {
			b.onTap()
		}
	}()
}

func (b *CircleStopBtn) TappedSecondary(_ *fyne.PointEvent) {}

func (b *CircleStopBtn) MouseIn(_ *desktop.MouseEvent) {
	b.circ.FillColor = colBorderFocus
	b.circ.Refresh()
}

func (b *CircleStopBtn) MouseMoved(_ *desktop.MouseEvent) {}

func (b *CircleStopBtn) MouseOut() {
	b.circ.FillColor = colAccentDark
	b.circ.Refresh()
}

// ── Track row ─────────────────────────────────────────────────────────────────

func buildTrackRow(t mockTrack) fyne.CanvasObject {
	if !t.playing {
		return container.NewPadded(container.NewHBox(
			txt("• ", colDimMid, 13, false, true),
			monoTxt("idle", colDim),
		))
	}

	stopBtn := newCircleStop(func() {})
	noteIcon := txt("♪ ", colAccent, 13, false, true)

	var detail fyne.CanvasObject
	if t.broadcast {
		broadcastIcon := txt(" ◉ ", colAccent, 13, false, true)
		titleStr := t.title
		if titleStr == "" {
			titleStr = "broadcasting"
		}
		detail = container.NewHBox(noteIcon, broadcastIcon, monoTxt(titleStr, colText))
	} else {
		byStr := "you"
		if t.startedBy != "you" {
			byStr = "by: " + t.startedBy
		}
		byLabel := monoTxt("  "+byStr, colDim)
		if t.title != "" {
			detail = container.NewVBox(
				container.NewHBox(noteIcon, byLabel),
				monoTxt("  "+t.title, colDimMid),
			)
		} else {
			detail = container.NewHBox(noteIcon, byLabel)
		}
	}

	return container.NewPadded(container.NewHBox(stopBtn, detail))
}

// ── Voice settings popup ──────────────────────────────────────────────────────

func fmtFloat(v float64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	whole := int(v)
	frac := int((v-float64(whole))*10 + 0.5)
	s := itoa(whole) + "." + itoa(frac)
	if neg {
		s = "-" + s
	}
	return s
}

func showVoiceSettingsPopup(w fyne.Window) {
	inputSel := widget.NewSelect([]string{"Default Microphone", "USB Headset", "Built-in Mic"}, func(string) {})
	inputSel.SetSelected("Default Microphone")
	outputSel := widget.NewSelect([]string{"Default Speaker", "USB Headset", "Built-in Output"}, func(string) {})
	outputSel.SetSelected("Default Speaker")
	noiseCheck := widget.NewCheck("Noise reduction (RNNoise)", func(bool) {})
	noiseCheck.SetChecked(true)
	aecCheck := widget.NewCheck("Echo cancellation (AEC3)", func(bool) {})
	aecCheck.SetChecked(true)

	pitchSlider := widget.NewSlider(-6, 6)
	pitchSlider.Step = 0.5
	pitchLabel := monoTxt("0.0 st", colText)
	pitchSlider.OnChanged = func(v float64) {
		sign := "+"
		if v < 0 {
			sign = ""
		}
		pitchLabel.Text = sign + fmtFloat(v) + " st"
		pitchLabel.Refresh()
	}

	body := container.NewVBox(
		monoTxt("Input device", colDimMid), inputSel, vspace(4),
		monoTxt("Output device", colDimMid), outputSel, vspace(4),
		hline(), vspace(4),
		noiseCheck, aecCheck, vspace(4),
		hline(), vspace(4),
		monoTxt("Pitch shift", colDimMid),
		container.NewHBox(pitchSlider, pitchLabel),
	)
	showNitoPopup("VOICE SETTINGS", body, w)
}

// ── Idle track popup ──────────────────────────────────────────────────────────

func showIdleTrackPopup(w fyne.Window, idx int) {
	urlEntry := widget.NewEntry()
	urlEntry.SetPlaceHolder("https://...")

	var aliases []string
	for _, a := range mockAliases {
		if a != "" {
			aliases = append(aliases, a)
		}
	}
	aliasSel := widget.NewSelect(aliases, func(string) {})

	playBtn := widget.NewButton("Play", nil)
	playBtn.Importance = widget.HighImportance
	cancelBtn := widget.NewButton("Cancel", nil)
	cancelBtn.Importance = widget.LowImportance

	var pop *widget.PopUp
	playBtn.OnTapped = func() {
		target := strings.TrimSpace(urlEntry.Text)
		if target == "" && aliasSel.Selected != "" {
			target = aliasSel.Selected
		}
		if pop != nil {
			pop.Hide()
		}
		if target != "" {
			showToast(w, "playing: "+target, toastInfo)
		}
	}
	cancelBtn.OnTapped = func() {
		if pop != nil {
			pop.Hide()
		}
	}

	body := container.NewVBox(
		monoTxt("URL", colDimMid), urlEntry, vspace(4),
		monoTxt("or alias", colDimMid), aliasSel, vspace(6),
		container.NewHBox(playBtn, cancelBtn),
	)
	pop = showNitoPopup("TRACK "+itoa(idx), body, w)
	w.Canvas().Focus(urlEntry)
}

// ── STATUS tab ────────────────────────────────────────────────────────────────

func buildStatusTab() fyne.CanvasObject {
	connSection := container.NewVBox(
		vspace(4),
		container.NewHBox(txt("● ", colGreen, 13, false, true), monoTxt("online", colText)),
		container.NewHBox(monoTxt("  ping    ", colDimMid), monoTxt("42ms", colText)),
		container.NewHBox(monoTxt("  broker  ", colDimMid), monoTxt("nito.example.com", colText)),
		container.NewHBox(monoTxt("  user    ", colDimMid), monoTxt("alice", colText)),
	)

	statsContent := container.NewVBox(
		container.NewHBox(monoTxt("voice pkt/s  ", colDimMid), monoTxt("42", colText)),
		container.NewHBox(monoTxt("voice loss   ", colDimMid), monoTxt("1.0%", colText)),
		container.NewHBox(monoTxt("rtt          ", colDimMid), monoTxt("38ms", colText)),
		container.NewHBox(monoTxt("jitter       ", colDimMid), monoTxt("4ms", colText)),
		container.NewHBox(monoTxt("opus bitrate ", colDimMid), monoTxt("32 kbps", colText)),
	)

	accordion := NewCollapseSection("STATS", container.NewVBox(statsContent), false)

	sep := func() fyne.CanvasObject { return container.NewVBox(vspace(8), hline(), vspace(8)) }

	return container.NewVScroll(container.NewVBox(
		connSection, sep(),
		accordion,
	))
}

// ── TRACKS tab ────────────────────────────────────────────────────────────────

func buildTracksTab(w fyne.Window) fyne.CanvasObject {
	var trackRows []fyne.CanvasObject
	for i, t := range mockTracks {
		row := buildTrackRow(t)
		if !t.playing {
			idx := i
			trackRows = append(trackRows, NewHoverRow(row, func() {
				showIdleTrackPopup(w, idx)
			}))
		} else {
			trackRows = append(trackRows, row)
		}
	}

	var aliasRows []fyne.CanvasObject
	for _, a := range mockAliases {
		if a == "" {
			addBtn := widget.NewButton("+", func() {})
			addBtn.Importance = widget.LowImportance
			aliasRows = append(aliasRows, container.NewBorder(nil, nil, nil, addBtn,
				container.NewPadded(dimTxt("empty"))))
		} else {
			alias := a
			removeBtn := widget.NewButton("−", func() { _ = alias }) // TODO: remove alias
			removeBtn.Importance = widget.LowImportance
			aliasRows = append(aliasRows, container.NewBorder(nil, nil, nil, removeBtn,
				container.NewPadded(monoTxt(alias, colText))))
		}
	}
	accordion := NewCollapseSection("PLAY ALIASES", container.NewVBox(aliasRows...), false)

	scrollItems := append(trackRows, vspace(8), accordion)
	scrollBody := container.NewVScroll(container.NewVBox(scrollItems...))

	stopAllBtn := widget.NewButton("■  Stop All", func() {})
	stopAllBtn.Importance = widget.LowImportance
	_, footerCard := panelStack(false, stopAllBtn)
	footer := container.NewVBox(vspace(6), footerCard, vspace(4))

	return container.NewBorder(nil, footer, nil, nil, scrollBody)
}

// ── TRACK EQ tab ──────────────────────────────────────────────────────────────

func buildEQTab(w fyne.Window) fyne.CanvasObject {
	// ── Preset selector ───────────────────────────────────────────────────────
	allPresetNames := func() []string {
		names := make([]string, 0, len(mockBuiltinPresets)+len(mockCustomPresets))
		for _, p := range mockBuiltinPresets {
			names = append(names, p.name)
		}
		for _, p := range mockCustomPresets {
			names = append(names, "★ "+p.name)
		}
		return names
	}

	infoLabel := txt("No processing  ·  Clean slate", colDimMid, 11, false, true)

	var presetSel *widget.Select
	presetSel = widget.NewSelect(allPresetNames(), func(selected string) {
		for _, p := range append(mockBuiltinPresets, mockCustomPresets...) {
			if p.name == selected || "★ "+p.name == selected {
				infoLabel.Text = p.tagline + "  ·  " + p.tags
				infoLabel.Refresh()
				name := p.name
				showToast(w, "applied: "+name, toastInfo)
				return
			}
		}
	})
	presetSel.SetSelected("Flat")

	saveBtn := widget.NewButton("Save as...", nil)
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
			if name != "" {
				mockCustomPresets = append(mockCustomPresets, mockPresetDef{
					name: name, tagline: "Custom preset", tags: "Custom", custom: true,
				})
				presetSel.Options = allPresetNames()
				presetSel.Refresh()
				showToast(w, "saved preset: "+name, toastSuccess)
			}
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

	// ── EQ graph ──────────────────────────────────────────────────────────────
	graph := NewEQGraphWidget()

	// Helper: labeled slider row.
	slRow := func(label, val string, lo, hi, cur float64) fyne.CanvasObject {
		sl := widget.NewSlider(lo, hi)
		sl.Value = cur
		return container.NewVBox(
			container.NewHBox(monoTxt(label, colDimMid), monoTxt("  "+val, colText)),
			sl,
		)
	}

	// wrapCard gives each section a subtle bordered panel.
	wrapCard := func(content fyne.CanvasObject) fyne.CanvasObject {
		_, card := panelStack(false, content)
		return card
	}

	// 4-band EQ
	bassCard := wrapCard(container.NewVBox(
		sectionBadge("BASS"),
		slRow("gain", "+3.0 dB", -18, 18, 3),
		slRow("freq", "80 Hz", 40, 500, 80),
	))
	midCard := wrapCard(container.NewVBox(
		sectionBadge("MID"),
		slRow("gain", "-0.5 dB", -18, 18, -0.5),
		slRow("freq", "800 Hz", 200, 8000, 800),
		slRow("Q   ", "1.2", 0.3, 6.0, 1.2),
	))
	trebCard := wrapCard(container.NewVBox(
		sectionBadge("TREBLE"),
		slRow("gain", "+2.0 dB", -18, 18, 2),
		slRow("freq", "8000 Hz", 1000, 16000, 8000),
	))
	presCard := wrapCard(container.NewVBox(
		sectionBadge("PRESENCE"),
		slRow("gain", "+2.0 dB", -18, 18, 2),
		slRow("freq", "3000 Hz", 2000, 5000, 3000),
		slRow("Q   ", "1.5", 0.3, 6.0, 1.5),
	))
	bandsRow := container.NewGridWithColumns(4, bassCard, midCard, trebCard, presCard)

	// Effects
	delayEnabled := widget.NewCheck("enabled", func(bool) {})
	delayCard := wrapCard(container.NewVBox(
		container.NewHBox(sectionBadge("DELAY"), delayEnabled),
		slRow("delay   ", "100 ms", 1, 500, 100),
		slRow("feedback", "0.50", 0, 0.95, 0.5),
	))
	reverbEnabled := widget.NewCheck("enabled", func(bool) {})
	reverbCard := wrapCard(container.NewVBox(
		container.NewHBox(sectionBadge("REVERB"), reverbEnabled),
		slRow("mix  ", "0.30", 0, 1, 0.3),
		slRow("size ", "1.0", 0.5, 2.0, 1.0),
		slRow("decay", "0.50", 0, 1, 0.5),
		slRow("tone ", "0.70", 0, 1, 0.7),
	))
	chorusEnabled := widget.NewCheck("enabled", func(bool) {})
	chorusCard := wrapCard(container.NewVBox(
		container.NewHBox(sectionBadge("CHORUS"), chorusEnabled),
		slRow("delay", "15 ms", 5, 30, 15),
		slRow("rate ", "0.5 Hz", 0.1, 5.0, 0.5),
		slRow("depth", "5.0 ms", 0, 15, 5.0),
		slRow("mix  ", "0.30", 0, 1, 0.3),
	))
	effectsRow := container.NewGridWithColumns(3, delayCard, reverbCard, chorusCard)

	// Pitch / Pan / Output
	pitchEnabled := widget.NewCheck("enabled", func(bool) {})
	pitchCard := wrapCard(container.NewVBox(
		container.NewHBox(sectionBadge("PITCH"), pitchEnabled),
		slRow("semitones", "0.0 st", -12, 12, 0),
	))
	autoPanCheck := widget.NewCheck("auto pan", func(bool) {})
	panCard := wrapCard(container.NewVBox(
		sectionBadge("PAN"),
		slRow("balance", "center", -1, 1, 0),
		autoPanCheck,
		slRow("rate ", "0.5 Hz", 0.05, 5.0, 0.5),
		slRow("depth", "0%", 0, 1, 0),
	))
	outputCard := wrapCard(container.NewVBox(
		sectionBadge("OUTPUT"),
		slRow("volume", "100%", 0, 800, 100),
	))
	bottomRow := container.NewGridWithColumns(3, pitchCard, panCard, outputCard)

	sep := func() fyne.CanvasObject { return container.NewVBox(vspace(8), hline(), vspace(8)) }

	return container.NewVScroll(container.NewVBox(
		presetSection, sep(),
		graph, sep(),
		bandsRow, sep(),
		effectsRow, sep(),
		bottomRow, vspace(8),
	))
}

// ── StatusPanel ───────────────────────────────────────────────────────────────

type StatusPanel struct {
	widget.BaseWidget
	TabBar  *TabBar
	content *fyne.Container
	tabs    []fyne.CanvasObject
}

func NewStatusPanel(w fyne.Window) *StatusPanel {
	sp := &StatusPanel{}

	tabNames := []string{"STATUS", "TRACKS", "TRACK EQ", "INVITES", "NOTIF", "LOGS"}
	tabs := []fyne.CanvasObject{
		buildStatusTab(),
		buildTracksTab(w),
		buildEQTab(w),
		buildInvitesTab(w),
		buildNotifTab(),
		buildLogsTab(),
	}
	for i, t := range tabs {
		if i != 0 {
			t.Hide()
		}
	}

	content := container.NewStack(tabs...)
	sp.content = content
	sp.tabs = tabs
	sp.TabBar = NewTabBar(tabNames, 0, func(idx int) {
		for i, t := range tabs {
			if i == idx {
				t.Show()
			} else {
				t.Hide()
			}
		}
	})
	sp.ExtendBaseWidget(sp)
	return sp
}

func (sp *StatusPanel) PrevTab() { sp.TabBar.Prev() }
func (sp *StatusPanel) NextTab() { sp.TabBar.Next() }

func (sp *StatusPanel) CreateRenderer() fyne.WidgetRenderer {
	body := container.NewBorder(
		container.NewVBox(sp.TabBar, hline()),
		nil, nil, nil,
		sp.content,
	)
	_, panel := panelStack(false, body)
	return widget.NewSimpleRenderer(panel)
}
