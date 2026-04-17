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
		Border:      color.NRGBA{R: 0x2a, G: 0x1e, B: 0x3c, A: 0xff},
		BorderFocus: color.NRGBA{R: 0x5b, G: 0x2d, B: 0x9e, A: 0xff},
		Sep:         color.NRGBA{R: 0x1e, G: 0x16, B: 0x2a, A: 0xff},
		Dim:         color.NRGBA{R: 0x52, G: 0x4e, B: 0x66, A: 0xff},
		DimMid:      color.NRGBA{R: 0x78, G: 0x74, B: 0x96, A: 0xff},
		Surface:     color.NRGBA{R: 0x14, G: 0x12, B: 0x18, A: 0xff},
		Surface2:    color.NRGBA{R: 0x1c, G: 0x18, B: 0x24, A: 0xff},
		TabActive:   color.NRGBA{R: 0x20, G: 0x18, B: 0x2e, A: 0xff},
		InputBg:     color.NRGBA{R: 0x18, G: 0x14, B: 0x22, A: 0xff},
		Hover:       color.NRGBA{R: 0x32, G: 0x22, B: 0x50, A: 0xff},
	},
	// ── Chill Blue ───────────────────────────────────────────────────────────
	{
		Name:        "blue",
		Accent:      color.NRGBA{R: 0x60, G: 0xa5, B: 0xfa, A: 0xff},
		AccentDark:  color.NRGBA{R: 0x1e, G: 0x3a, B: 0x8a, A: 0xff},
		Border:      color.NRGBA{R: 0x1e, G: 0x29, B: 0x3b, A: 0xff},
		BorderFocus: color.NRGBA{R: 0x3b, G: 0x82, B: 0xf6, A: 0xff},
		Sep:         color.NRGBA{R: 0x16, G: 0x20, B: 0x31, A: 0xff},
		Dim:         color.NRGBA{R: 0x4e, G: 0x5e, B: 0x6e, A: 0xff},
		DimMid:      color.NRGBA{R: 0x74, G: 0x90, B: 0xa0, A: 0xff},
		Surface:     color.NRGBA{R: 0x12, G: 0x16, B: 0x20, A: 0xff},
		Surface2:    color.NRGBA{R: 0x18, G: 0x1e, B: 0x2c, A: 0xff},
		TabActive:   color.NRGBA{R: 0x1a, G: 0x22, B: 0x34, A: 0xff},
		InputBg:     color.NRGBA{R: 0x14, G: 0x1a, B: 0x28, A: 0xff},
		Hover:       color.NRGBA{R: 0x1e, G: 0x35, B: 0x58, A: 0xff},
	},
	// ── Chill Red ────────────────────────────────────────────────────────────
	{
		Name:        "red",
		Accent:      color.NRGBA{R: 0xf8, G: 0x71, B: 0x71, A: 0xff},
		AccentDark:  color.NRGBA{R: 0x7f, G: 0x1d, B: 0x1d, A: 0xff},
		Border:      color.NRGBA{R: 0x2d, G: 0x1a, B: 0x1a, A: 0xff},
		BorderFocus: color.NRGBA{R: 0xb9, G: 0x1c, B: 0x1c, A: 0xff},
		Sep:         color.NRGBA{R: 0x23, G: 0x14, B: 0x14, A: 0xff},
		Dim:         color.NRGBA{R: 0x66, G: 0x50, B: 0x50, A: 0xff},
		DimMid:      color.NRGBA{R: 0x96, G: 0x74, B: 0x74, A: 0xff},
		Surface:     color.NRGBA{R: 0x18, G: 0x12, B: 0x12, A: 0xff},
		Surface2:    color.NRGBA{R: 0x22, G: 0x1a, B: 0x1a, A: 0xff},
		TabActive:   color.NRGBA{R: 0x2a, G: 0x1c, B: 0x1c, A: 0xff},
		InputBg:     color.NRGBA{R: 0x1e, G: 0x14, B: 0x14, A: 0xff},
		Hover:       color.NRGBA{R: 0x3d, G: 0x1c, B: 0x1c, A: 0xff},
	},
	// ── Chill Green ──────────────────────────────────────────────────────────
	{
		Name:        "green",
		Accent:      color.NRGBA{R: 0x34, G: 0xd3, B: 0x99, A: 0xff},
		AccentDark:  color.NRGBA{R: 0x06, G: 0x5f, B: 0x46, A: 0xff},
		Border:      color.NRGBA{R: 0x1a, G: 0x2e, B: 0x24, A: 0xff},
		BorderFocus: color.NRGBA{R: 0x05, G: 0x96, B: 0x69, A: 0xff},
		Sep:         color.NRGBA{R: 0x14, G: 0x1e, B: 0x18, A: 0xff},
		Dim:         color.NRGBA{R: 0x4e, G: 0x66, B: 0x58, A: 0xff},
		DimMid:      color.NRGBA{R: 0x74, G: 0x90, B: 0x8a, A: 0xff},
		Surface:     color.NRGBA{R: 0x12, G: 0x18, B: 0x14, A: 0xff},
		Surface2:    color.NRGBA{R: 0x18, G: 0x24, B: 0x1c, A: 0xff},
		TabActive:   color.NRGBA{R: 0x1c, G: 0x2e, B: 0x22, A: 0xff},
		InputBg:     color.NRGBA{R: 0x14, G: 0x1e, B: 0x16, A: 0xff},
		Hover:       color.NRGBA{R: 0x1c, G: 0x3d, B: 0x2c, A: 0xff},
	},
}

// activeProfileName is the name of the currently active profile.
var activeProfileName = "purple"

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

// setProfileColors updates all mutable col* globals to match the named profile.
// Does NOT call SetTheme or notify listeners — use applyColorProfile for that.
func setProfileColors(name string) {
	p := profileByName(name)
	activeProfileName = p.Name
	colAccent = p.Accent
	colAccentDark = p.AccentDark
	colBorder = p.Border
	colBorderFocus = p.BorderFocus
	colSep = p.Sep
	colDim = p.Dim
	colDimMid = p.DimMid
	colSurface = p.Surface
	colSurface2 = p.Surface2
	colTabActive = p.TabActive
	colInputBg = p.InputBg
	colHover = p.Hover
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

// loadSavedProfile reads the user's saved profile from preferences and applies
// the colors to the global col* vars (without triggering a theme refresh —
// the app's initial SetTheme call handles that).
func loadSavedProfile() {
	a := fyne.CurrentApp()
	if a == nil {
		return
	}
	name := a.Preferences().StringWithFallback("colorProfile", "purple")
	setProfileColors(name)
}
