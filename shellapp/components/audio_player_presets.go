// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package components

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/srschreiber/nito-client/shellapp/styles"
	"github.com/srschreiber/nito-client/shellapp/voice"
)

// ShowAudioPlayerPresetsMsg opens the preset selector screen.
type ShowAudioPlayerPresetsMsg struct{}

// HideAudioPlayerPresetsMsg closes the preset selector screen.
type HideAudioPlayerPresetsMsg struct{}

// audioPreset defines one named preset.
type audioPreset struct {
	name    string // display name
	tagline string // one-line vibe description
	tags    string // active effects summary e.g. "EQ · Reverb"
	apply   func() // applies all settings and saves
}

// noFX returns sensible "off" states for delay/reverb/chorus/pan.
// Preset apply functions call this then layer any FX they need on top.
func noFX() (voice.DelaySettings, voice.ReverbSettings, voice.ChorusSettings, voice.PannerSettings) {
	del := voice.DelaySettings{Enabled: false, DelayMs: 55, Feedback: 0.3}
	rev := voice.ReverbSettings{Enabled: false, Mix: 0.2, Size: 1.0, Decay: 0.5, Tone: 0.5}
	cho := voice.ChorusSettings{Enabled: false, BaseDelayMs: 15, RateHz: 0.5, DepthMs: 3.0, Mix: 0.2}
	// AutoPanRate must be >0 so LoadAudioSettings recognises the field as written.
	pan := voice.PannerSettings{Balance: 0, AutoPanEnabled: false, AutoPanRate: 1.0, AutoPanDepth: 0.5}
	return del, rev, cho, pan
}

// applyPreset sets EQ + FX settings and saves, preserving volume and pitch.
func applyPreset(eq voice.EQSettings,
	del voice.DelaySettings, rev voice.ReverbSettings,
	cho voice.ChorusSettings, pan voice.PannerSettings) {
	voice.SetPlaybackEQSettings(eq)
	voice.SetDelaySettings(del)
	voice.SetReverbSettings(rev)
	voice.SetChorusSettings(cho)
	voice.SetPannerSettings(pan)
	voice.SaveAudioSettings()
}

// audioPresetList contains all available presets in alphabetical order.
var audioPresetList = []audioPreset{
	// ── Airy ─────────────────────────────────────────────────────────────────
	{
		name:    "Airy",
		tagline: "Light & ethereal",
		tags:    "EQ · Chorus",
		apply: func() {
			del, rev, _, pan := noFX()
			cho := voice.ChorusSettings{
				Enabled: true, BaseDelayMs: 22, RateHz: 0.25, DepthMs: 2.5, Mix: 0.14,
			}
			applyPreset(voice.EQSettings{
				BassGain: -2.0, BassHz: 120,
				MidGain: -1.0, MidHz: 1000, MidQ: 1.0,
				TrebleGain: 3.0, TrebleHz: 10000,
				PresenceGain: 3.0, PresenceHz: 4500, PresenceQ: 0.9,
			}, del, rev, cho, pan)
		},
	},
	// ── Auto Pan ─────────────────────────────────────────────────────────────
	{
		name:    "Auto Pan",
		tagline: "Rhythmic stereo sweep",
		tags:    "Auto-Pan",
		apply: func() {
			del, rev, cho, _ := noFX()
			pan := voice.PannerSettings{
				Balance: 0, AutoPanEnabled: true, AutoPanRate: 1.5, AutoPanDepth: 0.7,
			}
			applyPreset(voice.EQSettings{
				BassGain: 0.0, BassHz: 120,
				MidGain: 0.0, MidHz: 1000, MidQ: 1.0,
				TrebleGain: 0.5, TrebleHz: 5000,
				PresenceGain: 1.0, PresenceHz: 3000, PresenceQ: 1.0,
			}, del, rev, cho, pan)
		},
	},
	// ── Balanced ─────────────────────────────────────────────────────────────
	{
		name:    "Balanced",
		tagline: "Gentle V-curve",
		tags:    "EQ only",
		apply: func() {
			del, rev, cho, pan := noFX()
			applyPreset(voice.EQSettings{
				BassGain: 2.0, BassHz: 120,
				MidGain: -0.5, MidHz: 1000, MidQ: 1.0,
				TrebleGain: 1.5, TrebleHz: 5000,
				PresenceGain: 2.0, PresenceHz: 3000, PresenceQ: 1.0,
			}, del, rev, cho, pan)
		},
	},
	// ── Bass Boost ───────────────────────────────────────────────────────────
	{
		name:    "Bass Boost",
		tagline: "Heavy low end",
		tags:    "EQ only",
		apply: func() {
			del, rev, cho, pan := noFX()
			applyPreset(voice.EQSettings{
				BassGain: 3.0, BassHz: 80,
				MidGain: -1.0, MidHz: 1000, MidQ: 1.0,
				TrebleGain: 0.0, TrebleHz: 5000,
				PresenceGain: 0.0, PresenceHz: 3000, PresenceQ: 1.0,
			}, del, rev, cho, pan)
		},
	},
	// ── Bright ───────────────────────────────────────────────────────────────
	{
		name:    "Bright",
		tagline: "Crispy highs",
		tags:    "EQ only",
		apply: func() {
			del, rev, cho, pan := noFX()
			applyPreset(voice.EQSettings{
				BassGain: -1.0, BassHz: 120,
				MidGain: 0.0, MidHz: 1000, MidQ: 1.0,
				TrebleGain: 3.0, TrebleHz: 9000,
				PresenceGain: 3.0, PresenceHz: 4000, PresenceQ: 1.2,
			}, del, rev, cho, pan)
		},
	},
	// ── Chill ────────────────────────────────────────────────────────────────
	{
		name:    "Chill",
		tagline: "Warm & relaxed",
		tags:    "EQ · Reverb",
		apply: func() {
			del, _, cho, pan := noFX()
			rev := voice.ReverbSettings{
				Enabled: true, Mix: 0.15, Size: 1.3, Decay: 0.55, Tone: 0.35,
			}
			applyPreset(voice.EQSettings{
				BassGain: 2.5, BassHz: 100,
				MidGain: -0.5, MidHz: 1000, MidQ: 1.0,
				TrebleGain: -2.5, TrebleHz: 5000,
				PresenceGain: -1.0, PresenceHz: 3000, PresenceQ: 1.0,
			}, del, rev, cho, pan)
		},
	},
	// ── Delay Echo ───────────────────────────────────────────────────────────
	{
		name:    "Delay Echo",
		tagline: "Bouncy echo repeats",
		tags:    "EQ · Delay",
		apply: func() {
			_, rev, cho, pan := noFX()
			del := voice.DelaySettings{Enabled: true, DelayMs: 250, Feedback: 0.42}
			applyPreset(voice.EQSettings{
				BassGain: 0.5, BassHz: 120,
				MidGain: -0.5, MidHz: 1000, MidQ: 1.0,
				TrebleGain: 2.0, TrebleHz: 7000,
				PresenceGain: 1.5, PresenceHz: 3500, PresenceQ: 1.0,
			}, del, rev, cho, pan)
		},
	},
	// ── Flat ─────────────────────────────────────────────────────────────────
	{
		name:    "Flat",
		tagline: "No processing",
		tags:    "Clean slate",
		apply: func() {
			del, rev, cho, pan := noFX()
			applyPreset(voice.EQSettings{
				BassGain: 0.0, BassHz: 120,
				MidGain: 0.0, MidHz: 1000, MidQ: 1.0,
				TrebleGain: 0.0, TrebleHz: 5000,
				PresenceGain: 0.0, PresenceHz: 3000, PresenceQ: 1.0,
			}, del, rev, cho, pan)
		},
	},
	// ── Immersive ────────────────────────────────────────────────────────────
	{
		name:    "Immersive",
		tagline: "Spacious & enveloping",
		tags:    "EQ · Reverb · Chorus",
		apply: func() {
			del, _, _, _ := noFX()
			pan := voice.PannerSettings{Balance: 0, AutoPanEnabled: true, AutoPanRate: 0.3, AutoPanDepth: 0.3}
			rev := voice.ReverbSettings{
				Enabled: true, Mix: 0.28, Size: 1.8, Decay: 0.72, Tone: 0.55,
			}
			cho := voice.ChorusSettings{
				Enabled: true, BaseDelayMs: 18, RateHz: 0.35, DepthMs: 3.5, Mix: 0.18,
			}
			applyPreset(voice.EQSettings{
				BassGain: 2.0, BassHz: 120,
				MidGain: -0.5, MidHz: 1000, MidQ: 1.0,
				TrebleGain: 2.0, TrebleHz: 5000,
				PresenceGain: 2.5, PresenceHz: 3500, PresenceQ: 1.0,
			}, del, rev, cho, pan)
		},
	},
	// ── Lo-Fi ────────────────────────────────────────────────────────────────
	{
		name:    "Lo-Fi",
		tagline: "Warm, dusty & nostalgic",
		tags:    "EQ · Reverb · Delay",
		apply: func() {
			_, _, cho, pan := noFX()
			del := voice.DelaySettings{Enabled: true, DelayMs: 80, Feedback: 0.2}
			rev := voice.ReverbSettings{Enabled: true, Mix: 0.2, Size: 1.2, Decay: 0.65, Tone: 0.3}
			applyPreset(voice.EQSettings{
				BassGain: 2.0, BassHz: 110,
				MidGain: -1.0, MidHz: 400, MidQ: 1.2,
				TrebleGain: -3.0, TrebleHz: 8000,
				PresenceGain: -3.0, PresenceHz: 3500, PresenceQ: 0.9,
			}, del, rev, cho, pan)
		},
	},
	// ── Punchy ───────────────────────────────────────────────────────────────
	{
		name:    "Punchy",
		tagline: "Tight bass, forward mids",
		tags:    "EQ only",
		apply: func() {
			del, rev, cho, pan := noFX()
			applyPreset(voice.EQSettings{
				BassGain: 3.0, BassHz: 80,
				MidGain: 1.5, MidHz: 700, MidQ: 1.5,
				TrebleGain: 2.0, TrebleHz: 5000,
				PresenceGain: 2.5, PresenceHz: 3000, PresenceQ: 1.4,
			}, del, rev, cho, pan)
		},
	},
	// ── Techno ───────────────────────────────────────────────────────────────
	{
		name:    "Techno",
		tagline: "Full FX chain, high energy",
		tags:    "EQ · Reverb · Chorus · Delay · Pan",
		apply: func() {
			del := voice.DelaySettings{Enabled: true, DelayMs: 120, Feedback: 0.35}
			rev := voice.ReverbSettings{Enabled: true, Mix: 0.25, Size: 2.0, Decay: 0.70, Tone: 0.6}
			cho := voice.ChorusSettings{Enabled: true, BaseDelayMs: 12, RateHz: 0.5, DepthMs: 2.0, Mix: 0.12}
			pan := voice.PannerSettings{Balance: 0, AutoPanEnabled: true, AutoPanRate: 0.5, AutoPanDepth: 0.4}
			applyPreset(voice.EQSettings{
				BassGain: 3.0, BassHz: 80,
				MidGain: -3.0, MidHz: 500, MidQ: 1.2,
				TrebleGain: 3.0, TrebleHz: 8000,
				PresenceGain: 3.0, PresenceHz: 4000, PresenceQ: 1.0,
			}, del, rev, cho, pan)
		},
	},
	// ── Vocal Boost ──────────────────────────────────────────────────────────
	{
		name:    "Vocal Boost",
		tagline: "Forward mids & presence",
		tags:    "EQ only",
		apply: func() {
			del, rev, cho, pan := noFX()
			applyPreset(voice.EQSettings{
				BassGain: -2.0, BassHz: 120,
				MidGain: 2.5, MidHz: 800, MidQ: 0.8,
				TrebleGain: 1.0, TrebleHz: 5000,
				PresenceGain: 3.0, PresenceHz: 3000, PresenceQ: 1.2,
			}, del, rev, cho, pan)
		},
	},
	// ── Warm ─────────────────────────────────────────────────────────────────
	{
		name:    "Warm",
		tagline: "Full bass, rolled highs",
		tags:    "EQ only",
		apply: func() {
			del, rev, cho, pan := noFX()
			applyPreset(voice.EQSettings{
				BassGain: 3.0, BassHz: 110,
				MidGain: 1.0, MidHz: 1000, MidQ: 1.0,
				TrebleGain: -3.0, TrebleHz: 6000,
				PresenceGain: -2.0, PresenceHz: 3000, PresenceQ: 1.0,
			}, del, rev, cho, pan)
		},
	},
}

// ── Display preset (built-in + custom unified) ────────────────────────────────

type displayPreset struct {
	name     string
	tagline  string
	tags     string
	isCustom bool
	apply    func()
}

func buildDisplayPresets(custom []voice.PlayerPreset) []displayPreset {
	out := make([]displayPreset, 0, len(audioPresetList)+len(custom))
	for _, p := range audioPresetList {
		out = append(out, displayPreset{
			name: p.name, tagline: p.tagline, tags: p.tags, apply: p.apply,
		})
	}
	for _, cp := range custom {
		cp := cp // capture
		out = append(out, displayPreset{
			name:     cp.Name,
			tagline:  "Custom preset",
			tags:     "Custom",
			isCustom: true,
			apply:    func() { cp.Apply() },
		})
	}
	return out
}

// ── Screen ────────────────────────────────────────────────────────────────────

// AudioPlayerPresetsScreen is the full-screen preset selector.
type AudioPlayerPresetsScreen struct {
	width, height int
	cursor        int // flat index into all presets (built-in + custom)
	scrollRow     int // index of first visible card row
	customPresets []voice.PlayerPreset
}

func NewAudioPlayerPresetsScreen(termW, termH int) *AudioPlayerPresetsScreen {
	return &AudioPlayerPresetsScreen{width: termW, height: termH}
}

func (s *AudioPlayerPresetsScreen) SetSize(termW, termH int) {
	s.width = termW
	s.height = termH
}

// Refresh reloads custom presets from disk. Called whenever the screen is opened.
func (s *AudioPlayerPresetsScreen) Refresh() {
	s.customPresets, _ = voice.LoadCustomPresets()
}

func (s *AudioPlayerPresetsScreen) Init() tea.Cmd { return nil }

func (s *AudioPlayerPresetsScreen) Update(msg tea.Msg) tea.Cmd {
	if kp, ok := msg.(tea.KeyPressMsg); ok {
		return s.handleKey(kp)
	}
	return nil
}

const presetsNumCols = 3

func (s *AudioPlayerPresetsScreen) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	all := buildDisplayPresets(s.customPresets)
	n := len(all)
	if n == 0 {
		if msg.String() == "esc" {
			return func() tea.Msg { return HideAudioPlayerPresetsMsg{} }
		}
		return nil
	}
	row := s.cursor / presetsNumCols
	col := s.cursor % presetsNumCols

	switch msg.String() {
	case "esc":
		return func() tea.Msg { return HideAudioPlayerPresetsMsg{} }
	case "up", "w":
		if row > 0 {
			s.cursor -= presetsNumCols
		}
	case "down", "s":
		next := s.cursor + presetsNumCols
		if next < n {
			s.cursor = next
		}
	case "left", "a":
		if col > 0 {
			s.cursor--
		}
	case "right":
		// Stay within the same row.
		next := s.cursor + 1
		if next < n && next/presetsNumCols == row {
			s.cursor = next
		}
	case "d":
		// Delete if cursor is on a custom preset; otherwise move right.
		if s.cursor < n && all[s.cursor].isCustom {
			name := all[s.cursor].name
			_ = voice.DeleteCustomPreset(name)
			s.customPresets, _ = voice.LoadCustomPresets()
			all = buildDisplayPresets(s.customPresets)
			if s.cursor >= len(all) {
				s.cursor = len(all) - 1
			}
			if s.cursor < 0 {
				s.cursor = 0
			}
			deletedName := name
			return func() tea.Msg { return ShowToastMsg{Text: "Deleted preset: " + deletedName} }
		}
		// Not on custom preset — treat as right arrow.
		next := s.cursor + 1
		if next < n && next/presetsNumCols == row {
			s.cursor = next
		}
	case "tab":
		s.cursor = (s.cursor + 1) % n
	case "shift+tab":
		s.cursor = (s.cursor - 1 + n) % n
	case "enter", " ":
		if s.cursor >= 0 && s.cursor < n {
			all[s.cursor].apply()
		}
		return func() tea.Msg { return HideAudioPlayerPresetsMsg{} }
	}
	return nil
}

const presetCardH = 5 // 3 content lines + 2 border lines = 5 rendered rows

func (s *AudioPlayerPresetsScreen) Render() string {
	innerW := s.width - 8
	innerH := s.height - 6
	if innerW < 36 {
		innerW = 36
	}
	if innerH < 10 {
		innerH = 10
	}

	all := buildDisplayPresets(s.customPresets)
	n := len(all)

	dim := styles.DimText
	activeName := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#c4b5fd")).
		Bold(true).
		Background(styles.ComponentBg)
	customName := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#86efac")).
		Bold(true).
		Background(styles.ComponentBg)
	activeCustomName := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#4ade80")).
		Bold(true).
		Background(styles.ComponentBg)
	tagDim := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#555577")).
		Background(styles.ComponentBg)
	customTag := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#34d399")).
		Background(styles.ComponentBg)

	colW := innerW / presetsNumCols
	if colW < 18 {
		colW = 18
	}

	numRows := (n + presetsNumCols - 1) / presetsNumCols
	if numRows == 0 {
		numRows = 1
	}

	// Visible rows calculation.
	const headerLines = 2 // badge + blank
	const footerLines = 1 // blank + optional scroll indicator
	availH := innerH - headerLines - footerLines
	visibleRows := availH / presetCardH
	if visibleRows < 1 {
		visibleRows = 1
	}
	if visibleRows > numRows {
		visibleRows = numRows
	}

	cursorRow := s.cursor / presetsNumCols

	// Clamp scroll so cursor is always visible.
	if cursorRow < s.scrollRow {
		s.scrollRow = cursorRow
	}
	if cursorRow >= s.scrollRow+visibleRows {
		s.scrollRow = cursorRow - visibleRows + 1
	}
	if s.scrollRow < 0 {
		s.scrollRow = 0
	}
	maxScroll := numRows - visibleRows
	if maxScroll < 0 {
		maxScroll = 0
	}
	if s.scrollRow > maxScroll {
		s.scrollRow = maxScroll
	}

	// ── Header ────────────────────────────────────────────────────────────────
	var lines []string
	badge := styles.AudioPlayerBadge.Render("PLAYER PRESETS")
	var hintStr string
	if s.cursor < n && all[s.cursor].isCustom {
		hintStr = "↑↓←→  navigate  •  enter  apply  •  d  delete  •  esc  back"
	} else {
		hintStr = "↑↓←→ wasd  navigate  •  enter  apply  •  esc  back"
	}
	hint := dim.Render(hintStr)
	gap := innerW - lipgloss.Width(badge) - lipgloss.Width(hint)
	if gap < 1 {
		gap = 1
	}
	lines = append(lines, badge+dim.Render(strings.Repeat(" ", gap))+hint, "")

	// ── Card rows ─────────────────────────────────────────────────────────────
	for rowIdx := s.scrollRow; rowIdx < s.scrollRow+visibleRows && rowIdx < numRows; rowIdx++ {
		var boxes []string
		for colIdx := 0; colIdx < presetsNumCols; colIdx++ {
			presetIdx := rowIdx*presetsNumCols + colIdx
			if presetIdx >= n {
				filler := lipgloss.NewStyle().
					Width(colW).
					Background(styles.ComponentBg).
					Height(presetCardH).
					Render("")
				boxes = append(boxes, filler)
				continue
			}
			p := all[presetIdx]
			isActive := presetIdx == s.cursor

			borderColor := styles.PanelBorderColor
			if isActive {
				borderColor = styles.PanelFocusedBorderColor
			}

			var prefix, namePart string
			switch {
			case isActive && p.isCustom:
				prefix = styles.CursorStyle.Render("▶ ")
				namePart = activeCustomName.Render(p.name)
			case isActive:
				prefix = styles.CursorStyle.Render("▶ ")
				namePart = activeName.Render(p.name)
			case p.isCustom:
				prefix = dim.Render("  ")
				namePart = customName.Render(p.name)
			default:
				prefix = dim.Render("  ")
				namePart = dim.Render(p.name)
			}

			var tagsLine string
			if p.isCustom {
				tagsLine = customTag.Render("  ★ " + p.tags)
			} else {
				tagsLine = tagDim.Render("  " + p.tags)
			}

			content := prefix + namePart + "\n" +
				dim.Render("  "+p.tagline) + "\n" +
				tagsLine

			box := lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(borderColor).
				BorderBackground(styles.ComponentBg).
				Background(styles.ComponentBg).
				Padding(0, 1).
				Width(colW).
				Render(content)
			boxes = append(boxes, box)
		}
		rowStr := lipgloss.JoinHorizontal(lipgloss.Top, boxes...)
		for _, l := range strings.Split(rowStr, "\n") {
			lines = append(lines, l)
		}
	}

	// ── Scroll indicator ──────────────────────────────────────────────────────
	if numRows > visibleRows {
		pct := 0
		if maxScroll > 0 {
			pct = s.scrollRow * 100 / maxScroll
		}
		lines = append(lines, dim.Render(
			strings.Repeat(" ", innerW-7)+"↕ "+fmt.Sprintf("%d%%", pct),
		))
	}

	// Clip to available height.
	if len(lines) > innerH {
		lines = lines[:innerH]
	}

	body := strings.Join(lines, "\n")
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.PanelBorderColor).
		BorderBackground(styles.ComponentBg).
		Background(styles.ComponentBg).
		Padding(0, 1).
		Width(s.width).
		Height(s.height).
		Render(body)
}
