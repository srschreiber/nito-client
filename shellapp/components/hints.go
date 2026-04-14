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
	case SwitchTabMsg:
		h.chatMode = m.Tab == TabChat
		if m.Tab != TabDM {
			h.dmMode = false
			h.dmTarget = ""
		}
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
	k := styles.HintKeyStyle
	d := styles.DimText
	sep := d.Render("  •  ")

	var lines []string
	switch h.focusedComp {
	case 0, 1: // history / tabs / status
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
				styles.CommandsBadge.Render("COMMANDS"),
				k.Render(".createroom") + d.Render(" --name <name>"),
				k.Render(".invite") + d.Render(" --user <username>"),
				k.Render(".play") + d.Render(" --mp3-or-m3u-or-alias <url|alias>"),
				k.Render(".playalias") + d.Render(" --alias <n> --url <url>"),
				k.Render(".delplayalias") + d.Render(" <name>"),
				k.Render(".image") + d.Render(" <file> [-h <height>]"),
				k.Render(".jump") + d.Render(" <line>") + sep + k.Render(".dm") + d.Render(" <user>"),
				k.Render(".stoptrack") + d.Render(" <0-2>") + sep + k.Render(".stopall"),
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

	// Always append the "/" quick-focus hint.
	lines = append(lines, k.Render("/")+" "+d.Render("quick-select input"))

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
		BorderForeground(styles.PanelBorderColor).
		Background(styles.ComponentBg).
		Padding(0, 1).
		Width(h.width + 4).
		Height(h.height).
		Render(body)
}
