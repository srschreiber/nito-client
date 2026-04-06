// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package components

import (
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/srschreiber/nito-client/shellapp/styles"
)

// HintsComponent renders context-sensitive keybinding hints for the focused component.
type HintsComponent struct {
	focusedComp int // comps index: 0=history, 1=rooms, 4=command
	chatMode    bool
	dmMode      bool
	dmTarget    string
	width       int
	height      int
}

func NewHintsComponent(width, height int) *HintsComponent {
	return &HintsComponent{width: width, height: height, focusedComp: 2}
}

func (h *HintsComponent) SetSize(width, height int) {
	h.width = width
	h.height = height
}

func (h *HintsComponent) SetFocused(_ bool) {}

func (h *HintsComponent) SetFocusedComp(idx int) {
	h.focusedComp = idx
}

func (h *HintsComponent) Init() tea.Cmd { return nil }

func (h *HintsComponent) Update(msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case ModeChangedMsg:
		h.chatMode = m.ChatMode
		h.dmMode = false
		h.dmTarget = ""
	case DMTargetChangedMsg:
		if m.User != "" {
			h.dmMode = true
			h.dmTarget = m.User
		} else {
			h.dmMode = false
			h.dmTarget = ""
		}
	}
	return nil
}

func (h *HintsComponent) Render() string {
	k := lipgloss.NewStyle().Foreground(lipgloss.Color("222")).Bold(true)
	d := styles.DimText
	sep := d.Render("  •  ")

	var lines []string
	switch h.focusedComp {
	case 0: // history / tabs
		if h.dmMode {
			lines = []string{
				k.Render("←/→") + d.Render("  switch tabs"),
				k.Render("↑/↓") + d.Render(" / ") + k.Render("ctrl+p/n") + d.Render("  select user"),
				k.Render("enter") + d.Render("  send DM to ") + k.Render(h.dmTarget),
			}
		} else {
			lines = []string{
				k.Render("←/→") + d.Render("  switch tabs"),
				k.Render("↑/↓") + d.Render(" / ") + k.Render("ctrl+p/n") + d.Render("  scroll"),
				k.Render("jump -L <n>") + d.Render("  go to line"),
			}
		}
	default: // command (idx 2)
		nav := k.Render("ctrl+b/f") + d.Render(" move") + sep + k.Render("ctrl+a/e") + d.Render(" home/end")
		del := k.Render("ctrl+k") + d.Render(" del to end")
		delSingle := k.Render("ctrl+d") + d.Render(" del char")
		if h.dmMode {
			lines = []string{
				nav,
				delSingle + sep + del,
				k.Render("enter") + d.Render(" send DM → ") + k.Render(h.dmTarget),
				d.Render(".dm <user>") + d.Render(" switch target"),
			}
		} else if h.chatMode {
			lines = []string{
				nav,
				delSingle + sep + del,
				k.Render("shift+enter") + d.Render(" newline") + sep + k.Render("enter") + d.Render(" send"),
				"",
				styles.KeysBadge.Render("COMMANDS"),
				k.Render(".image") + d.Render(" <file> [-h <height>]"),
				k.Render(".dm") + d.Render(" <user>"),
			}
		} else {
			lines = []string{
				k.Render("↑/↓") + d.Render(" / ") + k.Render("ctrl+p/n") + d.Render("  history"),
				nav,
				delSingle + sep + del,
				k.Render("enter") + d.Render("  run command"),
			}
		}
	}

	// Clip lines to available height (reserve 5 rows: title + border/padding overhead).
	maxLines := h.height - 5
	if maxLines < 1 {
		maxLines = 1
	}
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}

	body := styles.KeysBadge.Render("KEYS") + "\n"
	for _, l := range lines {
		body += l + "\n"
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#4a4a7a")).
		Background(styles.ComponentBg).
		Padding(0, 1).
		Width(h.width).
		Height(h.height).
		Render(body)
}
