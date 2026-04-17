// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package main

import (
	_ "embed"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

//go:embed licenses_data/01_nito.txt
var licNito string

//go:embed licenses_data/02_webrtc_audio_processing.txt
var licWebrtcAPM string

//go:embed licenses_data/03_webrtc.txt
var licWebrtc string

//go:embed licenses_data/04_rnnoise.txt
var licRnnoise string

//go:embed licenses_data/05_signalsmith.txt
var licSignalsmith string

//go:embed licenses_data/06_abseil.txt
var licAbseil string

//go:embed licenses_data/07_ooura_fft.txt
var licOoura string

//go:embed licenses_data/08_pffft.txt
var licPffft string

//go:embed licenses_data/09_fft_olesen.txt
var licFftOlesen string

//go:embed licenses_data/10_spl_sqrt_floor.txt
var licSplSqrt string

//go:embed licenses_data/11_webrtc_rnnoise.txt
var licWebrtcRnnoise string

//go:embed licenses_data/12_fyne.txt
var licFyne string

type licenseEntry struct {
	name string
	text string
}

var allLicenses = []licenseEntry{
	{"nito", licNito},
	{"webrtc-audio-processing", licWebrtcAPM},
	{"WebRTC", licWebrtc},
	{"rnnoise", licRnnoise},
	{"signalsmith-stretch", licSignalsmith},
	{"Abseil", licAbseil},
	{"Ooura FFT", licOoura},
	{"PFFFT", licPffft},
	{"FFT (Olesen)", licFftOlesen},
	{"spl_sqrt_floor", licSplSqrt},
	{"WebRTC/rnnoise", licWebrtcRnnoise},
	{"Fyne", licFyne},
}

// showAboutWindow opens a separate window with two panes: license list on the
// left, license text on the right.
func showAboutWindow(a fyne.App) {
	w := a.NewWindow("About & Licenses")
	w.Resize(fyne.NewSize(760, 520))

	selected := 0
	var listWidget *widget.List

	// Selectable label in a scroll container gives both H and V scrollbars.
	textLabel := widget.NewLabel(allLicenses[0].text)
	textLabel.TextStyle = fyne.TextStyle{Monospace: true}
	textLabel.Wrapping = fyne.TextWrapOff
	textLabel.Selectable = true
	rightScroll := container.NewScroll(textLabel)

	listWidget = widget.NewList(
		func() int { return len(allLicenses) },
		func() fyne.CanvasObject {
			return container.NewPadded(monoTxt("placeholder", colText))
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			label := obj.(*fyne.Container).Objects[0].(*canvas.Text)
			if id == selected {
				label.Color = colAccent
			} else {
				label.Color = colDimMid
			}
			label.Text = allLicenses[id].name
			label.Refresh()
		},
	)
	listWidget.OnSelected = func(id widget.ListItemID) {
		selected = id
		textLabel.SetText(allLicenses[id].text)
		rightScroll.ScrollToTop()
		listWidget.Refresh()
	}

	split := container.NewHSplit(listWidget, rightScroll)
	split.SetOffset(0.25)

	header := container.NewVBox(
		sectionBadge("ABOUT & LICENSES"),
		vspace(4),
		hline(),
	)

	bg := canvas.NewRectangle(colBg)
	w.SetContent(container.NewStack(bg,
		container.NewPadded(container.NewBorder(header, nil, nil, nil, split)),
	))
	w.Show()
}
