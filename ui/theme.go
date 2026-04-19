// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// ── Palette ───────────────────────────────────────────────────────────────────
// Kept deliberately low-contrast and purple-tinted. Bright red/green only
// appears where it carries real meaning (online status, errors).

var (
	colBg          = color.NRGBA{R: 0x0d, G: 0x0d, B: 0x0d, A: 0xff} // window bg
	colSurface     = color.NRGBA{R: 0x1c, G: 0x18, B: 0x26, A: 0xff} // panel/card bg
	colSurface2    = color.NRGBA{R: 0x26, G: 0x20, B: 0x30, A: 0xff} // raised surface (input, sidebar)
	colBorder      = color.NRGBA{R: 0x3e, G: 0x2e, B: 0x58, A: 0xff} // unfocused border — dark purple
	colBorderFocus = color.NRGBA{R: 0x70, G: 0x40, B: 0xc0, A: 0xff} // focused border — medium purple
	colSep         = color.NRGBA{R: 0x2c, G: 0x20, B: 0x40, A: 0xff} // separator line
	colText        = color.NRGBA{R: 0xcc, G: 0xcc, B: 0xd8, A: 0xff} // main text (cool white)
	colDim         = color.NRGBA{R: 0x80, G: 0x78, B: 0xa0, A: 0xff} // dim — purple-tinted gray
	colDimMid      = color.NRGBA{R: 0xa8, G: 0x9e, B: 0xc8, A: 0xff} // mid dim
	colAccent      = color.NRGBA{R: 0x8b, G: 0x5c, B: 0xf6, A: 0xff} // primary — soft violet
	colAccentDark  = color.NRGBA{R: 0x4c, G: 0x2d, B: 0x91, A: 0xff} // darker violet
	colCyan        = color.NRGBA{R: 0x67, G: 0xe8, B: 0xf9, A: 0xff} // self messages / labels
	colLavender    = color.NRGBA{R: 0xc4, G: 0xb5, B: 0xfd, A: 0xff} // other users in chat
	colGreen       = color.NRGBA{R: 0x34, G: 0xd3, B: 0x99, A: 0xff} // online / success (soft emerald)
	colAmber       = color.NRGBA{R: 0xfb, G: 0xbf, B: 0x24, A: 0xff} // warning
	colAlert       = color.NRGBA{R: 0xff, G: 0x3b, B: 0x30, A: 0xff} // "you're about to hurt yourself" — profile-independent; saturated enough to stand out against red/pink profiles
	colMuted       = color.NRGBA{R: 0x6b, G: 0x72, B: 0x80, A: 0xff} // system messages
	colTransparent = color.NRGBA{A: 0x00}
	colTabActive   = color.NRGBA{R: 0x2c, G: 0x24, B: 0x38, A: 0xff} // active tab bg
	colInputBg     = color.NRGBA{R: 0x20, G: 0x1c, B: 0x2c, A: 0xff}
	colHover       = color.NRGBA{R: 0x40, G: 0x2a, B: 0x68, A: 0xff} // button/item hover
)

// ── Live-colour wrappers ──────────────────────────────────────────────────────
//
// liveCol implements color.Color by reading through a pointer to a col* global.
// Canvas objects constructed with a liveXxx value will repaint with the correct
// profile colour after applyColorProfile() without any explicit per-widget update.

type liveCol struct{ p *color.NRGBA }

func (c liveCol) RGBA() (r, g, b, a uint32) { return c.p.RGBA() }

// One live reference per profile-sensitive global, in the same order they are
// declared above so Go initialises them in the right dependency order.
var (
	liveSep         = liveCol{&colSep}
	liveAccent      = liveCol{&colAccent}
	liveAccentDark  = liveCol{&colAccentDark}
	liveDim         = liveCol{&colDim}
	liveDimMid      = liveCol{&colDimMid}
	liveSurface     = liveCol{&colSurface}
	liveSurface2    = liveCol{&colSurface2}
	liveBorder      = liveCol{&colBorder}
	liveBorderFocus = liveCol{&colBorderFocus}
	liveHover       = liveCol{&colHover}
	liveInputBg     = liveCol{&colInputBg}
	liveTabActive   = liveCol{&colTabActive}
)

// ── Theme ─────────────────────────────────────────────────────────────────────

// colorNameAlert is a custom theme color name resolved by nitoTheme. Used by
// RichText segments that should render in the profile-independent colAlert.
const colorNameAlert fyne.ThemeColorName = "nitoAlert"

type nitoTheme struct{}

func (nitoTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	switch n {
	case colorNameAlert:
		return colAlert
	case theme.ColorNameBackground:
		return colBg
	case theme.ColorNameForeground:
		return colText
	case theme.ColorNamePrimary:
		return colAccent
	case theme.ColorNameFocus:
		return colBorderFocus // focus rings, dropdown selection, slider drag thumb
	case theme.ColorNameHover:
		return colHover
	case theme.ColorNameInputBackground:
		return colInputBg
	case theme.ColorNameButton:
		return colSurface2
	case theme.ColorNameDisabled:
		return colDim
	case theme.ColorNamePlaceHolder:
		return colDim
	case theme.ColorNameShadow:
		return color.NRGBA{R: 0x08, G: 0x04, B: 0x10, A: 0xaa}
	case theme.ColorNameSeparator:
		return colSep
	case theme.ColorNameScrollBar:
		return colBorder
	}
	return theme.DefaultTheme().Color(n, v)
}

func (nitoTheme) Font(style fyne.TextStyle) fyne.Resource {
	return theme.DefaultTheme().Font(style)
}

func (nitoTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return theme.DefaultTheme().Icon(name)
}

// sizeNameChatEmoji is a custom theme size name used by the chat message
// renderer to render emoji runs roughly 2× body text size.
const sizeNameChatEmoji fyne.ThemeSizeName = "nitoChatEmoji"

func (nitoTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case sizeNameChatEmoji:
		return 36 // ~2.8x SizeNameText — used for emoji-only messages, so no mixed-size baseline issues
	case theme.SizeNameText:
		return 13
	case theme.SizeNamePadding:
		return 5
	case theme.SizeNameInnerPadding:
		return 6
	case theme.SizeNameInlineIcon:
		return 22 // slightly larger slider thumb
	case theme.SizeNameScrollBar:
		return 5
	case theme.SizeNameScrollBarSmall:
		return 3
	case theme.SizeNameInputBorder:
		return 2 // thicker cursor (also controls input border stroke width)
	}
	return theme.DefaultTheme().Size(name)
}
