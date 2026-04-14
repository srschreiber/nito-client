// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package main

import (
	_ "embed"
	"strings"

	lipgloss "charm.land/lipgloss/v2"
	"github.com/srschreiber/nito-client/shellapp/styles"
)

//go:embed licenses/01_nito.txt
var licNito string

//go:embed licenses/02_webrtc_audio_processing.txt
var licWebrtcAPM string

//go:embed licenses/03_webrtc.txt
var licWebrtc string

//go:embed licenses/04_rnnoise.txt
var licRnnoise string

//go:embed licenses/05_signalsmith.txt
var licSignalsmith string

//go:embed licenses/06_abseil.txt
var licAbseil string

//go:embed licenses/07_ooura_fft.txt
var licOoura string

//go:embed licenses/08_pffft.txt
var licPffft string

//go:embed licenses/09_fft_olesen.txt
var licFftOlesen string

//go:embed licenses/10_spl_sqrt_floor.txt
var licSplSqrt string

//go:embed licenses/11_webrtc_rnnoise.txt
var licWebrtcRnnoise string

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
}

// aboutTruncate shortens s to at most maxW rune columns.
func aboutTruncate(s string, maxW int) string {
	if maxW <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= maxW {
		return s
	}
	if maxW <= 3 {
		return string(runes[:maxW])
	}
	return string(runes[:maxW-3]) + "..."
}

const aboutListW = 22 // inner content width of the license list pane

// renderAbout renders the about/licenses screen.
// focus: 0 = list pane active, 1 = text pane active.
func renderAbout(cursor, scroll, focus, termW, termH int) string {
	// ── sizing ───────────────────────────────────────────────────────────────
	totalW := termW - 8
	if totalW > 130 {
		totalW = 130
	}
	if totalW < 50 {
		totalW = 50
	}
	rightInnerW := totalW - (aboutListW + 4) - 1 // -1 gap between boxes
	if rightInnerW < 20 {
		rightInnerW = 20
	}
	// paneH is fixed for a given termH so Place never shifts.
	// Body height = 1 (title) + 1 (blank after title) + paneH+2 (box+border) + 1 (hint) = paneH+5.
	paneH := termH - 5
	if paneH < 5 {
		paneH = 5
	}
	// innerH is the number of content lines that fit inside the box
	// (border top + bottom = 2 rows consumed).
	innerH := paneH - 2
	if innerH < 1 {
		innerH = 1
	}

	// ── border colors ────────────────────────────────────────────────────────
	leftBorder := lipgloss.Color("#444")
	rightBorder := lipgloss.Color("#444")
	if focus == 0 {
		leftBorder = lipgloss.Color("#5b21b6")
	} else {
		rightBorder = lipgloss.Color("#5b21b6")
	}

	// ── left pane: license list ──────────────────────────────────────────────
	var leftLines []string
	for i, lic := range allLicenses {
		name := aboutTruncate(lic.name, aboutListW)
		if i == cursor {
			leftLines = append(leftLines, styles.CursorStyle.Render("▶ ")+name)
		} else {
			leftLines = append(leftLines, "  "+styles.DimText.Render(name))
		}
	}
	// Pad to exactly innerH so Height() never expands the box.
	for len(leftLines) < innerH {
		leftLines = append(leftLines, "")
	}
	leftBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(leftBorder).
		BorderBackground(styles.ComponentBg).
		Background(styles.ComponentBg).
		Padding(0, 1).
		Width(aboutListW + 4).
		Height(paneH).
		Render(strings.Join(leftLines[:innerH], "\n"))

	// ── right pane: license text ─────────────────────────────────────────────
	// Reserve the last row for the scroll indicator; content fills the rest.
	maxContent := innerH - 1
	if maxContent < 1 {
		maxContent = 1
	}

	var rightLines []string
	var indicatorLine string

	if cursor >= 0 && cursor < len(allLicenses) {
		lines := strings.Split(allLicenses[cursor].text, "\n")
		// Clamp scroll.
		maxScroll := len(lines) - maxContent
		if maxScroll < 0 {
			maxScroll = 0
		}
		if scroll > maxScroll {
			scroll = maxScroll
		}
		start := scroll
		for _, line := range lines[start:] {
			if len(rightLines) >= maxContent {
				break
			}
			rightLines = append(rightLines, aboutTruncate(line, rightInnerW))
		}
		// Scroll indicator (always occupies the last slot).
		hasMore := start+maxContent < len(lines)
		if scroll > 0 || hasMore {
			pct := 0
			if maxScroll > 0 {
				pct = scroll * 100 / maxScroll
			}
			indicatorLine = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#555")).
				Render(strings.Repeat(" ", rightInnerW-6) +
					lipgloss.NewStyle().Foreground(lipgloss.Color("#666")).Render("↕ "+itoa(pct)+"%"))
		}
	}
	// Pad content lines to maxContent, then append the indicator slot.
	for len(rightLines) < maxContent {
		rightLines = append(rightLines, "")
	}
	rightLines = append(rightLines, indicatorLine)

	rightBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(rightBorder).
		BorderBackground(styles.ComponentBg).
		Background(styles.ComponentBg).
		Padding(0, 1).
		Width(rightInnerW + 4).
		Height(paneH).
		Render(strings.Join(rightLines, "\n"))

	row := lipgloss.JoinHorizontal(lipgloss.Top, leftBox, rightBox)

	title := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#F25D94")).
		Bold(true).
		Render("About & Licenses")

	var hintLeft, hintRight string
	if focus == 0 {
		hintLeft = "↑/↓  navigate"
		hintRight = "tab  switch to text"
	} else {
		hintLeft = "↑/↓ j/k  scroll"
		hintRight = "tab  switch to list"
	}
	hint := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#555")).
		Render(hintLeft + "   " + hintRight + "   esc  back")

	// JoinVertical with explicit blank line between title and row keeps
	// body height = 1 + 1 + (paneH+2) + 1 = paneH+5, always fixed.
	body := lipgloss.JoinVertical(lipgloss.Left, title, "", row, hint)
	return lipgloss.Place(termW, termH, lipgloss.Center, lipgloss.Center, body)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := make([]byte, 0, 3)
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	return string(buf)
}
