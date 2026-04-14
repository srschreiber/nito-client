// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package components

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/srschreiber/nito-client/shellapp/styles"
	"github.com/srschreiber/nito-client/shellapp/types"
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

type StatusComponent struct {
	connected    bool
	brokerURL    string
	userID       string
	latencyMs    int64
	focused      bool
	width        int
	height       int
	trackPlaying [3]bool
	trackCursor  int
	inRoom       bool
	aliases      []AliasEntry // always 5 entries; empty Name = unfilled slot
}

func NewStatusComponent(width, height int) *StatusComponent {
	return &StatusComponent{width: width, height: height}
}

func (s *StatusComponent) SetSize(width, height int) {
	s.width = width
	s.height = height
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
		s.trackPlaying = m.Playing
		s.inRoom = m.InRoom
		s.aliases = m.Aliases
	case tea.KeyPressMsg:
		if !s.focused {
			return nil
		}
		maxCursor := 2
		if s.inRoom {
			maxCursor = cursorDelSoundAlias
		}
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
		text := fmt.Sprintf("%s --track %d", urlFlag, t)
		return func() tea.Msg {
			return PreFillCommandMsg{Text: text, CursorPos: len(urlFlag)}
		}
	case t == cursorStopAll:
		return func() tea.Msg { return StopAudioMsg{Track: -1} }
	case t >= cursorAliasBase && t < cursorSoundAlias:
		idx := t - cursorAliasBase
		if idx < len(s.aliases) && s.aliases[idx].Name != "" {
			name := s.aliases[idx].Name
			text := ".play --mp3-or-m3u-or-alias " + name
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
	d := styles.DimText

	// STATUS section
	label := styles.StatusBadge.Render("STATUS")
	var statusLine string
	if s.connected {
		latency := fmt.Sprintf("%dms", s.latencyMs)
		dimVal := lipgloss.NewStyle().Background(styles.ComponentBg).Foreground(lipgloss.Color("#8888b8"))
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

	if s.inRoom {
		// TRACKS section
		tracksLabel := styles.TracksBadge.Render("TRACKS")
		var trackLines []string
		for i := 0; i < 3; i++ {
			cur := "  "
			if s.focused && i == s.trackCursor {
				cur = c.Render("▶ ")
			}
			var icon string
			if s.trackPlaying[i] {
				icon = k.Render("🔊")
			} else {
				icon = lipgloss.NewStyle().
					Background(lipgloss.Color("#450a0a")).
					Foreground(lipgloss.Color("#f87171")).
					Padding(0, 1).
					Render("⏹")
			}
			trackLines = append(trackLines, fmt.Sprintf("%s%s %s", cur, icon, fmt.Sprintf("%d", i)))
		}
		// Stop All button
		stopAllCur := "  "
		if s.focused && s.trackCursor == cursorStopAll {
			stopAllCur = c.Render("▶ ")
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
				cur = c.Render("▶ ")
			}
			var label string
			if a.Name == "" {
				label = d.Render("- <empty>")
			} else {
				label = d.Render(a.Name)
			}
			aliasLines = append(aliasLines, cur+label)
		}
		// Sound Alias button
		soundAliasCur := "  "
		if s.focused && s.trackCursor == cursorSoundAlias {
			soundAliasCur = c.Render("▶ ")
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
			delSoundAliasCur = c.Render("▶ ")
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
		Background(bg).
		Padding(0, 1).
		Width(s.width + 4).
		Height(s.height).
		Render(body)
}
