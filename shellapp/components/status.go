// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package components

import (
	"fmt"
	"math"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/srschreiber/nito-client/shellapp/styles"
	"github.com/srschreiber/nito-client/shellapp/types"
	"github.com/srschreiber/nito-client/shellapp/voice"
)

// Cursor layout (inRoom):
//
//	0-2          : track slots
//	3            : Stop All button
//	4 .. 4+4     : alias slots 0-4
//	4+5 = 9      : Sound Alias button
const (
	cursorStopAll       = 3
	cursorAliasBase     = 4
	cursorSoundAlias    = cursorAliasBase + 5  // = 9
	cursorDelSoundAlias = cursorSoundAlias + 1 // = 10
)

// statusVoiceStatsTickMsg fires every second while a voice call is active.
type statusVoiceStatsTickMsg struct{}

// meterTickMsg fires every ~50 ms while at least one audio track is playing,
// driving the animated level-meter display in the TRACKS section.
type meterTickMsg struct{}

type StatusComponent struct {
	connected      bool
	brokerURL      string
	userID         string
	latencyMs      int64
	focused        bool
	width          int
	height         int
	trackPlaying   [3]bool
	trackStartedBy [3]string // username who started the track; "" if idle
	trackBroadcast [3]bool   // true if the track was network-broadcast
	trackCursor    int
	inRoom         bool
	aliases        []AliasEntry // always 5 entries; empty Name = unfilled slot
	voiceActive    bool
	recvPktsPerSec float64
	lossPercent    float64 // packet loss percentage over the last second
	wobblePhase1   float64 // LFO phase for left-bar wobble (radians)
	wobblePhase2   float64 // LFO phase for right-bar wobble (radians)
}

func NewStatusComponent(width, height int) *StatusComponent {
	return &StatusComponent{width: width, height: height}
}

func (s *StatusComponent) voiceStatsTick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return statusVoiceStatsTickMsg{} })
}

func (s *StatusComponent) meterTickCmd() tea.Cmd {
	return tea.Tick(50*time.Millisecond, func(time.Time) tea.Msg { return meterTickMsg{} })
}

// trackMeterBars returns a 6-cell braille spectrum bar graph for a single
// track. Each cell corresponds to a frequency band (sub-bass → air) and fills
// vertically in whole rows of 2 dots based on that band's energy.
// Braille chars are EAW="Neutral" (always 1 terminal column) so the enclosing
// box width calculation is never thrown off.
func (s *StatusComponent) trackMeterBars(track int) string {
	t := float64(track)
	n := voice.NumBands()

	// Small per-band wobble: subtle independent animation per band even during
	// spectrally-flat passages. Generated for any band count via a simple formula
	// so it works regardless of how many bands the terminal currently requests.
	wobbles := make([]float32, n)
	for i := range wobbles {
		phase := float64(i) / float64(max(n-1, 1)) * math.Pi
		amp := 0.02 + 0.02*math.Sin(phase) // 0.02–0.04 range
		wobbles[i] = float32(amp * math.Sin(s.wobblePhase1+t+phase) * math.Cos(s.wobblePhase2+phase))
	}

	// Color thresholds: green → orange → red as level rises.
	// Each braille cell is rendered individually so the color follows the level.
	var sb strings.Builder
	for band := 0; band < n; band++ {
		raw := voice.GetTrackBandLevel(track, band)
		// 8× pre-gain + sqrt perceptual curve: bandpass output amplitude is
		// typically 0.01–0.10 at normal listening volumes; this maps it to a
		// visible fraction of the bar range.
		boosted := raw * 8.0
		if boosted > 1 {
			boosted = 1
		}
		display := float32(math.Sqrt(float64(boosted))) + wobbles[band]
		if display < 0 {
			display = 0
		} else if display > 1 {
			display = 1
		}
		cell := meterCell(display)
		var hex string
		switch {
		case display < 0.55:
			hex = "#4ade80" // green — low level
		case display < 0.82:
			hex = "#f97316" // orange — medium level
		default:
			hex = "#f87171" // red — high level
		}
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(hex)).Render(string(cell)))
	}
	return sb.String()
}

// meterCell maps a level in [0,1] to a braille pattern that fills from the
// bottom up in complete rows of 2 dots. 5 states (0–4 rows filled):
//
//	⠀ U+2800  0 rows — silent
//	⣀ U+28C0  1 row  — dots 7,8   (bottom row)
//	⣤ U+28E4  2 rows — dots 7,8 + 3,6
//	⣶ U+28F6  3 rows — dots 7,8 + 3,6 + 2,5
//	⣿ U+28FF  4 rows — all dots
func meterCell(level float32) rune {
	cells := [5]rune{'\u2800', '\u28C0', '\u28E4', '\u28F6', '\u28FF'}
	idx := int(level * 5)
	if idx < 0 {
		idx = 0
	} else if idx >= 5 {
		idx = 4
	}
	return cells[idx]
}

func (s *StatusComponent) SetSize(width, height int) {
	s.width = width
	s.height = height
	// Set the number of spectrum bands to fill roughly half the panel width.
	// Each braille cell is 1 terminal column; the track line has 7 cols of fixed
	// overhead (cur + icon + space + digit + space + 2 buffer), so the rest is
	// split evenly between the meter and the "started by" label.
	n := width/2 - 4
	if n < 4 {
		n = 4
	}
	voice.SetNumBands(n)
}

func (s *StatusComponent) Init() tea.Cmd { return nil }

func (s *StatusComponent) SetFocused(focused bool) {
	s.focused = focused
}

func (s *StatusComponent) Update(msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case types.ConnectionStatusMsg:
		s.connected = m.Connected
		s.brokerURL = m.BrokerURL
		s.userID = m.UserID
		s.latencyMs = m.LatencyMs
	case TrackStateMsg:
		wasAnyPlaying := s.trackPlaying[0] || s.trackPlaying[1] || s.trackPlaying[2]
		s.trackPlaying = m.Playing
		s.trackStartedBy = m.StartedBy
		s.trackBroadcast = m.Broadcast
		s.inRoom = m.InRoom
		s.aliases = m.Aliases
		// Start the meter tick when audio goes from silent to playing.
		nowAnyPlaying := s.trackPlaying[0] || s.trackPlaying[1] || s.trackPlaying[2]
		if nowAnyPlaying && !wasAnyPlaying {
			return s.meterTickCmd()
		}
	case meterTickMsg:
		// Advance LFO phases for side-bar wobble.
		s.wobblePhase1 += 0.25
		s.wobblePhase2 += 0.19
		// Re-schedule as long as at least one track is playing.
		if s.trackPlaying[0] || s.trackPlaying[1] || s.trackPlaying[2] {
			return s.meterTickCmd()
		}
	case roomsVoiceResultMsg:
		wasActive := s.voiceActive
		if m.err == nil {
			s.voiceActive = m.joined
		}
		if s.voiceActive && !wasActive {
			return s.voiceStatsTick()
		}
		if !s.voiceActive {
			s.recvPktsPerSec = 0
			s.lossPercent = 0
		}
	case statusVoiceStatsTickMsg:
		if s.voiceActive {
			recv, lost := voice.DrainRecvLossStats()
			s.recvPktsPerSec = float64(recv)
			total := recv + lost
			if total > 0 {
				p := float64(lost) / float64(total) * 100
				if p < 1.0 {
					p = 1.0
				}
				s.lossPercent = p
			} else {
				s.lossPercent = 1.0
			}
			return s.voiceStatsTick()
		}
	case tea.KeyPressMsg:
		if !s.focused {
			return nil
		}
		maxCursor := cursorDelSoundAlias
		switch m.String() {
		case "up", "k":
			for next := s.trackCursor - 1; next >= 0; next-- {
				if !s.isEmptyAlias(next) {
					s.trackCursor = next
					break
				}
			}
		case "down", "j":
			for next := s.trackCursor + 1; next <= maxCursor; next++ {
				if !s.isEmptyAlias(next) {
					s.trackCursor = next
					break
				}
			}
		case "enter":
			return s.activate()
		}
	}
	return nil
}

// isEmptyAlias reports whether cursor position pos should be skipped during navigation.
// Only alias slots (cursorAliasBase..cursorSoundAlias-1) with an empty Name are skipped.
func (s *StatusComponent) isEmptyAlias(pos int) bool {
	if pos >= cursorAliasBase && pos < cursorSoundAlias {
		idx := pos - cursorAliasBase
		return idx >= len(s.aliases) || s.aliases[idx].Name == ""
	}
	return false
}

func (s *StatusComponent) activate() tea.Cmd {
	t := s.trackCursor
	switch {
	case t <= 2:
		if s.trackPlaying[t] {
			return func() tea.Msg { return StopAudioMsg{Track: t} }
		}
		const urlFlag = ".play --mp3-or-m3u-or-alias "
		text := fmt.Sprintf("%s --track %d --broadcast false", urlFlag, t)
		return func() tea.Msg {
			return PreFillCommandMsg{Text: text, CursorPos: len(urlFlag)}
		}
	case t == cursorStopAll:
		return func() tea.Msg { return StopAudioMsg{Track: -1} }
	case t >= cursorAliasBase && t < cursorSoundAlias:
		idx := t - cursorAliasBase
		if idx < len(s.aliases) && s.aliases[idx].Name != "" {
			name := s.aliases[idx].Name
			const pfx = ".play --mp3-or-m3u-or-alias "
			text := pfx + name + " --broadcast false"
			return func() tea.Msg { return PreFillCommandMsg{Text: text, CursorPos: -1} }
		}
	case t == cursorSoundAlias:
		const pfx = ".playalias --alias "
		return func() tea.Msg {
			return PreFillCommandMsg{Text: pfx + " --url ", CursorPos: len(pfx)}
		}
	case t == cursorDelSoundAlias:
		const pfx = ".delplayalias --alias "
		return func() tea.Msg {
			return PreFillCommandMsg{Text: pfx, CursorPos: -1}
		}
	}
	return nil
}

func (s *StatusComponent) Render() string {
	c := styles.CursorStyle
	k := styles.HintKeyStyle
	d := lipgloss.NewStyle().Foreground(lipgloss.Color("#8888b8"))

	// STATUS section
	label := styles.StatusBadge.Render("STATUS")
	var statusLine string
	if s.connected {
		latency := fmt.Sprintf("%dms", s.latencyMs)
		dimVal := lipgloss.NewStyle().Foreground(lipgloss.Color("#8888b8"))
		lbl := lipgloss.NewStyle().Foreground(lipgloss.Color("#7dd3fc"))
		brokerDisplay := strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(s.brokerURL, "https://"), "http://"), "www.")
		// "● online" = 8 visible chars; pad 4 so ping value starts at col 12, same as broker/user values.
		statusLine = styles.StatusConnectedStyle.Render("● online") +
			dimVal.Render("    "+latency) +
			"\n" + lbl.Render("⌁ "+fmt.Sprintf("%-9s", "broker")) + dimVal.Render(" "+brokerDisplay) +
			"\n" + lbl.Render("◉ "+fmt.Sprintf("%-9s", "user")) + dimVal.Render(" "+s.userID)
	} else {
		statusLine = styles.StatusDisconnectedStyle.Render("● offline")
	}

	body := label + "\n" + statusLine

	if s.voiceActive {
		// VOICE section — live packet loss rate
		voiceLabel := styles.VoiceBadge.Render("VOICE")
		dimVal := lipgloss.NewStyle().Foreground(lipgloss.Color("#8888b8"))
		lossStr := fmt.Sprintf("%.1f%%", s.lossPercent)
		voiceLine := dimVal.Render(fmt.Sprintf("  %.0f pkt/s  loss %s", s.recvPktsPerSec, lossStr))
		body += "\n\n" + voiceLabel + "\n" + voiceLine
	}

	// TRACKS section — always visible so local audio is always accessible.
	{
		tracksLabel := styles.TracksBadge.Render("TRACKS")
		var trackLines []string
		for i := 0; i < 3; i++ {
			cur := "  "
			if s.focused && i == s.trackCursor {
				cur = c.Render("> ")
			}
			var icon string
			if s.trackPlaying[i] {
				// "♪ " = 2 terminal cols (narrow Unicode note + space).
				// Using a plain emoji like 🔊 is EAW="Wide" (2 terminal cols) but
				// lipgloss counts it as 1, causing the box border to overflow.
				icon = k.Render("♪ ")
			} else {
				// "• " = bullet + space = 2 terminal cols, always narrow.
				icon = lipgloss.NewStyle().
					Foreground(lipgloss.Color("#f87171")).
					Render("• ")
			}
			// "Started by" and per-track level meter — both plain ASCII (zero ANSI)
			// so lipgloss's box-fill width calculation is never thrown off by a
			// trailing reset sequence inside a styled substring.
			byLabel := ""
			if s.trackStartedBy[i] != "" {
				var raw string
				if s.trackStartedBy[i] == s.userID {
					if s.trackBroadcast[i] {
						raw = "  You (broadcasting)"
					} else {
						raw = "  You"
					}
				} else {
					raw = "  by: " + s.trackStartedBy[i]
				}
				maxByW := s.width - 7 - voice.NumBands() - 2 // cur(2)+icon(2)+space+digit+space+N_bars+buffer(2)
				if maxByW < 0 {
					maxByW = 0
				}
				byLabel = truncate(raw, maxByW)
			}
			// Inline level meter: 6 braille bars to the right of the track number,
			// each reflecting the live audio level of that track's stream.
			meter := ""
			if s.trackPlaying[i] {
				meter = " " + s.trackMeterBars(i)
			}
			trackLines = append(trackLines, fmt.Sprintf("%s%s %d%s%s", cur, icon, i, meter, byLabel))
		}
		// Stop All button
		stopAllCur := "  "
		if s.focused && s.trackCursor == cursorStopAll {
			stopAllCur = c.Render("> ")
		}
		stopAllBtn := stopAllCur + styles.VoiceLeaveStyle.Render("⏹ Stop All") +
			lipgloss.NewStyle().Background(lipgloss.Color("#450a0a")).Render(" ")
		body += "\n\n" + tracksLabel + "\n\n" + strings.Join(trackLines, "\n\n") + "\n\n" + stopAllBtn

		// PLAY ALIASES section
		aliasesLabel := styles.PlayAliasesBadge.Render("PLAY ALIASES")
		var aliasLines []string
		for i, a := range s.aliases {
			cur := "  "
			if s.focused && s.trackCursor == cursorAliasBase+i {
				cur = c.Render("> ")
			}
			var label string
			if a.Name == "" {
				label = d.Render("- <empty>")
			} else {
				maxNameW := s.width - 2 // 2 for cursor
				if maxNameW < 3 {
					maxNameW = 3
				}
				label = d.Render(truncate(a.Name, maxNameW))
			}
			aliasLines = append(aliasLines, cur+label)
		}
		// Sound Alias button
		soundAliasCur := "  "
		if s.focused && s.trackCursor == cursorSoundAlias {
			soundAliasCur = c.Render("> ")
		}
		soundAliasBtn := lipgloss.NewStyle().
			Background(lipgloss.Color("54")).
			Foreground(lipgloss.Color("225")).
			PaddingLeft(1).
			Render("+ Sound Alias") +
			lipgloss.NewStyle().Background(lipgloss.Color("54")).Render(" ")
		aliasLines = append(aliasLines, "\n"+soundAliasCur+soundAliasBtn)

		// Del Sound Alias button
		delSoundAliasCur := "  "
		if s.focused && s.trackCursor == cursorDelSoundAlias {
			delSoundAliasCur = c.Render("> ")
		}
		delSoundAliasBtn := styles.VoiceLeaveStyle.Render("- Sound Alias") +
			lipgloss.NewStyle().Background(lipgloss.Color("#450a0a")).Render(" ")
		aliasLines = append(aliasLines, "\n"+delSoundAliasCur+delSoundAliasBtn)
		body += "\n\n" + aliasesLabel + "\n" + strings.Join(aliasLines, "\n")
	}

	// Clip to available height.
	maxLines := s.height - 5
	if maxLines < 1 {
		maxLines = 1
	}
	bodyLines := strings.Split(body, "\n")
	if len(bodyLines) > maxLines {
		bodyLines = bodyLines[:maxLines]
	}
	body = strings.Join(bodyLines, "\n")

	borderColor := styles.PanelBorderColor
	bg := styles.ComponentBg
	if s.focused {
		borderColor = styles.PanelFocusedBorderColor
		bg = styles.ComponentFocusedBg
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		BorderBackground(bg).
		Background(bg).
		Padding(0, 1).
		Width(s.width + 4).
		Height(s.height).
		Render(body)
}
