// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package components

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/srschreiber/nito-client/shellapp/styles"
)

const (
	// userListOuterW is the fixed visual width of the DM user list pane
	// (inner content 20 + border 2 + padding 2).
	userListOuterW = 24
	userListInnerW = userListOuterW - 4
)

const dmDisclaimer = "⚠  DMs are not persisted. Disconnecting loses all messages. Create a room for persistence."

// DMPane renders the DM tab: a user list on the left and the selected
// user's ConversationHistory on the right.
type DMPane struct {
	users          []string                        // ordered; most-recently-added first
	histories      map[string]*ConversationHistory // per-user
	cursor         int                             // index into users; -1 = none selected
	width          int                             // inner content width (same as histW from layout)
	height         int                             // inner content height (same as histH minus tabBarLines)
	focused        bool
	historyFocused bool // when true, key events go to the history pane; tab toggles
}

func NewDMPane(width, height int) *DMPane {
	return &DMPane{
		histories: make(map[string]*ConversationHistory),
		cursor:    -1,
		width:     width,
		height:    height,
	}
}

func (p *DMPane) SetSize(width, height int) {
	p.width = width
	p.height = height
	// Propagate to all existing per-user histories.
	histW := p.historyInnerW()
	for _, h := range p.histories {
		h.SetSize(histW, height)
	}
}

func (p *DMPane) SetFocused(focused bool) {
	p.focused = focused
	// Always reset sub-focus on any focus change so re-entering the DM tab
	// starts back at the user list.
	if h := p.activeHistory(); h != nil {
		h.SetFocused(false)
	}
	p.historyFocused = false
}

// activeHistory returns the ConversationHistory for the currently selected user, or nil.
func (p *DMPane) activeHistory() *ConversationHistory {
	if p.cursor < 0 || p.cursor >= len(p.users) {
		return nil
	}
	return p.histories[p.users[p.cursor]]
}

func (p *DMPane) Init() tea.Cmd { return nil }

// historyInnerW is the inner content width for per-user ConversationHistory boxes.
// Total visual = userListOuterW + historyOuterW = userListOuterW + (historyInnerW + 4).
// We want: userListOuterW + historyInnerW + 4 = p.width + 4 (matching a standalone ConversationHistory).
// → historyInnerW = p.width - userListOuterW
func (p *DMPane) historyInnerW() int {
	w := p.width - userListOuterW
	if w < 10 {
		w = 10
	}
	return w
}

// EnsureUser adds user to the list if not already present and creates their history.
func (p *DMPane) EnsureUser(user string) {
	if _, ok := p.histories[user]; ok {
		return
	}
	h := NewConversationHistory(p.historyInnerW(), p.height)
	h.chatMode = true // DM histories always show the [chat] badge
	// Seed with the persistence disclaimer as the first visible entry.
	h.entries = []historyEntry{{text: dmDisclaimer, isResponse: true}}
	p.histories[user] = h
	// Prepend so the most recent addition appears at the top.
	p.users = append([]string{user}, p.users...)
	if p.cursor == -1 {
		p.cursor = 0
	}
}

// SelectUser sets the cursor to the given user (creating them if needed).
func (p *DMPane) SelectUser(user string) {
	p.EnsureUser(user)
	for i, u := range p.users {
		if u == user {
			p.cursor = i
			return
		}
	}
}

// SelectedUser returns the currently selected username, or "" if none.
func (p *DMPane) SelectedUser() string {
	if p.cursor < 0 || p.cursor >= len(p.users) {
		return ""
	}
	return p.users[p.cursor]
}

func (p *DMPane) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case AppendDMHistoryMsg:
		p.EnsureUser(msg.User)
		return p.histories[msg.User].Update(AppendHistoryMsg{Entries: msg.Entries})
	case DMTargetChangedMsg:
		if msg.User != "" {
			// Switching user resets history focus back to user list.
			if p.historyFocused {
				if h := p.activeHistory(); h != nil {
					h.SetFocused(false)
				}
				p.historyFocused = false
			}
			p.SelectUser(msg.User)
		}
		return nil
	case tea.KeyPressMsg:
		if !p.focused {
			return nil
		}
		switch msg.String() {
		case "tab":
			// Toggle sub-focus between user list and history pane.
			if h := p.activeHistory(); h != nil {
				p.historyFocused = !p.historyFocused
				h.SetFocused(p.historyFocused)
			}
			return nil
		}
		if p.historyFocused {
			if h := p.activeHistory(); h != nil {
				return h.Update(msg)
			}
			return nil
		}
		if len(p.users) == 0 {
			return nil
		}
		switch msg.String() {
		case "up", "ctrl+p":
			if p.cursor > 0 {
				p.cursor--
				return func() tea.Msg { return DMTargetChangedMsg{User: p.SelectedUser()} }
			}
		case "down", "ctrl+n":
			if p.cursor < len(p.users)-1 {
				p.cursor++
				return func() tea.Msg { return DMTargetChangedMsg{User: p.SelectedUser()} }
			}
		}
	}
	return nil
}

func (p *DMPane) Render() string {
	return lipgloss.JoinHorizontal(lipgloss.Top, p.renderUserList(), p.renderHistory())
}

func (p *DMPane) renderUserList() string {
	borderColor := styles.DMListBorderColor
	if p.focused && !p.historyFocused {
		borderColor = styles.DMListFocusedBorderColor
	}

	title := styles.DMsBadge.Render("DMS")
	var rows []string
	rows = append(rows, title)

	if len(p.users) == 0 {
		rows = append(rows, styles.DimText.Render("No conversations."))
		rows = append(rows, styles.DimText.Render(".dm <user> to start"))
	} else {
		for i, user := range p.users {
			var line string
			if i == p.cursor {
				line = styles.SelectionRowStyle.Render(
					styles.DMSelectedUserStyle.Render(fmt.Sprintf("> %s", user)),
				)
			} else {
				line = styles.DimText.Render(fmt.Sprintf("  %s", user))
			}
			rows = append(rows, line)
		}
	}

	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Background(styles.ComponentBg).
		Padding(0, 1).
		Width(userListInnerW).
		Height(p.height)
	return style.Render(strings.Join(rows, "\n"))
}

func (p *DMPane) renderHistory() string {
	histW := p.historyInnerW()

	if p.cursor < 0 || len(p.users) == 0 {
		hint := styles.DimText.Render("Use .dm <username> or dm -u <username> to start a conversation.")
		style := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(styles.PanelBorderColor).
			Background(styles.ComponentBg).
			Padding(0, 1).
			Width(histW).
			Height(p.height)
		return style.Render(hint)
	}

	user := p.users[p.cursor]
	h := p.histories[user]
	if h == nil {
		style := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(styles.PanelBorderColor).
			Background(styles.ComponentBg).
			Padding(0, 1).
			Width(histW).
			Height(p.height)
		return style.Render(styles.DimText.Render("(loading)"))
	}
	return h.Render()
}
