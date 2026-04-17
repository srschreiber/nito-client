// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package main

import (
	"image"
	"image/color"
	"math"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
)

// ── Biquad filter math ────────────────────────────────────────────────────────

type biquadCoeffs struct{ b0, b1, b2, a1, a2 float64 }

// magDB returns the magnitude response in dB at frequency freq (Hz) at fs sample rate.
func (c biquadCoeffs) magDB(freq, fs float64) float64 {
	w := 2 * math.Pi * freq / fs
	cosW, sinW := math.Cos(w), math.Sin(w)
	cos2W, sin2W := math.Cos(2*w), math.Sin(2*w)
	rn := c.b0 + c.b1*cosW + c.b2*cos2W
	in_ := -c.b1*sinW - c.b2*sin2W
	rd := 1 + c.a1*cosW + c.a2*cos2W
	id := -c.a1*sinW - c.a2*sin2W
	den := rd*rd + id*id
	if den < 1e-20 {
		return 0
	}
	mag := math.Sqrt((rn*rn + in_*in_) / den)
	if mag < 1e-10 {
		return -100
	}
	return 20 * math.Log10(mag)
}

// lowShelf — Audio EQ Cookbook low-shelf, S=1.
func lowShelf(dBgain, fc, fs float64) biquadCoeffs {
	A := math.Pow(10, dBgain/40)
	w0 := 2 * math.Pi * fc / fs
	cosW, sinW := math.Cos(w0), math.Sin(w0)
	sqA := math.Sqrt(A)
	alpha := sinW * math.Sqrt2 / 2 // S=1 simplification
	b0 := A * ((A + 1) - (A-1)*cosW + 2*sqA*alpha)
	b1 := 2 * A * ((A - 1) - (A+1)*cosW)
	b2 := A * ((A + 1) - (A-1)*cosW - 2*sqA*alpha)
	a0 := (A + 1) + (A-1)*cosW + 2*sqA*alpha
	a1 := -2 * ((A - 1) + (A+1)*cosW)
	a2 := (A + 1) + (A-1)*cosW - 2*sqA*alpha
	return biquadCoeffs{b0 / a0, b1 / a0, b2 / a0, a1 / a0, a2 / a0}
}

// highShelf — Audio EQ Cookbook high-shelf, S=1.
func highShelf(dBgain, fc, fs float64) biquadCoeffs {
	A := math.Pow(10, dBgain/40)
	w0 := 2 * math.Pi * fc / fs
	cosW, sinW := math.Cos(w0), math.Sin(w0)
	sqA := math.Sqrt(A)
	alpha := sinW * math.Sqrt2 / 2
	b0 := A * ((A + 1) + (A-1)*cosW + 2*sqA*alpha)
	b1 := -2 * A * ((A - 1) + (A+1)*cosW)
	b2 := A * ((A + 1) + (A-1)*cosW - 2*sqA*alpha)
	a0 := (A + 1) - (A-1)*cosW + 2*sqA*alpha
	a1 := 2 * ((A - 1) - (A+1)*cosW)
	a2 := (A + 1) - (A-1)*cosW - 2*sqA*alpha
	return biquadCoeffs{b0 / a0, b1 / a0, b2 / a0, a1 / a0, a2 / a0}
}

// peaking — Audio EQ Cookbook peaking EQ.
func peaking(dBgain, fc, Q, fs float64) biquadCoeffs {
	A := math.Pow(10, dBgain/40)
	w0 := 2 * math.Pi * fc / fs
	cosW, sinW := math.Cos(w0), math.Sin(w0)
	alpha := sinW / (2 * Q)
	b0 := 1 + alpha*A
	b1 := -2 * cosW
	b2 := 1 - alpha*A
	a0 := 1 + alpha/A
	a1 := -2 * cosW
	a2 := 1 - alpha/A
	return biquadCoeffs{b0 / a0, b1 / a0, b2 / a0, a1 / a0, a2 / a0}
}

// ── EQ graph widget ───────────────────────────────────────────────────────────

const (
	eqFS      = 48000.0
	eqDBRange = 18.0
)

// eqGraphSettings holds displayable EQ parameters.
type eqGraphSettings struct {
	BassGain, BassHz                    float64
	MidGain, MidHz, MidQ                float64
	TrebGain, TrebHz                    float64
	PresenceGain, PresenceHz, PresenceQ float64
}

// Default mock settings — slight V-shape with presence boost.
var mockEQDefaults = eqGraphSettings{
	BassGain: 3.0, BassHz: 80,
	MidGain: -0.5, MidHz: 800, MidQ: 1.2,
	TrebGain: 2.0, TrebHz: 8000,
	PresenceGain: 2.0, PresenceHz: 3000, PresenceQ: 1.5,
}

func computeEQResponseGains(width int, eq eqGraphSettings) []float64 {
	if width < 1 {
		width = 1
	}
	bass := lowShelf(eq.BassGain, eq.BassHz, eqFS)
	mid := peaking(eq.MidGain, eq.MidHz, eq.MidQ, eqFS)
	treb := highShelf(eq.TrebGain, eq.TrebHz, eqFS)
	pres := peaking(eq.PresenceGain, eq.PresenceHz, eq.PresenceQ, eqFS)

	gains := make([]float64, width)
	for c := 0; c < width; c++ {
		t := float64(c) / float64(width-1)
		freq := 20.0 * math.Pow(1000.0, t) // 20 Hz → 20 kHz log
		gains[c] = bass.magDB(freq, eqFS) +
			mid.magDB(freq, eqFS) +
			treb.magDB(freq, eqFS) +
			pres.magDB(freq, eqFS)
	}
	return gains
}

func eqPixelColor(x, y, width, height int, gains []float64) color.Color {
	if x >= len(gains) {
		return colSurface
	}
	gainDB := gains[x]
	if gainDB > eqDBRange {
		gainDB = eqDBRange
	} else if gainDB < -eqDBRange {
		gainDB = -eqDBRange
	}

	zeroRow := height / 2
	curveRow := zeroRow - int(math.Round(gainDB/eqDBRange*float64(zeroRow)))
	if curveRow < 0 {
		curveRow = 0
	} else if curveRow >= height {
		curveRow = height - 1
	}

	// Grid lines at ±6 and ±12 dB — very subtle.
	for _, db := range []float64{-12, -6, 6, 12} {
		gr := zeroRow - int(math.Round(db/eqDBRange*float64(zeroRow)))
		if y == gr {
			return color.NRGBA{R: 0x1c, G: 0x14, B: 0x28, A: 0xff}
		}
	}

	// 0 dB line — dim purple.
	if y == zeroRow {
		return color.NRGBA{R: 0x2a, G: 0x1e, B: 0x3c, A: 0xff}
	}

	// Curve — 2px thick.
	if y >= curveRow-1 && y <= curveRow+1 {
		if gainDB >= 0 {
			return colAccent
		}
		return colAmber
	}

	return colSurface
}

// precomputeEQImage renders the full EQ response curve into an *image.NRGBA.
// This is called once per (width, height, settingsVer) combination so the
// per-pixel raster callback degrades to a cheap array lookup.
func precomputeEQImage(width, height int, eq eqGraphSettings) *image.NRGBA {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	gains := computeEQResponseGains(width, eq)
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			c := eqPixelColor(x, y, width, height, gains)
			nc := color.NRGBAModel.Convert(c).(color.NRGBA)
			img.SetNRGBA(x, y, nc)
		}
	}
	return img
}

// EQGraphWidget renders a frequency response curve using canvas.Raster.
// It caches a precomputed *image.NRGBA keyed on (width, height, settingsVer)
// to avoid recomputing on every pixel callback — eliminating the resize lag.
type EQGraphWidget struct {
	widget.BaseWidget
	Settings    eqGraphSettings
	settingsVer int
}

// UpdateSettings replaces the EQ settings, invalidates the image cache, and
// triggers a redraw.
func (w *EQGraphWidget) UpdateSettings(s eqGraphSettings) {
	w.Settings = s
	w.settingsVer++
	w.Refresh()
}

func NewEQGraphWidget() *EQGraphWidget {
	w := &EQGraphWidget{Settings: mockEQDefaults}
	w.ExtendBaseWidget(w)
	return w
}

func (w *EQGraphWidget) CreateRenderer() fyne.WidgetRenderer {
	var cachedW, cachedH, cachedVer int
	var img *image.NRGBA

	raster := canvas.NewRasterWithPixels(func(px, py, pw, ph int) color.Color {
		if pw != cachedW || ph != cachedH || w.settingsVer != cachedVer || img == nil {
			cachedW, cachedH, cachedVer = pw, ph, w.settingsVer
			img = precomputeEQImage(pw, ph, w.Settings)
		}
		return img.NRGBAAt(px, py)
	})
	raster.SetMinSize(fyne.NewSize(0, 100))
	return widget.NewSimpleRenderer(raster)
}
