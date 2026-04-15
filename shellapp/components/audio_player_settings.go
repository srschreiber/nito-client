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
	apsDelayEnabled   = 7
	apsDelayDuration  = 8
	apsDelayFeedback  = 9
	apsReverbEnabled  = 10
	apsReverbMix      = 11
	apsReverbSize     = 12
	apsReverbDecay    = 13
	apsReverbTone     = 14
	apsChorusEnabled  = 15
	apsChorusDelay    = 16
	apsChorusRate     = 17
	apsChorusDepth    = 18
	apsChorusMix      = 19
	apsPitchEnabled   = 20
	apsPitchSemitones = 21
	apsBalance        = 22
	apsPanEnabled     = 23
	apsPanRate        = 24
	apsPanDepth       = 25
	apsVolume         = 26
	apsReset          = 27
	apsItemCount      = 28
)

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
	scrollOffset  int
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

// innerH computes the available line count below the fixed header (title +
// blank + graph + blank) for use in scroll calculations.
func (s *AudioPlayerSettingsScreen) innerH() int {
	h := s.height - 6 // border padding
	if h < 12 {
		h = 12
	}
	// The fixed section: title(1) + blank(1) + graphH(9) + axisLine(1) + freqLabel(1) + blank(1) = 14
	const fixedLines = 14
	avail := h - fixedLines
	if avail < 4 {
		avail = 4
	}
	return avail
}

// clampScroll adjusts scrollOffset so the active cursor item remains visible.
func (s *AudioPlayerSettingsScreen) clampScroll(availLines int) {
	// Estimate the line index of each cursor item among all item lines.
	// Section headers + items are listed in order; we approximate line counts.
	// Items that are selected show two lines (content + hint), others show one.
	// We compute the start line of cursor item conservatively (assume all
	// non-selected items take 1 line and selected items take 2).
	// For scrolling purposes we just estimate 2 lines per item on average.
	type sectionEntry struct {
		headerLines int // lines for the section header (including trailing blank)
		items       []int
	}
	sections := []sectionEntry{
		{2, []int{apsBassGain, apsBassFreq}},
		{2, []int{apsMidGain, apsMidFreq, apsMidQ}},
		{2, []int{apsTrebGain, apsTrebFreq}},
		{2, []int{apsDelayEnabled, apsDelayDuration, apsDelayFeedback}},
		{2, []int{apsReverbEnabled, apsReverbMix, apsReverbSize, apsReverbDecay, apsReverbTone}},
		{2, []int{apsChorusEnabled, apsChorusDelay, apsChorusRate, apsChorusDepth, apsChorusMix}},
		{2, []int{apsPitchEnabled, apsPitchSemitones}},
		{2, []int{apsBalance, apsPanEnabled, apsPanRate, apsPanDepth}},
		{2, []int{apsVolume}},
		{0, []int{apsReset}},
	}

	// Walk sections to find the approximate start line of cursor's item.
	line := 0
	cursorLine := 0
	for _, sec := range sections {
		line += sec.headerLines
		for _, idx := range sec.items {
			if idx == s.cursor {
				cursorLine = line
			}
			line += 2 // content line + hint line (conservative; non-selected shows 1 but ok to over-estimate)
		}
	}

	// Keep cursor item in view.
	if cursorLine < s.scrollOffset {
		s.scrollOffset = cursorLine
	}
	if cursorLine >= s.scrollOffset+availLines {
		s.scrollOffset = cursorLine - availLines + 2
	}
	if s.scrollOffset < 0 {
		s.scrollOffset = 0
	}
}

func (s *AudioPlayerSettingsScreen) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		return func() tea.Msg { return HideAudioPlayerSettingsMsg{} }
	case "up", "ctrl+p", "w":
		if s.cursor > 0 {
			s.cursor--
		}
		s.clampScroll(s.innerH())
	case "down", "ctrl+n", "s":
		if s.cursor < apsItemCount-1 {
			s.cursor++
		}
		s.clampScroll(s.innerH())
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
		// Toggle boolean items on enter/space too.
		switch s.cursor {
		case apsDelayEnabled:
			s.adjust(1)
		case apsReverbEnabled:
			s.adjust(1)
		case apsChorusEnabled:
			s.adjust(1)
		case apsPitchEnabled:
			s.adjust(1)
		case apsPanEnabled:
			s.adjust(1)
		}
	}
	return nil
}

func (s *AudioPlayerSettingsScreen) adjust(delta int) {
	switch s.cursor {
	// ── EQ ──────────────────────────────────────────────────────────────────────
	case apsBassGain, apsBassFreq, apsMidGain, apsMidFreq, apsMidQ, apsTrebGain, apsTrebFreq:
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
// settings. The graph is graphH rows tall and graphW columns wide.
// Frequency axis is logarithmic from 20 Hz to 20 kHz.
// Y axis spans ±graphDBRange dB with the 0 dB line at the centre.
func renderEQGraph(eq voice.EQSettings, graphW, graphH int) []string {
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
			switch ch {
			case '*':
				if gains[c] >= 0 {
					rowStr.WriteString(curveAbove.Render("●"))
				} else {
					rowStr.WriteString(curveBelow.Render("●"))
				}
			case '-':
				rowStr.WriteString(zeroStyle.Render("─"))
			default:
				rowStr.WriteByte(' ')
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

// ── Render ────────────────────────────────────────────────────────────────────

func (s *AudioPlayerSettingsScreen) Render() string {
	innerW := s.width - 8
	innerH := s.height - 6
	if innerW < 30 {
		innerW = 30
	}
	if innerH < 12 {
		innerH = 12
	}

	eq := voice.GetPlaybackEQSettings()
	del := voice.GetDelaySettings()
	rev := voice.GetReverbSettings()
	cho := voice.GetChorusSettings()
	pitch := voice.GetPlaybackPitchSettings()
	vol := voice.GetPlaybackEQVolume()

	dim := styles.DimText
	active := lipgloss.NewStyle().Foreground(lipgloss.Color("#c4b5fd")).Bold(true)
	onStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#86efac")).Bold(true)
	offStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#f87171"))

	// ── Fixed header ───────────────────────────────────────────────────────────
	var headerLines []string

	title := styles.AudioPlayerBadge.Render("PLAYER EQ")
	escHint := dim.Render("ESC to exit")
	gap := innerW - lipgloss.Width(title) - lipgloss.Width(escHint)
	if gap < 1 {
		gap = 1
	}
	headerLines = append(headerLines, title+strings.Repeat(" ", gap)+escHint, "")

	// ── EQ Frequency Response Graph ─────────────────────────────────────────
	const graphH = 9
	graphW := innerW - 8 // 8 = len("      │") label column
	if graphW < 10 {
		graphW = 10
	}
	for _, row := range renderEQGraph(eq, graphW, graphH) {
		headerLines = append(headerLines, row)
	}
	headerLines = append(headerLines, "")

	// ── Build scrollable item lines ────────────────────────────────────────────
	var itemLines []string
	// cursorLineOf maps cursor index → first line index in itemLines
	cursorLineOf := make(map[int]int)

	renderItem := func(idx int, label, slider, value, hint string) {
		const lblW = 13
		paddedLbl := fmt.Sprintf("%-*s", lblW, label)
		content := paddedLbl + slider + "  " + value

		cursorLineOf[idx] = len(itemLines)
		if s.cursor == idx {
			prefix := styles.CursorStyle.Render("▶ ")
			itemLines = append(itemLines, prefix+active.Render(content))
			itemLines = append(itemLines, "  "+dim.Render("  ◀▶/ad "+hint))
		} else {
			itemLines = append(itemLines, "  "+dim.Render(content))
		}
	}

	renderBool := func(idx int, label string, enabled bool, hint string) {
		const lblW = 13
		paddedLbl := fmt.Sprintf("%-*s", lblW, label)

		cursorLineOf[idx] = len(itemLines)
		var valueStr string
		if enabled {
			valueStr = onStyle.Render("[ON] ")
		} else {
			valueStr = offStyle.Render("[OFF]")
		}
		content := paddedLbl + "              " + valueStr
		if s.cursor == idx {
			prefix := styles.CursorStyle.Render("▶ ")
			itemLines = append(itemLines, prefix+active.Render(paddedLbl)+"              "+valueStr)
			itemLines = append(itemLines, "  "+dim.Render("  ◀▶/ad "+hint))
		} else {
			_ = content
			itemLines = append(itemLines, "  "+dim.Render(paddedLbl)+"              "+valueStr)
		}
	}

	// ── Bass ──────────────────────────────────────────────────────────────────
	itemLines = append(itemLines, apsHeader("BASS  ±18 dB", innerW))
	renderItem(apsBassGain, "Bass Gain",
		gainSliderAPS(eq.BassGain, bassGainMin, bassGainMax),
		gainLabel(eq.BassGain), "adjust  •  ±18 dB max")
	renderItem(apsBassFreq, "Bass Freq",
		freqSliderAPS(eq.BassHz, bassFreqMin, bassFreqMax, bassFreqStep),
		fmt.Sprintf("%d Hz", int(eq.BassHz)), "shelf corner frequency")
	itemLines = append(itemLines, "")

	// ── Mid ───────────────────────────────────────────────────────────────────
	itemLines = append(itemLines, apsHeader("MID  ±18 dB", innerW))
	renderItem(apsMidGain, "Mid Gain",
		gainSliderAPS(eq.MidGain, midGainMin, midGainMax),
		gainLabel(eq.MidGain), "adjust  •  ±18 dB max")
	renderItem(apsMidFreq, "Mid Freq",
		freqSliderAPS(eq.MidHz, midFreqMin, midFreqMax, midFreqStep),
		fmt.Sprintf("%d Hz", int(eq.MidHz)), "peak center frequency")
	renderItem(apsMidQ, "Mid Q",
		qSliderAPS(eq.MidQ),
		fmt.Sprintf("%.1f", eq.MidQ), "bandwidth  •  higher = narrower peak")
	itemLines = append(itemLines, "")

	// ── Treble ────────────────────────────────────────────────────────────────
	itemLines = append(itemLines, apsHeader("TREBLE  ±18 dB", innerW))
	renderItem(apsTrebGain, "Treble Gain",
		gainSliderAPS(eq.TrebleGain, trebGainMin, trebGainMax),
		gainLabel(eq.TrebleGain), "adjust  •  ±18 dB max")
	renderItem(apsTrebFreq, "Treble Freq",
		freqSliderAPS(eq.TrebleHz, trebFreqMin, trebFreqMax, trebFreqStep),
		fmt.Sprintf("%d Hz", int(eq.TrebleHz)), "shelf corner frequency")
	itemLines = append(itemLines, "")

	// ── Delay ─────────────────────────────────────────────────────────────────
	itemLines = append(itemLines, apsHeader("DELAY", innerW))
	renderBool(apsDelayEnabled, "Enabled", del.Enabled, "toggle on/off")
	renderItem(apsDelayDuration, "Delay",
		f32SliderAPS(del.DelayMs, delayDurMin, delayDurMax, delayDurStep),
		fmt.Sprintf("%.0f ms", del.DelayMs), "echo spacing (1–500 ms)")
	renderItem(apsDelayFeedback, "Feedback",
		f32SliderAPS(del.Feedback, delayFeedbackMin, delayFeedbackMax, delayFeedbackStep),
		fmt.Sprintf("%.2f", del.Feedback), "echo decay  •  0=none  0.95=long tail")
	itemLines = append(itemLines, "")

	// ── Reverb ────────────────────────────────────────────────────────────────
	itemLines = append(itemLines, apsHeader("REVERB", innerW))
	renderBool(apsReverbEnabled, "Enabled", rev.Enabled, "toggle on/off")
	renderItem(apsReverbMix, "Mix",
		f32SliderAPS(rev.Mix, reverbMixMin, reverbMixMax, reverbMixStep),
		fmt.Sprintf("%.2f", rev.Mix), "wet/dry blend (0=dry  1=wet)")
	renderItem(apsReverbSize, "Size",
		f32SliderAPS(rev.Size, reverbSizeMin, reverbSizeMax, reverbSizeStep),
		fmt.Sprintf("%.1f", rev.Size), "room size  •  0.5=small  2.0=large")
	renderItem(apsReverbDecay, "Decay",
		f32SliderAPS(rev.Decay, reverbDecayMin, reverbDecayMax, reverbDecayStep),
		fmt.Sprintf("%.2f", rev.Decay), "tail length  •  0=short  1.0=long")
	renderItem(apsReverbTone, "Tone",
		f32SliderAPS(rev.Tone, reverbToneMin, reverbToneMax, reverbToneStep),
		fmt.Sprintf("%.2f", rev.Tone), "brightness  •  0=dark  1.0=bright")
	itemLines = append(itemLines, "")

	// ── Chorus ────────────────────────────────────────────────────────────────
	itemLines = append(itemLines, apsHeader("CHORUS", innerW))
	renderBool(apsChorusEnabled, "Enabled", cho.Enabled, "toggle on/off")
	renderItem(apsChorusDelay, "Base Delay",
		f32SliderAPS(cho.BaseDelayMs, chorusDelayMin, chorusDelayMax, chorusDelayStep),
		fmt.Sprintf("%.0f ms", cho.BaseDelayMs), "center of modulated delay (5–30 ms)")
	renderItem(apsChorusRate, "LFO Rate",
		f32SliderAPS(cho.RateHz, chorusRateMin, chorusRateMax, chorusRateStep),
		fmt.Sprintf("%.1f Hz", cho.RateHz), "LFO speed (0.1–5.0 Hz)")
	renderItem(apsChorusDepth, "Depth",
		f32SliderAPS(cho.DepthMs, chorusDepthMin, chorusDepthMax, chorusDepthStep),
		fmt.Sprintf("%.1f ms", cho.DepthMs), "delay modulation range (0–15 ms)")
	renderItem(apsChorusMix, "Mix",
		f32SliderAPS(cho.Mix, chorusMixMin, chorusMixMax, chorusMixStep),
		fmt.Sprintf("%.2f", cho.Mix), "wet/dry blend (0=dry  1=wet)")
	itemLines = append(itemLines, "")

	// ── Pitch ─────────────────────────────────────────────────────────────────
	itemLines = append(itemLines, apsHeader("PITCH", innerW))
	renderBool(apsPitchEnabled, "Enabled", pitch.Enabled, "toggle on/off")
	renderItem(apsPitchSemitones, "Semitones",
		f32SliderAPS(pitch.Semitones, pitchMin, pitchMax, pitchStep),
		fmt.Sprintf("%+.1f st", pitch.Semitones), "pitch shift (-12 to +12 semitones)")
	itemLines = append(itemLines, "")

	// ── Balance / Auto Pan ────────────────────────────────────────────────────
	pan := voice.GetPannerSettings()
	itemLines = append(itemLines, apsHeader("PAN", innerW))
	renderItem(apsBalance, "Balance",
		f32SliderAPS(pan.Balance, balanceMin, balanceMax, balanceStep),
		balanceLabel(pan.Balance), "L/R balance  •  L 100% … center … R 100%")
	renderBool(apsPanEnabled, "Auto Pan", pan.AutoPanEnabled, "LFO panning — wobbles balance over time")
	renderItem(apsPanRate, "Rate",
		f32SliderAPS(pan.AutoPanRate, panRateMin, panRateMax, panRateStep),
		fmt.Sprintf("%.2f Hz", pan.AutoPanRate), "LFO speed (0.05–5.0 Hz)")
	renderItem(apsPanDepth, "Depth",
		f32SliderAPS(pan.AutoPanDepth, panDepthMin, panDepthMax, panDepthStep),
		fmt.Sprintf("%.0f%%", pan.AutoPanDepth*100), "sweep range (0=off  100%=full L↔R)")
	itemLines = append(itemLines, "")

	// ── Output Volume ─────────────────────────────────────────────────────────
	itemLines = append(itemLines, apsHeader("OUTPUT", innerW))
	renderItem(apsVolume, "Volume",
		volSliderAPS(vol),
		fmt.Sprintf("%d%%", vol), "output level  •  0–800%  (raise to compensate for EQ boost)")
	// Limiter is always-on; show it as a read-only status line.
	itemLines = append(itemLines, "  "+dim.Render(fmt.Sprintf("%-13s              ", "Limiter"))+
		lipgloss.NewStyle().Foreground(lipgloss.Color("#86efac")).Bold(true).Render("[ON] ")+
		dim.Render("peak limiter + tanh  •  always active"))
	itemLines = append(itemLines, "")

	// ── Reset ─────────────────────────────────────────────────────────────────
	cursorLineOf[apsReset] = len(itemLines)
	if s.cursor == apsReset {
		itemLines = append(itemLines, styles.CursorStyle.Render("▶ ")+
			lipgloss.NewStyle().Foreground(lipgloss.Color("#f87171")).Bold(true).Render("Reset to Defaults"))
	} else {
		itemLines = append(itemLines, "  "+dim.Render("Reset to Defaults"))
	}

	// ── Scroll ────────────────────────────────────────────────────────────────
	// Compute available lines for the scrollable section.
	availLines := innerH - len(headerLines)
	if availLines < 4 {
		availLines = 4
	}

	// Ensure cursor line is visible (exact lookup now available).
	// Scroll one extra line upward so the section header immediately above a
	// first-in-section item is never clipped.
	if cl, ok := cursorLineOf[s.cursor]; ok {
		show := cl - 1 // include the line above (section header or prev item)
		if show < 0 {
			show = 0
		}
		if show < s.scrollOffset {
			s.scrollOffset = show
		}
		if cl+1 >= s.scrollOffset+availLines {
			s.scrollOffset = cl + 2 - availLines
		}
		if s.scrollOffset < 0 {
			s.scrollOffset = 0
		}
	}

	// Slice visible item lines.
	visible := itemLines
	if s.scrollOffset > 0 {
		if s.scrollOffset >= len(visible) {
			s.scrollOffset = len(visible) - 1
		}
		visible = visible[s.scrollOffset:]
	}
	if len(visible) > availLines {
		visible = visible[:availLines]
	}

	// ── Assemble ──────────────────────────────────────────────────────────────
	allLines := append(headerLines, visible...)
	// Clip to available height (belt-and-suspenders).
	if len(allLines) > innerH {
		allLines = allLines[:innerH]
	}
	body := strings.Join(allLines, "\n")

	_ = onOffLabel // used via renderBool directly; keep compiler happy
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

func apsHeader(label string, innerW int) string {
	text := styles.VoiceSettingsActiveSectionStyle.Render(label)
	used := lipgloss.Width(text) + 1
	fill := innerW - used
	if fill < 0 {
		fill = 0
	}
	return text + styles.DimText.Render(" "+strings.Repeat("─", fill))
}
