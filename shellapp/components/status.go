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
	case tea.KeyPressMsg:
		if !s.focused {
			return nil
		}
		switch m.String() {
		case "up", "k":
			if s.trackCursor > 0 {
				s.trackCursor--
			}
		case "down", "j":
			if s.trackCursor < 2 {
				s.trackCursor++
			}
		case "enter":
			t := s.trackCursor
			if s.trackPlaying[t] {
				return func() tea.Msg { return StopAudioMsg{Track: t} }
			}
			// Not playing — pre-fill command with .play; cursor lands after the url flag.
			const urlFlag = ".play --mp3-or-m3u-or-alias "
			text := fmt.Sprintf("%s --track %d", urlFlag, t)
			return func() tea.Msg {
				return PreFillCommandMsg{Text: text, CursorPos: len(urlFlag)}
			}
		}
	}
	return nil
}

func (s *StatusComponent) Render() string {
	k := styles.HintKeyStyle
	d := styles.DimText

	// STATUS section
	label := styles.StatusBadge.Render("STATUS")
	var statusLine string
	if s.connected {
		latency := fmt.Sprintf("%dms", s.latencyMs)
		statusLine = styles.StatusConnectedStyle.Render("● online") +
			"  " + styles.DimText.Render(latency) +
			"\n" + styles.DimText.Render("  "+s.brokerURL) +
			"\n" + styles.DimText.Render("  "+s.userID)
	} else {
		statusLine = styles.StatusDisconnectedStyle.Render("● offline")
	}

	body := label + "\n" + statusLine

	if s.inRoom {
		tracksLabel := styles.KeysBadge.Render("TRACKS")
		var trackLines []string
		for i := 0; i < 3; i++ {
			cursor := "  "
			if s.focused && i == s.trackCursor {
				cursor = k.Render("▶ ")
			}
			var icon string
			if s.trackPlaying[i] {
				icon = k.Render("🔊")
			} else {
				icon = d.Render("⏹")
			}
			trackLines = append(trackLines, fmt.Sprintf("%s%s %s", cursor, icon, d.Render(fmt.Sprintf("%d", i))))
		}
		body += "\n\n" + tracksLabel + "\n" + strings.Join(trackLines, "\n")
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
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Background(bg).
		Padding(0, 1).
		Width(s.width).
		Height(s.height)
	return style.Render(body)
}
