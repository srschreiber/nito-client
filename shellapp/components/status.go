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
	connected bool
	brokerURL string
	userID    string
	latencyMs int64
	focused   bool
	width     int
	height    int
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
	if m, ok := msg.(types.ConnectionStatusMsg); ok {
		s.connected = m.Connected
		s.brokerURL = m.BrokerURL
		s.userID = m.UserID
		s.latencyMs = m.LatencyMs
	}
	return nil
}

func (s *StatusComponent) Render() string {
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

	// Clip to available height (reserve 5 rows for title + border/padding overhead).
	maxLines := s.height - 5
	if maxLines < 1 {
		maxLines = 1
	}
	statusLines := strings.Split(statusLine, "\n")
	if len(statusLines) > maxLines {
		statusLines = statusLines[:maxLines]
	}
	statusLine = strings.Join(statusLines, "\n")

	body := label + "\n" + statusLine

	borderColor := styles.PanelBorderColor
	if s.focused {
		borderColor = styles.PanelFocusedBorderColor
	}
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Background(styles.ComponentBg).
		Padding(0, 1).
		Width(s.width).
		Height(s.height)
	return style.Render(body)
}
