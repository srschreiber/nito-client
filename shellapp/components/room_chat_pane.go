// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package components

import (
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/srschreiber/nito-client/shellapp/types"
)

// roomsChatPanelOuterW is the outer visual width of the rooms+members side panel
// (inner = roomsChatPanelOuterW - border(2) - padding(2)).
const roomsChatPanelOuterW = 32

// RoomChatPane combines the Room Chat history with the Rooms and Members panels
// in a side-by-side layout. Tab cycles: rooms list → Create btn → Invite btn →
// chat history → rooms list.
type RoomChatPane struct {
	chat          *ConversationHistory
	rooms         *RoomsComponent
	members       *RoomMembersComponent
	width         int
	height        int
	focused       bool
	histFocused   bool // when true, chat history has sub-focus; otherwise rooms has it
	showMembers   bool
	cycleComplete bool // set when internal tab cycle finishes without being able to focus history
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
		rooms:   NewRoomsComponent(panelInnerW, height),
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

	if p.showMembers {
		roomsH := int(float64(p.height) * 0.6)
		if roomsH < 3 {
			roomsH = 3
		}
		membersH := p.height - roomsH
		if membersH < 3 {
			membersH = 3
		}
		p.rooms.SetSize(panelInnerW, roomsH)
		p.members.SetSize(panelInnerW, membersH)
	} else {
		p.rooms.SetSize(panelInnerW, p.height)
		p.members.SetSize(panelInnerW, 0)
	}
}

func (p *RoomChatPane) SetFocused(focused bool) {
	p.focused = focused
	p.cycleComplete = false
	if !focused {
		p.histFocused = false
		p.rooms.SetFocused(false)
		p.chat.SetFocused(false)
	} else {
		// Default: rooms focused when entering the pane.
		p.histFocused = false
		p.rooms.SetFocused(true)
		p.rooms.area = roomsAreaList
		p.chat.SetFocused(false)
	}
}

// CanConsumeTab returns true while there are still internal stops to cycle through
// (rooms list → Create → Invite → Voice → TestAudio). Returns false when chat history
// is the active sub-focus, signalling the outer handler to advance to the next top-level component.
func (p *RoomChatPane) CanConsumeTab() bool {
	return !p.histFocused && !p.cycleComplete
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
		if p.histFocused {
			return p.chat.Update(msg)
		}
		return p.rooms.Update(msg)

	default:
		// Always forward background events (polling, room/member updates) to rooms+members.
		var cmds []tea.Cmd
		if cmd := p.rooms.Update(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if cmd := p.members.Update(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
		return tea.Batch(cmds...)
	}
}

// handleTab cycles sub-focus: rooms list → Create → Invite → Voice → TestAudio → chat history → rooms list.
func (p *RoomChatPane) handleTab() tea.Cmd {
	if p.histFocused {
		// history → back to rooms list
		p.histFocused = false
		p.chat.SetFocused(false)
		p.rooms.SetFocused(true)
		p.rooms.area = roomsAreaList
		return nil
	}
	// Advance through rooms areas: list → Create → Invite → Voice → TestAudio → history
	switch p.rooms.area {
	case roomsAreaList:
		p.rooms.area = roomsAreaCreateBtn
	case roomsAreaCreateBtn:
		p.rooms.area = roomsAreaInviteBtn
	case roomsAreaInviteBtn:
		p.rooms.area = roomsAreaVoiceBtn
	case roomsAreaVoiceBtn:
		p.rooms.area = roomsAreaTestAudioBtn
	default:
		// testAudioBtn (or form) → switch to history only if it has scrollable content
		if p.chat.CanFocus() {
			p.rooms.area = roomsAreaList
			p.rooms.SetFocused(false)
			p.histFocused = true
			p.chat.SetFocused(true)
		} else {
			p.cycleComplete = true
		}
	}
	return nil
}

func (p *RoomChatPane) Render() string {
	chatStr := p.chat.Render()

	var sidePanel string
	if p.showMembers {
		sidePanel = lipgloss.JoinVertical(lipgloss.Left, p.rooms.Render(), p.members.Render())
	} else {
		sidePanel = p.rooms.Render()
	}

	// Side panel on the left, chat history on the right (consistent with DMs tab layout).
	return lipgloss.JoinHorizontal(lipgloss.Top, sidePanel, chatStr)
}
