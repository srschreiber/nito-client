// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package components

import (
	"time"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	apitypes "github.com/srschreiber/nito-client/shared/api_types"
	"github.com/srschreiber/nito-client/shellapp/connection"
	"github.com/srschreiber/nito-client/shellapp/styles"
	"github.com/srschreiber/nito-client/shellapp/types"
)

type membersPollMsg struct{}

type RoomMembersComponent struct {
	members []apitypes.RoomMemberEntry
	roomID  *string
	width   int
	height  int
}

func NewRoomMembersComponent(width, height int) *RoomMembersComponent {
	return &RoomMembersComponent{width: width, height: height}
}

func (m *RoomMembersComponent) SetSize(width, height int) {
	m.width = width
	m.height = height
}

func (m *RoomMembersComponent) SetFocused(_ bool) {}

func (m *RoomMembersComponent) Init() tea.Cmd {
	return m.schedulePoll()
}

func (m *RoomMembersComponent) schedulePoll() tea.Cmd {
	return tea.Tick(15*time.Second, func(time.Time) tea.Msg {
		return membersPollMsg{}
	})
}

func (m *RoomMembersComponent) fetch() tea.Cmd {
	roomID := m.roomID
	if roomID == nil {
		return nil
	}
	id := *roomID
	return func() tea.Msg {
		members, err := connection.ListRoomMembers(id)
		if err != nil {
			return nil
		}
		return types.RoomMembersUpdatedMsg{Members: members}
	}
}

func (m *RoomMembersComponent) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case membersPollMsg:
		return tea.Batch(m.fetch(), m.schedulePoll())
	case types.RoomSelectedMsg:
		m.roomID = &msg.RoomID
		m.members = nil
		return m.fetch()
	case types.RoomMembersFetchMsg:
		m.roomID = &msg.RoomID
		return m.fetch()
	case types.MembersUpdatedMsg:
		return m.fetch()
	case types.RoomMembersUpdatedMsg:
		m.members = msg.Members
	}
	return nil
}

func (m *RoomMembersComponent) Render() string {
	if m.width <= 0 {
		return ""
	}

	title := styles.MembersBadge.Render("MEMBERS")
	body := title + "\n"

	if len(m.members) == 0 {
		body += styles.Grey.Render("  no members")
	} else {
		for _, member := range m.members {
			dot := styles.MemberOfflineStyle.Render("●")
			if member.Online {
				dot = styles.MemberOnlineStyle.Render("●")
			}
			body += styles.ItemStyle.Render(dot+" "+member.Username) + "\n"
		}
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.PanelBorderColor).
		Background(styles.ComponentBg).
		Padding(0, 1).
		Width(m.width).
		Height(m.height).
		Render(body)
}
