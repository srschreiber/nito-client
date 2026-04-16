// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package components

import (
	"fmt"
	"math"
	"strings"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/srschreiber/nito-client/shellapp/styles"
	"github.com/srschreiber/nito-client/shellapp/voice"
)

// ShowAudioPlayerSettingsMsg opens the audio player EQ settings screen.
type ShowAudioPlayerSettingsMsg struct{}

// HideAudioPlayerSettingsMsg closes the audio player EQ settings screen.
type HideAudioPlayerSettingsMsg struct{}

// Cursor indices.
const (
	apsBassGain       = 0
	apsBassFreq       = 1
	apsMidGain        = 2
	apsMidFreq        = 3
	apsMidQ           = 4
	apsTrebGain       = 5
	apsTrebFreq       = 6
	apsPresGain       = 7 // Presence peaking filter (2–5 kHz)
	apsPresHz         = 8
	apsPresQ          = 9
	apsDelayEnabled   = 10
	apsDelayDuration  = 11
	apsDelayFeedback  = 12
	apsReverbEnabled  = 13
	apsReverbMix      = 14
	apsReverbSize     = 15
	apsReverbDecay    = 16
	apsReverbTone     = 17
	apsChorusEnabled  = 18
	apsChorusDelay    = 19
	apsChorusRate     = 20
	apsChorusDepth    = 21
	apsChorusMix      = 22
	apsPitchEnabled   = 23
	apsPitchSemitones = 24
	apsBalance        = 25
	apsPanEnabled     = 26
	apsPanRate        = 27
	apsPanDepth       = 28
	apsVolume         = 29
	apsReset          = 30
	apsItemCount      = 31
)

// apsSecDef describes one effect section in the 3-column layout.
type apsSecDef struct {
	name  string
	start int // first cursor index in this section
	end   int // last cursor index in this section (inclusive)
}

// apsSectionList maps section index → cursor range.
// Sections 0-9 fill the column grid; section 10 is the Reset row.
var apsSectionList = [11]apsSecDef{
	{name: "BASS", start: apsBassGain, end: apsBassFreq},
	{name: "MID", start: apsMidGain, end: apsMidQ},
	{name: "TREBLE", start: apsTrebGain, end: apsTrebFreq},
	{name: "PRESENCE", start: apsPresGain, end: apsPresQ},
	{name: "DELAY", start: apsDelayEnabled, end: apsDelayFeedback},
	{name: "REVERB", start: apsReverbEnabled, end: apsReverbTone},
	{name: "CHORUS", start: apsChorusEnabled, end: apsChorusMix},
	{name: "PITCH", start: apsPitchEnabled, end: apsPitchSemitones},
	{name: "PAN", start: apsBalance, end: apsPanDepth},
	{name: "OUTPUT", start: apsVolume, end: apsVolume},
	{name: "RESET", start: apsReset, end: apsReset},
}

// apsCursorSectionIdx returns the section index (0-9) for the given cursor position.
func apsCursorSectionIdx(cursor int) int {
	for i, s := range apsSectionList {
		if cursor >= s.start && cursor <= s.end {
			return i
		}
	}
	return 0
}

// Per-band gain limits (dB) and shared step size.
const (
	bassGainMin = float32(-18)
	bassGainMax = float32(18)
	midGainMin  = float32(-18)
	midGainMax  = float32(18)
	trebGainMin = float32(-18)
	trebGainMax = float32(18)
	gainStepDB  = float32(0.5)

	bassFreqMin, bassFreqMax, bassFreqStep = 40, 500, 10
	midFreqMin, midFreqMax, midFreqStep    = 200, 8000, 100
	midQMin, midQMax                       = float32(0.3), float32(6.0)
	midQStep                               = float32(0.1)
	trebFreqMin, trebFreqMax, trebFreqStep = 1000, 16000, 500

	presGainMin                            = float32(-18)
	presGainMax                            = float32(18)
	presFreqMin, presFreqMax, presFreqStep = 2000, 5000, 100
	presQMin, presQMax                     = float32(0.3), float32(6.0)
	presQStep                              = float32(0.1)

	volMin, volMax, volStep = 0, 800, 20 // output volume %

	delayDurMin, delayDurMax, delayDurStep                = 1, 500, 5 // ms
	delayFeedbackMin, delayFeedbackMax, delayFeedbackStep = float32(0), float32(0.95), float32(0.05)
	reverbMixMin, reverbMixMax, reverbMixStep             = float32(0), float32(1.0), float32(0.01)
	reverbSizeMin, reverbSizeMax, reverbSizeStep          = float32(0.5), float32(2.0), float32(0.1)
	reverbDecayMin, reverbDecayMax, reverbDecayStep       = float32(0), float32(1.0), float32(0.05)
	reverbToneMin, reverbToneMax, reverbToneStep          = float32(0), float32(1.0), float32(0.05)
	chorusDelayMin, chorusDelayMax, chorusDelayStep       = 5, 30, 1 // ms
	chorusRateMin, chorusRateMax, chorusRateStep          = float32(0.1), float32(5.0), float32(0.1)
	chorusDepthMin, chorusDepthMax, chorusDepthStep       = float32(0), float32(15), float32(0.5)
	chorusMixMin, chorusMixMax, chorusMixStep             = float32(0), float32(1.0), float32(0.05)
	pitchMin, pitchMax, pitchStep                         = float32(-12), float32(12), float32(0.5) // semitones

	balanceMin, balanceMax, balanceStep    = float32(-1), float32(1), float32(0.05)
	panRateMin, panRateMax, panRateStep    = float32(0.05), float32(5.0), float32(0.05)
	panDepthMin, panDepthMax, panDepthStep = float32(0), float32(1.0), float32(0.05)
)

// AudioPlayerSettingsScreen is a full-screen multi-effect panel for .play audio clips.
// Changes take effect immediately; every adjustment auto-saves.
type AudioPlayerSettingsScreen struct {
	width, height int
	cursor        int
}

func NewAudioPlayerSettingsScreen(termW, termH int) *AudioPlayerSettingsScreen {
	return &AudioPlayerSettingsScreen{width: termW, height: termH}
}

func (s *AudioPlayerSettingsScreen) SetSize(termW, termH int) {
	s.width = termW
	s.height = termH
}

func (s *AudioPlayerSettingsScreen) Init() tea.Cmd { return nil }

func (s *AudioPlayerSettingsScreen) Update(msg tea.Msg) tea.Cmd {
	if msg, ok := msg.(tea.KeyPressMsg); ok {
		return s.handleKey(msg)
	}
	return nil
}

func (s *AudioPlayerSettingsScreen) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		return func() tea.Msg { return HideAudioPlayerSettingsMsg{} }
	case "p":
		return func() tea.Msg { return ShowAudioPlayerPresetsMsg{} }
	case "tab":
		secIdx := apsCursorSectionIdx(s.cursor)
		next := (secIdx + 1) % len(apsSectionList)
		s.cursor = apsSectionList[next].start
	case "shift+tab":
		secIdx := apsCursorSectionIdx(s.cursor)
		prev := (secIdx - 1 + len(apsSectionList)) % len(apsSectionList)
		s.cursor = apsSectionList[prev].start
	case "up", "ctrl+p", "w":
		sec := apsSectionList[apsCursorSectionIdx(s.cursor)]
		if s.cursor > sec.start {
			s.cursor--
		}
	case "down", "ctrl+n", "s":
		sec := apsSectionList[apsCursorSectionIdx(s.cursor)]
		if s.cursor < sec.end {
			s.cursor++
		}
	case "left", "a":
		s.adjust(-1)
	case "right", "d":
		s.adjust(+1)
	case "enter", " ":
		if s.cursor == apsReset {
			var defEQ voice.EQSettings
			defEQ.SetDefaults()
			voice.SetPlaybackEQSettings(defEQ)
			voice.SetPlaybackEQVolume(100)

			var defDelay voice.DelaySettings
			defDelay.SetDefaults()
			voice.SetDelaySettings(defDelay)

			var defRev voice.ReverbSettings
			defRev.SetDefaults()
			voice.SetReverbSettings(defRev)

			var defCho voice.ChorusSettings
			defCho.SetDefaults()
			voice.SetChorusSettings(defCho)

			var defPitch voice.PlaybackPitchSettings
			defPitch.SetDefaults()
			voice.SetPlaybackPitchSettings(defPitch)

			var defPan voice.PannerSettings
			defPan.SetDefaults()
			voice.SetPannerSettings(defPan)

			voice.SaveAudioSettings()
		}
		switch s.cursor {
		case apsDelayEnabled, apsReverbEnabled, apsChorusEnabled, apsPitchEnabled, apsPanEnabled:
			s.adjust(1)
		}
	}
	return nil
}

func (s *AudioPlayerSettingsScreen) adjust(delta int) {
	switch s.cursor {
	// ── EQ ──────────────────────────────────────────────────────────────────────
	case apsBassGain, apsBassFreq,
		apsMidGain, apsMidFreq, apsMidQ,
		apsTrebGain, apsTrebFreq,
		apsPresGain, apsPresHz, apsPresQ:
		eq := voice.GetPlaybackEQSettings()
		switch s.cursor {
		case apsBassGain:
			eq.BassGain = clampF32(eq.BassGain+float32(delta)*gainStepDB, bassGainMin, bassGainMax)
		case apsBassFreq:
			eq.BassHz = clampF32(eq.BassHz+float32(delta*bassFreqStep), bassFreqMin, bassFreqMax)
		case apsMidGain:
			eq.MidGain = clampF32(eq.MidGain+float32(delta)*gainStepDB, midGainMin, midGainMax)
		case apsMidFreq:
			eq.MidHz = clampF32(eq.MidHz+float32(delta*midFreqStep), midFreqMin, midFreqMax)
		case apsMidQ:
			eq.MidQ = clampF32(eq.MidQ+float32(delta)*midQStep, midQMin, midQMax)
		case apsTrebGain:
			eq.TrebleGain = clampF32(eq.TrebleGain+float32(delta)*gainStepDB, trebGainMin, trebGainMax)
		case apsTrebFreq:
			eq.TrebleHz = clampF32(eq.TrebleHz+float32(delta*trebFreqStep), trebFreqMin, trebFreqMax)
		case apsPresGain:
			eq.PresenceGain = clampF32(eq.PresenceGain+float32(delta)*gainStepDB, presGainMin, presGainMax)
		case apsPresHz:
			eq.PresenceHz = clampF32(eq.PresenceHz+float32(delta*presFreqStep), presFreqMin, presFreqMax)
		case apsPresQ:
			eq.PresenceQ = clampF32(eq.PresenceQ+float32(delta)*presQStep, presQMin, presQMax)
		}
		voice.SetPlaybackEQSettings(eq)
		voice.SaveAudioSettings()

	// ── Delay ────────────────────────────────────────────────────────────────────
	case apsDelayEnabled:
		d := voice.GetDelaySettings()
		d.Enabled = !d.Enabled
		voice.SetDelaySettings(d)
		voice.SaveAudioSettings()
	case apsDelayDuration:
		d := voice.GetDelaySettings()
		d.DelayMs = clampF32(d.DelayMs+float32(delta*delayDurStep), delayDurMin, delayDurMax)
		voice.SetDelaySettings(d)
		voice.SaveAudioSettings()
	case apsDelayFeedback:
		d := voice.GetDelaySettings()
		d.Feedback = clampF32(d.Feedback+float32(delta)*delayFeedbackStep, delayFeedbackMin, delayFeedbackMax)
		voice.SetDelaySettings(d)
		voice.SaveAudioSettings()

	// ── Reverb ───────────────────────────────────────────────────────────────────
	case apsReverbEnabled:
		rev := voice.GetReverbSettings()
		rev.Enabled = !rev.Enabled
		voice.SetReverbSettings(rev)
		voice.SaveAudioSettings()
	case apsReverbMix:
		rev := voice.GetReverbSettings()
		rev.Mix = clampF32(rev.Mix+float32(delta)*reverbMixStep, reverbMixMin, reverbMixMax)
		voice.SetReverbSettings(rev)
		voice.SaveAudioSettings()
	case apsReverbSize:
		rev := voice.GetReverbSettings()
		rev.Size = clampF32(rev.Size+float32(delta)*reverbSizeStep, reverbSizeMin, reverbSizeMax)
		voice.SetReverbSettings(rev)
		voice.SaveAudioSettings()
	case apsReverbDecay:
		rev := voice.GetReverbSettings()
		rev.Decay = clampF32(rev.Decay+float32(delta)*reverbDecayStep, reverbDecayMin, reverbDecayMax)
		voice.SetReverbSettings(rev)
		voice.SaveAudioSettings()
	case apsReverbTone:
		rev := voice.GetReverbSettings()
		rev.Tone = clampF32(rev.Tone+float32(delta)*reverbToneStep, reverbToneMin, reverbToneMax)
		voice.SetReverbSettings(rev)
		voice.SaveAudioSettings()

	// ── Chorus ───────────────────────────────────────────────────────────────────
	case apsChorusEnabled:
		cho := voice.GetChorusSettings()
		cho.Enabled = !cho.Enabled
		voice.SetChorusSettings(cho)
		voice.SaveAudioSettings()
	case apsChorusDelay:
		cho := voice.GetChorusSettings()
		cho.BaseDelayMs = clampF32(cho.BaseDelayMs+float32(delta*chorusDelayStep), chorusDelayMin, chorusDelayMax)
		voice.SetChorusSettings(cho)
		voice.SaveAudioSettings()
	case apsChorusRate:
		cho := voice.GetChorusSettings()
		cho.RateHz = clampF32(cho.RateHz+float32(delta)*chorusRateStep, chorusRateMin, chorusRateMax)
		voice.SetChorusSettings(cho)
		voice.SaveAudioSettings()
	case apsChorusDepth:
		cho := voice.GetChorusSettings()
		cho.DepthMs = clampF32(cho.DepthMs+float32(delta)*chorusDepthStep, chorusDepthMin, chorusDepthMax)
		voice.SetChorusSettings(cho)
		voice.SaveAudioSettings()
	case apsChorusMix:
		cho := voice.GetChorusSettings()
		cho.Mix = clampF32(cho.Mix+float32(delta)*chorusMixStep, chorusMixMin, chorusMixMax)
		voice.SetChorusSettings(cho)
		voice.SaveAudioSettings()

	// ── Pitch ────────────────────────────────────────────────────────────────────
	case apsPitchEnabled:
		pitch := voice.GetPlaybackPitchSettings()
		pitch.Enabled = !pitch.Enabled
		voice.SetPlaybackPitchSettings(pitch)
		voice.SaveAudioSettings()
	case apsPitchSemitones:
		pitch := voice.GetPlaybackPitchSettings()
		pitch.Semitones = clampF32(pitch.Semitones+float32(delta)*pitchStep, pitchMin, pitchMax)
		voice.SetPlaybackPitchSettings(pitch)
		voice.SaveAudioSettings()

	// ── Balance / Auto Pan ───────────────────────────────────────────────────────
	case apsBalance:
		pan := voice.GetPannerSettings()
		pan.Balance = clampF32(pan.Balance+float32(delta)*balanceStep, balanceMin, balanceMax)
		voice.SetPannerSettings(pan)
		voice.SaveAudioSettings()
	case apsPanEnabled:
		pan := voice.GetPannerSettings()
		pan.AutoPanEnabled = !pan.AutoPanEnabled
		voice.SetPannerSettings(pan)
		voice.SaveAudioSettings()
	case apsPanRate:
		pan := voice.GetPannerSettings()
		pan.AutoPanRate = clampF32(pan.AutoPanRate+float32(delta)*panRateStep, panRateMin, panRateMax)
		voice.SetPannerSettings(pan)
		voice.SaveAudioSettings()
	case apsPanDepth:
		pan := voice.GetPannerSettings()
		pan.AutoPanDepth = clampF32(pan.AutoPanDepth+float32(delta)*panDepthStep, panDepthMin, panDepthMax)
		voice.SetPannerSettings(pan)
		voice.SaveAudioSettings()

	// ── Volume ───────────────────────────────────────────────────────────────────
	case apsVolume:
		newVol := voice.GetPlaybackEQVolume() + delta*volStep
		if newVol < volMin {
			newVol = volMin
		} else if newVol > volMax {
			newVol = volMax
		}
		voice.SetPlaybackEQVolume(newVol)
		voice.SaveAudioSettings()
	}
}

func clampF32(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ── Sliders ──────────────────────────────────────────────────────────────────

func gainSliderAPS(gain, lo, hi float32) string {
	steps := int((hi-lo)/gainStepDB + 0.5)
	pos := int((gain-lo)/gainStepDB + 0.5)
	return buildSlider(pos, steps)
}

func freqSliderAPS(val float32, lo, hi, step int) string {
	return buildSlider((int(val)-lo)/step, (hi-lo)/step)
}

func qSliderAPS(q float32) string {
	qRange := midQMax - midQMin
	steps := int(qRange/midQStep + 0.5)
	pos := int((q-midQMin)/midQStep + 0.5)
	return buildSlider(pos, steps)
}

func volSliderAPS(pct int) string {
	return buildSlider((pct-volMin)/volStep, (volMax-volMin)/volStep)
}

func f32SliderAPS(val, lo, hi, step float32) string {
	total := int((hi-lo)/step + 0.5)
	pos := int((val-lo)/step + 0.5)
	return buildSlider(pos, total)
}

func gainLabel(db float32) string {
	if db >= 0 {
		return fmt.Sprintf("+%.1f dB", db)
	}
	return fmt.Sprintf("%.1f dB", db)
}

func balanceLabel(bal float32) string {
	if bal < -0.01 {
		return fmt.Sprintf("L %.0f%%", -bal*100)
	}
	if bal > 0.01 {
		return fmt.Sprintf("R %.0f%%", bal*100)
	}
	return "center"
}

func onOffLabel(enabled bool) string {
	if enabled {
		return "[ON]"
	}
	return "[OFF]"
}

// ── Frequency response graph ──────────────────────────────────────────────────

// renderEQGraph draws an ASCII frequency-response curve for the given EQ
// settings with a live spectrum overlay showing playback energy per band.
// The graph is graphH rows tall and graphW columns wide.
// Frequency axis is logarithmic from 20 Hz to 20 kHz.
// Y axis spans ±graphDBRange dB with the 0 dB line at the centre.
// levels contains the per-band smoothed amplitude from the band analyser;
// pass an all-zero array when nothing is playing.
func renderEQGraph(eq voice.EQSettings, graphW, graphH int, levels []float32) []string {
	const sampleRate = 48000.0
	const graphDBRange = 18.0 // dB above/below zero visible in the graph

	// Build a temporary EQ to evaluate the frequency response.
	var tmpEQ voice.EQ
	tmpEQ.Settings = eq
	tmpEQ.UpdateFilters(sampleRate)

	// Compute gain in dB at each column (log-spaced frequencies).
	gains := make([]float64, graphW)
	for c := 0; c < graphW; c++ {
		t := float64(c) / float64(graphW-1)
		freq := 20.0 * math.Pow(1000.0, t) // 20 Hz → 20 kHz log scale
		gains[c] = tmpEQ.MagResponseDB(freq, sampleRate)
	}

	// Compute per-column bar heights from the live band levels.
	// Same gain/sqrt curve as the status panel meter so they feel consistent.
	barH := make([]int, graphW)
	for c := 0; c < graphW; c++ {
		if graphW > 1 {
			t := float64(c) / float64(graphW-1)
			freq := 20.0 * math.Pow(1000.0, t)
			level := interpolateBandLevel(freq, levels)
			boosted := level * 8.0
			if boosted > 1 {
				boosted = 1
			}
			display := float32(math.Sqrt(float64(boosted)))
			h := int(display * float32(graphH))
			if h > graphH {
				h = graphH
			}
			barH[c] = h
		}
	}

	// Build the character grid: grid[row][col].
	grid := make([][]byte, graphH)
	for r := range grid {
		grid[r] = make([]byte, graphW)
		for c := range grid[r] {
			grid[r][c] = ' '
		}
	}

	// Mark the 0 dB centre line with dashes.
	zeroRow := graphH / 2
	for c := 0; c < graphW; c++ {
		grid[zeroRow][c] = '-'
	}

	// Plot the curve.
	for c, gainDB := range gains {
		// Clamp to display range.
		if gainDB > graphDBRange {
			gainDB = graphDBRange
		} else if gainDB < -graphDBRange {
			gainDB = -graphDBRange
		}
		// Map gainDB to row: top = +graphDBRange, bottom = -graphDBRange.
		rowF := float64(zeroRow) - gainDB/graphDBRange*float64(zeroRow)
		row := int(math.Round(rowF))
		if row < 0 {
			row = 0
		}
		if row >= graphH {
			row = graphH - 1
		}
		grid[row][c] = '*'
	}

	// Render rows with left-side dB labels (every other row to avoid crowding).
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#555577"))
	curveAbove := lipgloss.NewStyle().Foreground(lipgloss.Color("#86efac")) // green for boost
	curveBelow := lipgloss.NewStyle().Foreground(lipgloss.Color("#f87171")) // red for cut
	zeroStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#4a4a7a"))

	// Bar overlay background colours — dark tinted backgrounds so the EQ curve
	// remains readable on top. Gradient from bottom: green → orange → red.
	barBgGreen := lipgloss.NewStyle().Background(lipgloss.Color("#0a1f0a"))
	barBgOrange := lipgloss.NewStyle().Background(lipgloss.Color("#1f130a"))
	barBgRed := lipgloss.NewStyle().Background(lipgloss.Color("#1f0a0a"))
	// Curve-on-bar styles: brighter so the dot stands out against the tint.
	barCurveAbove := lipgloss.NewStyle().Foreground(lipgloss.Color("#4ade80")).Bold(true)
	barCurveBelow := lipgloss.NewStyle().Foreground(lipgloss.Color("#fca5a5")).Bold(true)

	dbPerRow := graphDBRange / float64(zeroRow)
	var rows []string
	for r := 0; r < graphH; r++ {
		dbAtRow := graphDBRange - float64(r)*dbPerRow
		var label string
		if r == 0 {
			label = fmt.Sprintf("%+4.0fdB│", dbAtRow)
		} else if r == zeroRow {
			label = fmt.Sprintf("%4.0fdB│", dbAtRow)
		} else if r == graphH-1 {
			label = fmt.Sprintf("%+4.0fdB│", dbAtRow)
		} else {
			label = "      │"
		}

		// Render each column with appropriate colour.
		var rowStr strings.Builder
		for c, ch := range grid[r] {
			// Determine whether this cell is inside the spectrum bar for column c.
			// Bar fills from the bottom: rows graphH-barH[c] … graphH-1 are in bar.
			inBar := barH[c] > 0 && r >= graphH-barH[c]

			// Pick bar background tint: gradient green (bottom) → orange → red (top).
			// barFrac = 0 at bottom of bar, ≈1 at top of bar.
			var barBg lipgloss.Style
			if inBar && barH[c] > 0 {
				barFrac := float64(graphH-1-r) / float64(barH[c])
				switch {
				case barFrac < 0.55:
					barBg = barBgGreen // bottom of bar → green
				case barFrac < 0.82:
					barBg = barBgOrange
				default:
					barBg = barBgRed // top of bar → red
				}
			}

			switch ch {
			case '*':
				if inBar {
					if gains[c] >= 0 {
						rowStr.WriteString(barBg.Inherit(barCurveAbove).Render("●"))
					} else {
						rowStr.WriteString(barBg.Inherit(barCurveBelow).Render("●"))
					}
				} else {
					if gains[c] >= 0 {
						rowStr.WriteString(curveAbove.Render("●"))
					} else {
						rowStr.WriteString(curveBelow.Render("●"))
					}
				}
			case '-':
				if inBar {
					rowStr.WriteString(barBg.Render("─"))
				} else {
					rowStr.WriteString(zeroStyle.Render("─"))
				}
			default:
				if inBar {
					rowStr.WriteString(barBg.Render(" "))
				} else {
					rowStr.WriteByte(' ')
				}
			}
		}

		rows = append(rows, dimStyle.Render(label)+rowStr.String())
	}

	// Axis line.
	rows = append(rows, dimStyle.Render("      └"+strings.Repeat("─", graphW)))

	// Frequency labels — place at roughly log-spaced positions.
	type freqMark struct {
		col   int
		label string
	}
	marks := []freqMark{
		{col: freqToCol(20, graphW), label: "20"},
		{col: freqToCol(100, graphW), label: "100"},
		{col: freqToCol(500, graphW), label: "500"},
		{col: freqToCol(1000, graphW), label: "1k"},
		{col: freqToCol(5000, graphW), label: "5k"},
		{col: freqToCol(20000, graphW), label: "20k"},
	}
	axisLabel := make([]byte, graphW+7) // 7 = len("      └")
	for i := range axisLabel {
		axisLabel[i] = ' '
	}
	for _, m := range marks {
		pos := m.col + 7
		for i, ch := range []byte(m.label) {
			if pos+i < len(axisLabel) {
				axisLabel[pos+i] = ch
			}
		}
	}
	rows = append(rows, dimStyle.Render(string(axisLabel)))

	return rows
}

// freqToCol converts a frequency in Hz to a graph column index using the same
// log scale as renderEQGraph: 20 Hz → col 0, 20 kHz → col graphW-1.
func freqToCol(freqHz, graphW int) int {
	t := math.Log10(float64(freqHz)/20.0) / math.Log10(1000.0)
	return int(math.Round(t * float64(graphW-1)))
}

// interpolateBandLevel returns the smoothed amplitude level at the given
// frequency by linearly interpolating between the nearest band centers in
// log-frequency space. Used to paint the live spectrum overlay on the EQ graph.
func interpolateBandLevel(freq float64, levels []float32) float32 {
	n := len(levels)
	if freq <= 0 || n == 0 {
		return 0
	}
	logFreq := math.Log(freq)
	centers := voice.BandCenters(n)
	logCenters := make([]float64, n)
	for i, c := range centers {
		logCenters[i] = math.Log(float64(c))
	}
	if logFreq <= logCenters[0] {
		return levels[0]
	}
	if logFreq >= logCenters[n-1] {
		return levels[n-1]
	}
	for i := 0; i < n-1; i++ {
		if logFreq >= logCenters[i] && logFreq <= logCenters[i+1] {
			t := float32((logFreq - logCenters[i]) / (logCenters[i+1] - logCenters[i]))
			return (1-t)*levels[i] + t*levels[i+1]
		}
	}
	return 0
}

// currentBandLevels returns the peak band level across all 3 audio tracks
// for the current active band count.
func currentBandLevels() []float32 {
	n := voice.NumBands()
	out := make([]float32, n)
	for track := 0; track < 3; track++ {
		for b := 0; b < n; b++ {
			if l := voice.GetTrackBandLevel(track, b); l > out[b] {
				out[b] = l
			}
		}
	}
	return out
}

// ── Render ────────────────────────────────────────────────────────────────────

func (s *AudioPlayerSettingsScreen) Render() string {
	innerW := s.width - 8
	innerH := s.height - 6
	if innerW < 60 {
		innerW = 60
	}
	if innerH < 20 {
		innerH = 20
	}

	eq := voice.GetPlaybackEQSettings()
	del := voice.GetDelaySettings()
	rev := voice.GetReverbSettings()
	cho := voice.GetChorusSettings()
	pitch := voice.GetPlaybackPitchSettings()
	pan := voice.GetPannerSettings()
	vol := voice.GetPlaybackEQVolume()

	dim := styles.DimText
	activeTxt := lipgloss.NewStyle().Foreground(lipgloss.Color("#c4b5fd")).Bold(true).Background(styles.ComponentBg)
	onStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#86efac")).Bold(true).Background(styles.ComponentBg)
	offStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#f87171")).Background(styles.ComponentBg)

	// colW3 — width for 3-column rows (effects, pitch/pan/output).
	// colW4 — width for the 4-column EQ row (bass/mid/treble/presence).
	// Both minimums ensure content never overflows and triggers word-wrap.
	colW3 := innerW / 3
	if colW3 < 28 {
		colW3 = 28
	}
	colW4 := innerW / 4
	if colW4 < 22 {
		colW4 = 22
	}

	// renderColItem returns 1 or 2 lines for a numeric/adjustable item.
	// Both label and value are padded to fixed widths so the line width never
	// varies as the value changes — prevents lipgloss word-wrap artifacts.
	renderColItem := func(idx int, label, value string) []string {
		content := fmt.Sprintf("%-9s %-9s", label, value)
		if s.cursor == idx {
			return []string{
				styles.CursorStyle.Render("▶ ") + activeTxt.Render(content),
				dim.Render("  ◀▶ adjust"),
			}
		}
		return []string{dim.Render("  " + content)}
	}

	// renderColBool returns 1 or 2 lines for a boolean toggle item.
	renderColBool := func(idx int, label string, enabled bool) []string {
		lbl := fmt.Sprintf("%-9s", label)
		var val string
		if enabled {
			val = onStyle.Render("[ON] ")
		} else {
			val = offStyle.Render("[OFF]")
		}
		if s.cursor == idx {
			return []string{
				styles.CursorStyle.Render("▶ ") + activeTxt.Render(lbl) + dim.Render(" ") + val,
				dim.Render("  enter toggle"),
			}
		}
		return []string{dim.Render("  "+lbl+" ") + val}
	}

	// buildBox wraps a section's item lines in a rounded border box.
	// w is the outer rendered width (use colW4 for EQ row, colW3 for others).
	// The border is highlighted when the cursor is inside that section.
	buildBox := func(secIdx int, header string, itemLines []string, w int) string {
		isActive := apsCursorSectionIdx(s.cursor) == secIdx
		borderColor := styles.PanelBorderColor
		if isActive {
			borderColor = styles.PanelFocusedBorderColor
		}
		hdr := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#c4b5fd")).
			Bold(true).
			Background(styles.ComponentBg).
			Render(header)
		content := hdr + "\n" + strings.Join(itemLines, "\n")
		return lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(borderColor).
			BorderBackground(styles.ComponentBg).
			Background(styles.ComponentBg).
			Padding(0, 1).
			Width(w).
			Render(content)
	}

	// ── Fixed header ──────────────────────────────────────────────────────────
	var lines []string
	title := styles.AudioPlayerBadge.Render("PLAYER EQ")
	hint := dim.Render("tab section  •  ↑↓ item  •  ◀▶ adjust  •  p presets  •  ESC exit")
	gap := innerW - lipgloss.Width(title) - lipgloss.Width(hint)
	if gap < 1 {
		gap = 1
	}
	lines = append(lines, title+dim.Render(strings.Repeat(" ", gap))+hint, "")

	const graphH = 9
	graphW := innerW - 8
	if graphW < 10 {
		graphW = 10
	}
	levels := currentBandLevels()
	for _, row := range renderEQGraph(eq, graphW, graphH, levels) {
		lines = append(lines, row)
	}
	processStr := "--"
	if us := playbackProcessEMAus.Load(); us > 0 {
		processStr = fmt.Sprintf("%.1f ms", float64(us)/1000.0)
	}
	jitterAvgStr := "--"
	if us := playbackJitterEMAus.Load(); us > 0 {
		jitterAvgStr = fmt.Sprintf("%.1f ms", float64(us)/1000.0)
	}
	jitterPeakStr := "--"
	if us := playbackJitterPeakUs.Load(); us > 0 {
		jitterPeakStr = fmt.Sprintf("%.1f ms", float64(us)/1000.0)
	}
	lines = append(lines, dim.Render("  Process "+processStr+"  Jitter avg "+jitterAvgStr+"  peak "+jitterPeakStr), "")

	// ── Row 1: Bass | Mid | Treble | Presence (4-column EQ row) ─────────────
	var bassItems, midItems, trebItems, presItems []string
	bassItems = append(bassItems, renderColItem(apsBassGain, "Gain", gainLabel(eq.BassGain))...)
	bassItems = append(bassItems, renderColItem(apsBassFreq, "Freq", fmt.Sprintf("%d Hz", int(eq.BassHz)))...)

	midItems = append(midItems, renderColItem(apsMidGain, "Gain", gainLabel(eq.MidGain))...)
	midItems = append(midItems, renderColItem(apsMidFreq, "Freq", fmt.Sprintf("%d Hz", int(eq.MidHz)))...)
	midItems = append(midItems, renderColItem(apsMidQ, "Q", fmt.Sprintf("%.1f", eq.MidQ))...)

	trebItems = append(trebItems, renderColItem(apsTrebGain, "Gain", gainLabel(eq.TrebleGain))...)
	trebItems = append(trebItems, renderColItem(apsTrebFreq, "Freq", fmt.Sprintf("%d Hz", int(eq.TrebleHz)))...)

	presItems = append(presItems, renderColItem(apsPresGain, "Gain", gainLabel(eq.PresenceGain))...)
	presItems = append(presItems, renderColItem(apsPresHz, "Freq", fmt.Sprintf("%d Hz", int(eq.PresenceHz)))...)
	presItems = append(presItems, renderColItem(apsPresQ, "Q", fmt.Sprintf("%.1f", eq.PresenceQ))...)

	row1 := lipgloss.JoinHorizontal(lipgloss.Top,
		buildBox(0, "BASS", bassItems, colW4),
		buildBox(1, "MID", midItems, colW4),
		buildBox(2, "TREBLE", trebItems, colW4),
		buildBox(3, "PRESENCE", presItems, colW4),
	)

	// ── Row 2: Delay | Reverb | Chorus ───────────────────────────────────────
	var delItems, revItems, choItems []string
	delItems = append(delItems, renderColBool(apsDelayEnabled, "Enabled", del.Enabled)...)
	delItems = append(delItems, renderColItem(apsDelayDuration, "Delay", fmt.Sprintf("%.0f ms", del.DelayMs))...)
	delItems = append(delItems, renderColItem(apsDelayFeedback, "Feedback", fmt.Sprintf("%.2f", del.Feedback))...)

	revItems = append(revItems, renderColBool(apsReverbEnabled, "Enabled", rev.Enabled)...)
	revItems = append(revItems, renderColItem(apsReverbMix, "Mix", fmt.Sprintf("%.2f", rev.Mix))...)
	revItems = append(revItems, renderColItem(apsReverbSize, "Size", fmt.Sprintf("%.1f", rev.Size))...)
	revItems = append(revItems, renderColItem(apsReverbDecay, "Decay", fmt.Sprintf("%.2f", rev.Decay))...)
	revItems = append(revItems, renderColItem(apsReverbTone, "Tone", fmt.Sprintf("%.2f", rev.Tone))...)

	choItems = append(choItems, renderColBool(apsChorusEnabled, "Enabled", cho.Enabled)...)
	choItems = append(choItems, renderColItem(apsChorusDelay, "Delay", fmt.Sprintf("%.0f ms", cho.BaseDelayMs))...)
	choItems = append(choItems, renderColItem(apsChorusRate, "Rate", fmt.Sprintf("%.1f Hz", cho.RateHz))...)
	choItems = append(choItems, renderColItem(apsChorusDepth, "Depth", fmt.Sprintf("%.1f ms", cho.DepthMs))...)
	choItems = append(choItems, renderColItem(apsChorusMix, "Mix", fmt.Sprintf("%.2f", cho.Mix))...)

	row2 := lipgloss.JoinHorizontal(lipgloss.Top,
		buildBox(4, "DELAY", delItems, colW3),
		buildBox(5, "REVERB", revItems, colW3),
		buildBox(6, "CHORUS", choItems, colW3),
	)

	// ── Row 3: Pitch | Pan | Output ───────────────────────────────────────────
	var pitchItems, panItems, outItems []string
	pitchItems = append(pitchItems, renderColBool(apsPitchEnabled, "Enabled", pitch.Enabled)...)
	pitchItems = append(pitchItems, renderColItem(apsPitchSemitones, "Semitones", fmt.Sprintf("%+.1f st", pitch.Semitones))...)

	panItems = append(panItems, renderColItem(apsBalance, "Balance", balanceLabel(pan.Balance))...)
	panItems = append(panItems, renderColBool(apsPanEnabled, "Auto Pan", pan.AutoPanEnabled)...)
	panItems = append(panItems, renderColItem(apsPanRate, "Rate", fmt.Sprintf("%.2f Hz", pan.AutoPanRate))...)
	panItems = append(panItems, renderColItem(apsPanDepth, "Depth", fmt.Sprintf("%.0f%%", pan.AutoPanDepth*100))...)

	outItems = append(outItems, renderColItem(apsVolume, "Volume", fmt.Sprintf("%d%%", vol))...)
	outItems = append(outItems, dim.Render("  Limiter  ")+onStyle.Render("[ON]"))

	row3 := lipgloss.JoinHorizontal(lipgloss.Top,
		buildBox(7, "PITCH", pitchItems, colW3),
		buildBox(8, "PAN", panItems, colW3),
		buildBox(9, "OUTPUT", outItems, colW3),
	)

	// ── Reset ─────────────────────────────────────────────────────────────────
	var resetLine string
	if s.cursor == apsReset {
		resetLine = styles.CursorStyle.Render("▶ ") +
			lipgloss.NewStyle().Foreground(lipgloss.Color("#f87171")).Bold(true).Background(styles.ComponentBg).Render("Reset to Defaults")
	} else {
		resetLine = dim.Render("  Reset to Defaults")
	}

	// ── Assemble ──────────────────────────────────────────────────────────────
	for _, row := range strings.Split(row1, "\n") {
		lines = append(lines, row)
	}
	for _, row := range strings.Split(row2, "\n") {
		lines = append(lines, row)
	}
	for _, row := range strings.Split(row3, "\n") {
		lines = append(lines, row)
	}
	lines = append(lines, "", resetLine)

	if len(lines) > innerH {
		lines = lines[:innerH]
	}
	body := strings.Join(lines, "\n")

	_ = onOffLabel    // package-level helper kept for potential future use
	_ = gainSliderAPS // slider helpers kept for potential future use
	_ = freqSliderAPS //
	_ = qSliderAPS    //
	_ = volSliderAPS  //
	_ = f32SliderAPS  //
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
