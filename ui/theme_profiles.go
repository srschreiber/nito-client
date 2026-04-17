// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package main

import (
	"image/color"

	"fyne.io/fyne/v2"
)

// ColorProfile defines the complete accent-derived palette for one theme variant.
// Static colors (colBg, colText, colGreen, colAmber, etc.) never change.
type ColorProfile struct {
	Name        string      // preference key, e.g. "purple"
	Accent      color.NRGBA // primary highlight
	AccentDark  color.NRGBA // dark version — button/circle bg
	Border      color.NRGBA // unfocused border
	BorderFocus color.NRGBA // focused border / hover ring
	Sep         color.NRGBA // separator line
	Dim         color.NRGBA // dim text
	DimMid      color.NRGBA // mid-dim text
	Surface     color.NRGBA // panel / card bg
	Surface2    color.NRGBA // raised surface (input, sidebar)
	TabActive   color.NRGBA // active tab indicator
	InputBg     color.NRGBA // text-entry background
	Hover       color.NRGBA // hover / active-tab overlay
}

// colorProfiles lists all built-in presets. First entry is the default.
var colorProfiles = []ColorProfile{
	// ── Chill Purple ─────────────────────────────────────────────────────────
	{
		Name:        "purple",
		Accent:      color.NRGBA{R: 0x8b, G: 0x5c, B: 0xf6, A: 0xff},
		AccentDark:  color.NRGBA{R: 0x4c, G: 0x2d, B: 0x91, A: 0xff},
		Border:      color.NRGBA{R: 0x3e, G: 0x2e, B: 0x58, A: 0xff},
		BorderFocus: color.NRGBA{R: 0x70, G: 0x40, B: 0xc0, A: 0xff},
		Sep:         color.NRGBA{R: 0x2c, G: 0x20, B: 0x40, A: 0xff},
		Dim:         color.NRGBA{R: 0x80, G: 0x78, B: 0xa0, A: 0xff},
		DimMid:      color.NRGBA{R: 0xa8, G: 0x9e, B: 0xc8, A: 0xff},
		Surface:     color.NRGBA{R: 0x1c, G: 0x18, B: 0x26, A: 0xff},
		Surface2:    color.NRGBA{R: 0x26, G: 0x20, B: 0x30, A: 0xff},
		TabActive:   color.NRGBA{R: 0x2c, G: 0x24, B: 0x38, A: 0xff},
		InputBg:     color.NRGBA{R: 0x20, G: 0x1c, B: 0x2c, A: 0xff},
		Hover:       color.NRGBA{R: 0x40, G: 0x2a, B: 0x68, A: 0xff},
	},
	// ── Chill Blue ───────────────────────────────────────────────────────────
	{
		Name:        "blue",
		Accent:      color.NRGBA{R: 0x60, G: 0xa5, B: 0xfa, A: 0xff},
		AccentDark:  color.NRGBA{R: 0x1e, G: 0x3a, B: 0x8a, A: 0xff},
		Border:      color.NRGBA{R: 0x2c, G: 0x3c, B: 0x52, A: 0xff},
		BorderFocus: color.NRGBA{R: 0x50, G: 0x90, B: 0xe8, A: 0xff},
		Sep:         color.NRGBA{R: 0x20, G: 0x2e, B: 0x44, A: 0xff},
		Dim:         color.NRGBA{R: 0x70, G: 0x90, B: 0xa8, A: 0xff},
		DimMid:      color.NRGBA{R: 0x96, G: 0xb4, B: 0xc8, A: 0xff},
		Surface:     color.NRGBA{R: 0x18, G: 0x20, B: 0x2c, A: 0xff},
		Surface2:    color.NRGBA{R: 0x20, G: 0x2a, B: 0x38, A: 0xff},
		TabActive:   color.NRGBA{R: 0x24, G: 0x2e, B: 0x42, A: 0xff},
		InputBg:     color.NRGBA{R: 0x1c, G: 0x24, B: 0x34, A: 0xff},
		Hover:       color.NRGBA{R: 0x2a, G: 0x48, B: 0x70, A: 0xff},
	},
	// ── Chill Red ────────────────────────────────────────────────────────────
	{
		Name:        "red",
		Accent:      color.NRGBA{R: 0xf8, G: 0x71, B: 0x71, A: 0xff},
		AccentDark:  color.NRGBA{R: 0x7f, G: 0x1d, B: 0x1d, A: 0xff},
		Border:      color.NRGBA{R: 0x44, G: 0x28, B: 0x28, A: 0xff},
		BorderFocus: color.NRGBA{R: 0xcc, G: 0x28, B: 0x28, A: 0xff},
		Sep:         color.NRGBA{R: 0x34, G: 0x20, B: 0x20, A: 0xff},
		Dim:         color.NRGBA{R: 0x8a, G: 0x68, B: 0x68, A: 0xff},
		DimMid:      color.NRGBA{R: 0xb4, G: 0x90, B: 0x90, A: 0xff},
		Surface:     color.NRGBA{R: 0x20, G: 0x18, B: 0x18, A: 0xff},
		Surface2:    color.NRGBA{R: 0x2c, G: 0x20, B: 0x20, A: 0xff},
		TabActive:   color.NRGBA{R: 0x34, G: 0x24, B: 0x24, A: 0xff},
		InputBg:     color.NRGBA{R: 0x28, G: 0x1c, B: 0x1c, A: 0xff},
		Hover:       color.NRGBA{R: 0x50, G: 0x28, B: 0x28, A: 0xff},
	},
	// ── Chill Green ──────────────────────────────────────────────────────────
	{
		Name:        "green",
		Accent:      color.NRGBA{R: 0x34, G: 0xd3, B: 0x99, A: 0xff},
		AccentDark:  color.NRGBA{R: 0x06, G: 0x5f, B: 0x46, A: 0xff},
		Border:      color.NRGBA{R: 0x28, G: 0x3e, B: 0x30, A: 0xff},
		BorderFocus: color.NRGBA{R: 0x18, G: 0xb0, B: 0x7c, A: 0xff},
		Sep:         color.NRGBA{R: 0x1e, G: 0x2c, B: 0x24, A: 0xff},
		Dim:         color.NRGBA{R: 0x6a, G: 0x88, B: 0x78, A: 0xff},
		DimMid:      color.NRGBA{R: 0x90, G: 0xb0, B: 0xa8, A: 0xff},
		Surface:     color.NRGBA{R: 0x18, G: 0x1e, B: 0x1a, A: 0xff},
		Surface2:    color.NRGBA{R: 0x20, G: 0x2a, B: 0x22, A: 0xff},
		TabActive:   color.NRGBA{R: 0x26, G: 0x34, B: 0x2c, A: 0xff},
		InputBg:     color.NRGBA{R: 0x1c, G: 0x26, B: 0x1e, A: 0xff},
		Hover:       color.NRGBA{R: 0x28, G: 0x4e, B: 0x38, A: 0xff},
	},
	// ── Chill Orange ─────────────────────────────────────────────────────────
	{
		Name:        "orange",
		Accent:      color.NRGBA{R: 0xfb, G: 0x92, B: 0x3c, A: 0xff},
		AccentDark:  color.NRGBA{R: 0x7c, G: 0x2d, B: 0x12, A: 0xff},
		Border:      color.NRGBA{R: 0x3c, G: 0x2c, B: 0x1c, A: 0xff},
		BorderFocus: color.NRGBA{R: 0xd4, G: 0x50, B: 0x14, A: 0xff},
		Sep:         color.NRGBA{R: 0x28, G: 0x20, B: 0x1a, A: 0xff},
		Dim:         color.NRGBA{R: 0x88, G: 0x78, B: 0x70, A: 0xff},
		DimMid:      color.NRGBA{R: 0xb8, G: 0xa4, B: 0x8e, A: 0xff},
		Surface:     color.NRGBA{R: 0x20, G: 0x1a, B: 0x16, A: 0xff},
		Surface2:    color.NRGBA{R: 0x2c, G: 0x24, B: 0x20, A: 0xff},
		TabActive:   color.NRGBA{R: 0x34, G: 0x2c, B: 0x24, A: 0xff},
		InputBg:     color.NRGBA{R: 0x28, G: 0x1e, B: 0x18, A: 0xff},
		Hover:       color.NRGBA{R: 0x50, G: 0x30, B: 0x28, A: 0xff},
	},
	// ── Chill Teal ───────────────────────────────────────────────────────────
	{
		Name:        "teal",
		Accent:      color.NRGBA{R: 0x2d, G: 0xd4, B: 0xbf, A: 0xff},
		AccentDark:  color.NRGBA{R: 0x13, G: 0x4e, B: 0x4a, A: 0xff},
		Border:      color.NRGBA{R: 0x26, G: 0x40, B: 0x3e, A: 0xff},
		BorderFocus: color.NRGBA{R: 0x18, G: 0xa8, B: 0x9a, A: 0xff},
		Sep:         color.NRGBA{R: 0x1e, G: 0x2c, B: 0x2c, A: 0xff},
		Dim:         color.NRGBA{R: 0x6e, G: 0x8a, B: 0x88, A: 0xff},
		DimMid:      color.NRGBA{R: 0x96, G: 0xb4, B: 0xb2, A: 0xff},
		Surface:     color.NRGBA{R: 0x18, G: 0x1e, B: 0x1e, A: 0xff},
		Surface2:    color.NRGBA{R: 0x20, G: 0x2c, B: 0x2c, A: 0xff},
		TabActive:   color.NRGBA{R: 0x26, G: 0x36, B: 0x36, A: 0xff},
		InputBg:     color.NRGBA{R: 0x1c, G: 0x28, B: 0x28, A: 0xff},
		Hover:       color.NRGBA{R: 0x28, G: 0x52, B: 0x50, A: 0xff},
	},
	// ── Chill Pink ───────────────────────────────────────────────────────────
	{
		Name:        "pink",
		Accent:      color.NRGBA{R: 0xf4, G: 0x72, B: 0xb6, A: 0xff},
		AccentDark:  color.NRGBA{R: 0x83, G: 0x18, B: 0x43, A: 0xff},
		Border:      color.NRGBA{R: 0x3e, G: 0x28, B: 0x34, A: 0xff},
		BorderFocus: color.NRGBA{R: 0xd0, G: 0x20, B: 0x70, A: 0xff},
		Sep:         color.NRGBA{R: 0x30, G: 0x20, B: 0x30, A: 0xff},
		Dim:         color.NRGBA{R: 0x8a, G: 0x70, B: 0x90, A: 0xff},
		DimMid:      color.NRGBA{R: 0xb4, G: 0x94, B: 0xbc, A: 0xff},
		Surface:     color.NRGBA{R: 0x20, G: 0x16, B: 0x20, A: 0xff},
		Surface2:    color.NRGBA{R: 0x2c, G: 0x22, B: 0x2c, A: 0xff},
		TabActive:   color.NRGBA{R: 0x34, G: 0x28, B: 0x34, A: 0xff},
		InputBg:     color.NRGBA{R: 0x28, G: 0x1e, B: 0x28, A: 0xff},
		Hover:       color.NRGBA{R: 0x50, G: 0x26, B: 0x4c, A: 0xff},
	},
}

// activeProfileName is the name of the currently active profile.
var activeProfileName = "purple"

// brightnessScale is a multiplier applied to all surface/dim/border colors when
// a profile is set. Accent and AccentDark are intentionally excluded so the hue
// stays vivid at any brightness. Range roughly 0.5–1.8; default 1.0.
var brightnessScale float32 = 1.0

// scaledCol multiplies the RGB channels of c by brightnessScale, clamping to 255.
func scaledCol(c color.NRGBA) color.NRGBA {
	if brightnessScale == 1.0 {
		return c
	}
	clamp := func(v float32) uint8 {
		if v >= 255 {
			return 255
		}
		if v <= 0 {
			return 0
		}
		return uint8(v)
	}
	return color.NRGBA{
		R: clamp(float32(c.R) * brightnessScale),
		G: clamp(float32(c.G) * brightnessScale),
		B: clamp(float32(c.B) * brightnessScale),
		A: c.A,
	}
}

// ── Theme-change listener registry ───────────────────────────────────────────
//
// Custom widgets that store canvas-object colors (not going through nitoTheme)
// register here. notifyThemeListeners() is called on the Fyne main thread after
// every profile change, so listeners can update their canvas objects.

var themeListeners []func()

func registerThemeListener(fn func()) {
	themeListeners = append(themeListeners, fn)
}

func notifyThemeListeners() {
	for _, fn := range themeListeners {
		fn()
	}
}

// ── Profile lookup ────────────────────────────────────────────────────────────

func profileByName(name string) ColorProfile {
	for _, p := range colorProfiles {
		if p.Name == name {
			return p
		}
	}
	return colorProfiles[0] // default purple
}

// ── Apply ─────────────────────────────────────────────────────────────────────

// setProfileColors updates all mutable col* globals to match the named profile,
// applying the current brightnessScale to surface/dim/border colors.
// Does NOT call SetTheme or notify listeners — use applyColorProfile for that.
func setProfileColors(name string) {
	p := profileByName(name)
	activeProfileName = p.Name
	// Accent and AccentDark are hue-defining — skip brightness scaling.
	colAccent = p.Accent
	colAccentDark = p.AccentDark
	// All other colors are scaled by the current brightness multiplier.
	colBorder = scaledCol(p.Border)
	colBorderFocus = scaledCol(p.BorderFocus)
	colSep = scaledCol(p.Sep)
	colDim = scaledCol(p.Dim)
	colDimMid = scaledCol(p.DimMid)
	colSurface = scaledCol(p.Surface)
	colSurface2 = scaledCol(p.Surface2)
	colTabActive = scaledCol(p.TabActive)
	colInputBg = scaledCol(p.InputBg)
	colHover = scaledCol(p.Hover)
}

// applyColorProfile applies a named profile, refreshes the Fyne theme, saves
// the preference, and notifies all registered theme listeners. Must be called
// on the Fyne main thread.
func applyColorProfile(name string) {
	setProfileColors(name)
	a := fyne.CurrentApp()
	if a != nil {
		a.Preferences().SetString("colorProfile", name)
		a.Settings().SetTheme(nitoTheme{})
	}
	notifyThemeListeners()
}

// applyBrightness sets the brightness multiplier, saves the preference, and
// re-applies the current profile. Must be called on the Fyne main thread.
func applyBrightness(scale float32) {
	brightnessScale = scale
	a := fyne.CurrentApp()
	if a != nil {
		a.Preferences().SetFloat("brightness", float64(scale))
	}
	applyColorProfile(activeProfileName)
}

// loadSavedProfile reads the user's saved profile and brightness from
// preferences and applies them to the global col* vars (without triggering a
// theme refresh — the app's initial SetTheme call handles that).
func loadSavedProfile() {
	a := fyne.CurrentApp()
	if a == nil {
		return
	}
	brightnessScale = float32(a.Preferences().FloatWithFallback("brightness", 1.0))
	name := a.Preferences().StringWithFallback("colorProfile", "purple")
	setProfileColors(name)
}
