// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package components

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/srschreiber/nito-client/shellapp/clientlog"
)

// Component is the common interface satisfied by all focusable tab content components.
type Component interface {
	SetFocused(focused bool)
	Init() tea.Cmd
	Update(msg tea.Msg) tea.Cmd
	Render() string
}

// tabBarLines is the number of terminal rows consumed by the tab bar rendered
// above the history box (top border row + text row + bottom border row).
const tabBarLines = 3

var (
	tabsActiveTabBorder = lipgloss.Border{
		Top: "─", Bottom: " ", Left: "│", Right: "│",
		TopLeft: "╭", TopRight: "╮", BottomLeft: "┘", BottomRight: "└",
	}
	tabsInactiveTabBorder = lipgloss.Border{
		Top: "─", Bottom: "─", Left: "│", Right: "│",
		TopLeft: "╭", TopRight: "╮", BottomLeft: "┴", BottomRight: "┴",
	}
)

// tabDefs lists tab names in order (index matches HistoryTab constants).
var tabDefs = []string{"CMD", "Room Chat", "DMs", "Notifications", "Invites", "Logs"}

// ConversationTabs wraps separate conversation panes for all tabs.
// It implements the Component interface as a drop-in replacement for ConversationHistory.
type ConversationTabs struct {
	cmd           *ConversationHistory
	roomChat      *RoomChatPane
	dmPane        *DMPane
	notifications *ConversationHistory
	invites       *InvitesPane
	logs          *ConversationHistory
	active        HistoryTab
	width         int
	height        int
	focused       bool
}

func NewConversationTabs(width, height int) *ConversationTabs {
	innerH := height - tabBarLines
	if innerH < 3 {
		innerH = 3
	}
	cmd := NewConversationHistory(width, innerH)
	cmd.chatMode = false
	notifs := NewConversationHistory(width, innerH)
	notifs.chatMode = false
	logs := NewConversationHistory(width, innerH)
	logs.chatMode = false
	return &ConversationTabs{
		cmd:           cmd,
		roomChat:      NewRoomChatPane(width, innerH),
		dmPane:        NewDMPane(width, innerH),
		notifications: notifs,
		invites:       NewInvitesPane(width, innerH),
		logs:          logs,
		width:         width,
		height:        height,
		active:        TabCmd,
	}
}

func (t *ConversationTabs) SetSize(width, height int) {
	t.width = width
	t.height = height
	innerH := height - tabBarLines
	if innerH < 3 {
		innerH = 3
	}
	t.cmd.SetSize(width, innerH)
	t.roomChat.SetSize(width, innerH)
	t.dmPane.SetSize(width, innerH)
	t.notifications.SetSize(width, innerH)
	t.invites.SetSize(width, innerH)
}

// activeComponent returns the component for the current tab.
func (t *ConversationTabs) activeComponent() Component {
	switch t.active {
	case TabChat:
		return t.roomChat
	case TabDM:
		return t.dmPane
	case TabNotifications:
		return t.notifications
	case TabInvites:
		return t.invites
	default:
		return t.cmd
	}
}

func (t *ConversationTabs) SetFocused(focused bool) {
	t.focused = focused
	t.cmd.SetFocused(false)
	t.roomChat.SetFocused(false)
	t.dmPane.SetFocused(false)
	t.notifications.SetFocused(false)
	t.invites.SetFocused(false)
	t.activeComponent().SetFocused(focused)
}

func (t *ConversationTabs) Init() tea.Cmd {
	return tea.Batch(t.cmd.Init(), t.roomChat.Init(), t.dmPane.Init(), t.notifications.Init(), t.invites.Init())
}

// switchTabInternal updates the active tab and focus state without emitting messages.
func (t *ConversationTabs) switchTabInternal(newTab HistoryTab) {
	t.active = newTab
	t.cmd.SetFocused(false)
	t.roomChat.SetFocused(false)
	t.dmPane.SetFocused(false)
	t.notifications.SetFocused(false)
	t.invites.SetFocused(false)
	t.activeComponent().SetFocused(t.focused)
}

// switchTabWithMessages switches the active tab and returns mode-change commands
// so that CommandComponent and HintsComponent stay in sync.
func (t *ConversationTabs) switchTabWithMessages(newTab HistoryTab) tea.Cmd {
	t.switchTabInternal(newTab)
	switch newTab {
	case TabCmd, TabNotifications, TabInvites:
		return tea.Batch(
			func() tea.Msg { return ModeChangedMsg{ChatMode: false} },
			func() tea.Msg { return DMTargetChangedMsg{User: ""} },
		)
	case TabChat:
		return tea.Batch(
			func() tea.Msg { return ModeChangedMsg{ChatMode: true} },
			func() tea.Msg { return DMTargetChangedMsg{User: ""} },
		)
	case TabDM:
		target := t.dmPane.SelectedUser()
		return func() tea.Msg { return DMTargetChangedMsg{User: target} }
	}
	return nil
}

// CanConsumeTab returns true when the active tab should absorb tab presses for
// internal sub-focus cycling rather than letting the outer focus cycle run.
// For TabChat this delegates to RoomChatPane so tab bubbles up once we reach
// the end of the internal cycle (depth-first: last child → next sibling).
// CanFocus reports whether the currently active tab can usefully receive focus.
// CMD, Chat, and Notifications tabs require scrollable overflow; DM and Invites
// are always focusable (DM for user-list navigation, Invites for accepting).
func (t *ConversationTabs) CanFocus() bool {
	switch t.active {
	case TabDM, TabInvites:
		return true
	case TabCmd:
		return t.cmd.CanFocus()
	case TabNotifications:
		return t.notifications.CanFocus()
	case TabChat:
		return true // rooms panel is always worth navigating
	default:
		return true
	}
}

func (t *ConversationTabs) CanConsumeTab() bool {
	if t.active == TabDM && t.focused && t.dmPane.activeHistory() != nil {
		// Only consume when history isn't yet focused; once it is, bubble out to the outer cycle.
		return !t.dmPane.historyFocused
	}
	if t.active == TabChat && t.focused {
		return t.roomChat.CanConsumeTab()
	}
	return false
}

func (t *ConversationTabs) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case AppendHistoryMsg:
		switch msg.Tab {
		case TabChat:
			return t.roomChat.chat.Update(msg)
		case TabNotifications:
			return t.notifications.Update(msg)
		default: // TabCmd (and any unrecognised value)
			return t.cmd.Update(msg)
		}
	case AppendDMHistoryMsg:
		return t.dmPane.Update(msg)
	case ClearHistoryMsg:
		return t.cmd.Update(msg)
	case ModeChangedMsg:
		// /chat and /cmd commands drive tab switching; DM, Notifications, and Invites tabs are NOT affected.
		if msg.ChatMode {
			t.switchTabInternal(TabChat)
		} else if t.active != TabDM && t.active != TabNotifications && t.active != TabInvites {
			t.switchTabInternal(TabCmd)
		}
		return nil
	case SwitchTabMsg:
		return t.switchTabWithMessages(msg.Tab)
	case StartDMMsg:
		t.dmPane.EnsureUser(msg.User)
		t.dmPane.SelectUser(msg.User)
		t.switchTabInternal(TabDM)
		return func() tea.Msg { return DMTargetChangedMsg{User: msg.User} }
	case JumpScrollMsg:
		return t.activeComponent().Update(msg)
	case tea.KeyPressMsg:
		switch msg.String() {
		case "left":
			n := (int(t.active) - 1 + len(tabDefs)) % len(tabDefs)
			return t.switchTabWithMessages(HistoryTab(n))
		case "right":
			n := (int(t.active) + 1) % len(tabDefs)
			return t.switchTabWithMessages(HistoryTab(n))
		case "tab":
			// Route tab to the active pane for internal sub-focus toggling.
			if t.active == TabDM {
				return t.dmPane.Update(msg)
			}
			if t.active == TabChat {
				return t.roomChat.Update(msg)
			}
			return t.activeComponent().Update(msg)
		default:
			return t.activeComponent().Update(msg)
		}
	default:
		// Always route background events to roomChat and invites so their polls
		// stay alive regardless of which tab is visible.
		var cmds []tea.Cmd
		if cmd := t.roomChat.Update(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if cmd := t.invites.Update(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if t.active != TabChat && t.active != TabInvites {
			if cmd := t.activeComponent().Update(msg); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		return tea.Batch(cmds...)
	}
}

func (t *ConversationTabs) renderTabBar() string {
	highlight := lipgloss.Color("213")
	dim := lipgloss.Color("241")

	inactiveStyle := lipgloss.NewStyle().
		Border(tabsInactiveTabBorder, true).
		BorderForeground(dim).
		Padding(0, 1)
	activeStyle := inactiveStyle.
		Border(tabsActiveTabBorder, true).
		BorderForeground(highlight)
	gapStyle := inactiveStyle.
		BorderTop(false).
		BorderLeft(false).
		BorderRight(false).
		BorderForeground(dim)

	renderedTabs := make([]string, len(tabDefs))
	for i, name := range tabDefs {
		if HistoryTab(i) == t.active {
			renderedTabs[i] = activeStyle.Render(name)
		} else {
			renderedTabs[i] = inactiveStyle.Render(name)
		}
	}

	row := lipgloss.JoinHorizontal(lipgloss.Top, renderedTabs...)

	// Total visual width of the history box = inner content width + border(2) + padding(2).
	totalWidth := t.width + 4
	gapContentWidth := totalWidth - lipgloss.Width(row) - 2 // -2 for gap's own padding
	if gapContentWidth < 0 {
		gapContentWidth = 0
	}
	gap := gapStyle.Render(strings.Repeat(" ", gapContentWidth))
	return lipgloss.JoinHorizontal(lipgloss.Bottom, row, gap)
}

func (t *ConversationTabs) Render() string {
	tabBar := t.renderTabBar()
	content := t.activeComponent().Render()
	return lipgloss.JoinVertical(lipgloss.Left, tabBar, content)
}
