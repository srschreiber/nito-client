// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package components

import (
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/srschreiber/nito-client/shellapp/types"
)

// roomsChatPanelOuterW is the outer visual width of the rooms+members side panel
// (inner = roomsChatPanelOuterW - border(2) - padding(2)).
const roomsChatPanelOuterW = 32

// roomActionsOuterH is the fixed outer height of the actions panel (border(2) + title(1) + blank(1) + 4 items).
const roomActionsOuterH = 8

// roomChatSubFocus tracks which sub-component holds keyboard focus inside RoomChatPane.
type roomChatSubFocus int

const (
	subFocusRooms   roomChatSubFocus = iota
	subFocusActions                  // actions panel (Create/Invite/Voice/Audio)
	subFocusHistory                  // chat history
)

// RoomChatPane combines the Room Chat history with the Rooms, Actions, and Members panels
// in a side-by-side layout. Tab cycles: rooms list → actions panel → chat history → [exit to parent].
type RoomChatPane struct {
	chat        *ConversationHistory
	rooms       *RoomsComponent
	actions     *RoomActionsComponent
	members     *RoomMembersComponent
	width       int
	height      int
	focused     bool
	subFocus    roomChatSubFocus
	showMembers bool
}

func NewRoomChatPane(width, height int) *RoomChatPane {
	panelInnerW := roomsChatPanelOuterW - 4
	if panelInnerW < 5 {
		panelInnerW = 5
	}
	chatW := width - roomsChatPanelOuterW
	if chatW < 10 {
		chatW = 10
	}
	p := &RoomChatPane{
		chat:    NewConversationHistory(chatW, height),
		rooms:   NewRoomsComponent(panelInnerW, height-roomActionsOuterH),
		actions: NewRoomActionsComponent(panelInnerW, roomActionsOuterH),
		members: NewRoomMembersComponent(panelInnerW, 0),
		width:   width,
		height:  height,
	}
	p.chat.chatMode = true
	return p
}

func (p *RoomChatPane) SetSize(w, h int) {
	p.width = w
	p.height = h
	p.resizeChildren()
}

func (p *RoomChatPane) resizeChildren() {
	panelInnerW := roomsChatPanelOuterW - 4
	if panelInnerW < 5 {
		panelInnerW = 5
	}
	chatW := p.width - roomsChatPanelOuterW
	if chatW < 10 {
		chatW = 10
	}
	p.chat.SetSize(chatW, p.height)
	p.actions.SetSize(panelInnerW, roomActionsOuterH)

	if p.showMembers {
		remaining := p.height - roomActionsOuterH
		roomsH := int(float64(remaining) * 0.6)
		if roomsH < 3 {
			roomsH = 3
		}
		membersH := remaining - roomsH
		if membersH < 3 {
			membersH = 3
		}
		p.rooms.SetSize(panelInnerW, roomsH)
		p.members.SetSize(panelInnerW, membersH)
	} else {
		p.rooms.SetSize(panelInnerW, p.height-roomActionsOuterH)
		p.members.SetSize(panelInnerW, 0)
	}
}

func (p *RoomChatPane) SetFocused(focused bool) {
	p.focused = focused
	if !focused {
		p.subFocus = subFocusRooms
		p.rooms.SetFocused(false)
		p.actions.SetFocused(false)
		p.chat.SetFocused(false)
	} else {
		p.subFocus = subFocusRooms
		p.rooms.SetFocused(true)
		p.actions.SetFocused(false)
		p.chat.SetFocused(false)
	}
}

// CanConsumeTab returns true while there are still internal tab stops to cycle through:
// - rooms (always advances to actions)
// - actions (advances to history only if chat has scrollable content)
// Returns false otherwise, signalling the outer handler to advance to the next top-level component.
func (p *RoomChatPane) CanConsumeTab() bool {
	switch p.subFocus {
	case subFocusRooms:
		return true
	case subFocusActions:
		return p.chat.CanFocus()
	default: // subFocusHistory
		return false
	}
}

// CanFocus reports whether the chat history pane has scrollable content.
func (p *RoomChatPane) CanFocus() bool {
	return p.chat.CanFocus()
}

func (p *RoomChatPane) Init() tea.Cmd {
	return tea.Batch(p.rooms.Init(), p.members.Init())
}

func (p *RoomChatPane) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case types.RoomSelectedMsg:
		p.showMembers = true
		p.resizeChildren()
		p.chat.Update(ClearHistoryMsg{})
		var cmds []tea.Cmd
		if cmd := p.rooms.Update(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if cmd := p.members.Update(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
		return tea.Batch(cmds...)

	case JumpScrollMsg:
		return p.chat.Update(msg)

	case tea.KeyPressMsg:
		if !p.focused {
			return nil
		}
		if msg.String() == "tab" {
			return p.handleTab()
		}
		switch p.subFocus {
		case subFocusHistory:
			return p.chat.Update(msg)
		case subFocusActions:
			return p.actions.Update(msg)
		default:
			return p.rooms.Update(msg)
		}

	default:
		// Always forward background events (polling, room/member updates) to all sub-components.
		var cmds []tea.Cmd
		if cmd := p.rooms.Update(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if cmd := p.actions.Update(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if cmd := p.members.Update(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
		return tea.Batch(cmds...)
	}
}

// handleTab cycles sub-focus: rooms list → actions panel → chat history → rooms list.
func (p *RoomChatPane) handleTab() tea.Cmd {
	switch p.subFocus {
	case subFocusHistory:
		// history → back to rooms list
		p.chat.SetFocused(false)
		p.rooms.SetFocused(true)
		p.subFocus = subFocusRooms
	case subFocusRooms:
		// rooms list → actions panel
		p.rooms.SetFocused(false)
		p.actions.SetFocused(true)
		p.subFocus = subFocusActions
	case subFocusActions:
		// Only reached when chat.CanFocus() is true (CanConsumeTab returns false otherwise).
		p.actions.SetFocused(false)
		p.chat.SetFocused(true)
		p.subFocus = subFocusHistory
	}
	return nil
}

func (p *RoomChatPane) Render() string {
	chatStr := p.chat.Render()

	var sidePanel string
	if p.showMembers {
		sidePanel = lipgloss.JoinVertical(lipgloss.Left,
			p.rooms.Render(),
			p.actions.Render(),
			p.members.Render(),
		)
	} else {
		sidePanel = lipgloss.JoinVertical(lipgloss.Left,
			p.rooms.Render(),
			p.actions.Render(),
		)
	}

	// Side panel on the left, chat history on the right.
	return lipgloss.JoinHorizontal(lipgloss.Top, sidePanel, chatStr)
}
