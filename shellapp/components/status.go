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
	spinnerPhase   int     // incremented every meterTick (50 ms); drives the live-buffer spinner
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

// trackMeterBarsBoth returns a top-row and bottom-row block-char string for a
// two-row tall spectrum meter. Each band uses two stacked eighth-block chars
// (▁▂▃▄▅▆▇█) giving 16 effective fill levels with solid fill and no gaps.
// The bottom row sits inline with the track number; the top row is rendered on
// the line immediately above to give bars room to grow.
func (s *StatusComponent) trackMeterBarsBoth(track int) (topRow, bottomRow string) {
	bg := styles.ComponentBg
	if s.focused {
		bg = styles.ComponentFocusedBg
	}

	t := float64(track)
	n := voice.NumBands()

	wobbles := make([]float32, n)
	for i := range wobbles {
		phase := float64(i) / float64(max(n-1, 1)) * math.Pi
		amp := 0.02 + 0.02*math.Sin(phase)
		wobbles[i] = float32(amp * math.Sin(s.wobblePhase1+t+phase) * math.Cos(s.wobblePhase2+phase))
	}

	var topSB, botSB strings.Builder
	for band := 0; band < n; band++ {
		raw := voice.GetTrackBandLevel(track, band)
		boosted := raw * 5.0
		if boosted > 1 {
			boosted = 1
		}
		display := float32(math.Sqrt(float64(boosted))) + wobbles[band]
		if display < 0 {
			display = 0
		} else if display > 1 {
			display = 1
		}

		// 16 ticks total split across two rows of 8 — bottom fills first.
		totalTicks := int(display * 16)
		if totalTicks > 16 {
			totalTicks = 16
		}
		botTicks := totalTicks
		if botTicks > 8 {
			botTicks = 8
		}
		topTicks := totalTicks - botTicks

		hex := "#4ade80"
		// Explicit bg prevents terminal-default bleed between/around bar chars.
		style := lipgloss.NewStyle().Foreground(lipgloss.Color(hex)).Background(bg)
		topSB.WriteString(style.Render(string(meterBlock(topTicks))))
		botSB.WriteString(style.Render(string(meterBlock(botTicks))))
	}
	return topSB.String(), botSB.String()
}

// meterBlock maps 0–8 ticks to a solid eighth-block character (▁▂▃▄▅▆▇█).
// 0 → space (silent), 8 → █ (full). Chars fill solid from the bottom up with
// no gaps, unlike braille dots.
func meterBlock(ticks int) rune {
	blocks := [9]rune{' ', '▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}
	if ticks < 0 {
		ticks = 0
	} else if ticks > 8 {
		ticks = 8
	}
	return blocks[ticks]
}

func (s *StatusComponent) SetSize(width, height int) {
	s.width = width
	s.height = height
	// Set the number of spectrum bands to fill roughly half the panel width.
	// Each braille cell is 1 terminal column; the track line has 7 cols of fixed
	// overhead (cur + icon + space + digit + space + 2 buffer), so the rest is
	// split evenly between the meter and the "started by" label.
	n := width*3/4 - 4
	if n < 4 {
		n = 4
	} else if n > 6 {
		n = 6
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
		// Advance LFO phases for side-bar wobble and spinner animation.
		s.wobblePhase1 += 0.25
		s.wobblePhase2 += 0.19
		s.spinnerPhase++
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
		case "up", "w", "k", "ctrl+p":
			for next := s.trackCursor - 1; next >= 0; next-- {
				if !s.isEmptyAlias(next) {
					s.trackCursor = next
					break
				}
			}
		case "down", "s", "j", "ctrl+n":
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
	borderColor := styles.PanelBorderColor
	bg := styles.ComponentBg
	if s.focused {
		borderColor = styles.PanelFocusedBorderColor
		bg = styles.ComponentFocusedBg
	}

	// Every inner style carries bg so no character bleeds through to the
	// terminal's own background color between/after ANSI reset sequences.
	txt := styles.DimText
	if s.focused {
		txt = styles.DimTextFocused
	}
	lbl := lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color("#7dd3fc"))
	cur := styles.CursorStyle.Background(bg)
	hint := styles.HintKeyStyle.Background(bg)
	red := lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color("#f87171"))
	conn := styles.StatusConnectedStyle.Background(bg)
	disconn := styles.StatusDisconnectedStyle.Background(bg)

	// STATUS section
	label := styles.StatusBadge.Render("STATUS")
	var statusLine string
	if s.connected {
		brokerDisplay := strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(s.brokerURL, "https://"), "http://"), "www.")
		statusLine = conn.Render("● online") +
			txt.Render(fmt.Sprintf("    %dms", s.latencyMs)) +
			"\n" + lbl.Render("⌁ "+fmt.Sprintf("%-9s", "broker")) + txt.Render(" "+brokerDisplay) +
			"\n" + lbl.Render("◉ "+fmt.Sprintf("%-9s", "user")) + txt.Render(" "+s.userID)
	} else {
		statusLine = disconn.Render("● offline")
	}
	body := label + "\n" + statusLine

	if s.voiceActive {
		voiceLabel := styles.VoiceBadge.Render("VOICE")
		lossStr := fmt.Sprintf("%.1f%%", s.lossPercent)
		voiceLine := txt.Render(fmt.Sprintf("  %.0f pkt/s  loss %s", s.recvPktsPerSec, lossStr))
		body += "\n\n" + voiceLabel + "\n" + voiceLine
	}

	// TRACKS section
	{
		tracksLabel := styles.TracksBadge.Render("TRACKS")
		var trackLines []string
		for i := 0; i < 3; i++ {
			curStr := txt.Render("  ")
			if s.focused && i == s.trackCursor {
				curStr = cur.Render("> ")
			}

			var icon string
			if s.trackPlaying[i] {
				// "♪ " = 2 terminal cols (narrow Unicode note + space).
				icon = hint.Render("♪ ")
			} else {
				// "• " = bullet + space = 2 terminal cols, always narrow.
				icon = red.Render("• ")
			}

			liveBadge := ""
			liveBadgeW := 0
			if s.trackPlaying[i] && voice.IsTrackLive(i) {
				liveBadge = txt.Render(" ") + lipgloss.NewStyle().
					Background(lipgloss.Color("#7f1d1d")).
					Foreground(lipgloss.Color("#fca5a5")).
					Bold(true).
					Render("LIVE")
				liveBadgeW = 5 // " LIVE" = 1 space + 4 chars
			}

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
				// cur(2)+icon(2)+digit+space+N_bars+liveBadge+2_buf
				maxByW := s.width - 7 - voice.NumBands() - liveBadgeW - 2
				if maxByW < 0 {
					maxByW = 0
				}
				byLabel = txt.Render(truncate(raw, maxByW))
			}

			// Two-row spectrum meter — all segments carry explicit bg.
			topMeter := ""
			meter := ""
			if s.trackPlaying[i] {
				if voice.IsTrackBuffering(i) {
					spinners := []rune{'◐', '◓', '◑', '◒'}
					frame := (s.spinnerPhase / 5) % len(spinners)
					spin := lipgloss.NewStyle().Foreground(lipgloss.Color("#f97316")).Background(bg).Render(string(spinners[frame]))
					meter = txt.Render(" ") + spin
				} else if !voice.IsTrackLive(i) {
					top, bot := s.trackMeterBarsBoth(i)
					// 7-space indent: cur(2)+icon(2)+space(1)+digit(1)+space(1)
					topMeter = txt.Render("       ") + top
					meter = txt.Render(" ") + bot
				}
			}

			// Digit and separator wrapped so no raw character has an unset background.
			digit := txt.Render(fmt.Sprintf(" %d", i))
			trackInfo := curStr + icon + digit + meter + liveBadge + byLabel
			if topMeter != "" {
				trackLines = append(trackLines, topMeter+"\n"+trackInfo)
			} else {
				trackLines = append(trackLines, trackInfo)
			}
		}

		stopAllCur := txt.Render("  ")
		if s.focused && s.trackCursor == cursorStopAll {
			stopAllCur = cur.Render("> ")
		}
		stopAllBtn := stopAllCur + styles.VoiceLeaveStyle.Render("⏹ Stop All") +
			lipgloss.NewStyle().Background(lipgloss.Color("#450a0a")).Render(" ")
		body += "\n\n" + tracksLabel + "\n\n" + strings.Join(trackLines, "\n\n") + "\n\n" + stopAllBtn

		// PLAY ALIASES section
		aliasesLabel := styles.PlayAliasesBadge.Render("PLAY ALIASES")
		var aliasLines []string
		for i, a := range s.aliases {
			alCur := txt.Render("  ")
			if s.focused && s.trackCursor == cursorAliasBase+i {
				alCur = cur.Render("> ")
			}
			var alLabel string
			if a.Name == "" {
				alLabel = txt.Render("- <empty>")
			} else {
				maxNameW := s.width - 2 // 2 for cursor
				if maxNameW < 3 {
					maxNameW = 3
				}
				alLabel = txt.Render(truncate(a.Name, maxNameW))
			}
			aliasLines = append(aliasLines, alCur+alLabel)
		}

		soundAliasCur := txt.Render("  ")
		if s.focused && s.trackCursor == cursorSoundAlias {
			soundAliasCur = cur.Render("> ")
		}
		soundAliasBtn := lipgloss.NewStyle().
			Background(lipgloss.Color("54")).
			Foreground(lipgloss.Color("225")).
			PaddingLeft(1).
			Render("+ Sound Alias") +
			lipgloss.NewStyle().Background(lipgloss.Color("54")).Render(" ")
		aliasLines = append(aliasLines, "\n"+soundAliasCur+soundAliasBtn)

		delSoundAliasCur := txt.Render("  ")
		if s.focused && s.trackCursor == cursorDelSoundAlias {
			delSoundAliasCur = cur.Render("> ")
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
