// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package components

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	apitypes "github.com/srschreiber/nito-client/shared/api_types"
	"github.com/srschreiber/nito-client/shellapp/connection"
	"github.com/srschreiber/nito-client/shellapp/styles"
)

const invitesPollInterval = 30 * time.Second

type invitesPollMsg struct{}
type invitesAppendMsg struct{}

func NewInvitesAppendMsg() tea.Msg { return invitesAppendMsg{} }

type invitesFetchedMsg struct {
	invites []apitypes.PendingInvite
	err     error
}

type invitesAcceptResultMsg struct {
	roomName string
	err      error
}

// InvitesPane shows pending room invites and lets the user accept them interactively.
type InvitesPane struct {
	invites []apitypes.PendingInvite
	cursor  int
	focused bool
	width   int
	height  int
	status  string // transient feedback line
}

func NewInvitesPane(width, height int) *InvitesPane {
	return &InvitesPane{width: width, height: height}
}

func (p *InvitesPane) SetSize(width, height int) {
	p.width = width
	p.height = height
}

func (p *InvitesPane) SetFocused(focused bool) { p.focused = focused }

func (p *InvitesPane) Init() tea.Cmd {
	return tea.Batch(p.fetch(), p.schedulePoll())
}

func (p *InvitesPane) schedulePoll() tea.Cmd {
	return tea.Tick(invitesPollInterval, func(time.Time) tea.Msg { return invitesPollMsg{} })
}

func (p *InvitesPane) fetch() tea.Cmd {
	return func() tea.Msg {
		invites, err := connection.ListPendingInvites()
		return invitesFetchedMsg{invites: invites, err: err}
	}
}

func (p *InvitesPane) accept(roomID, roomName string) tea.Cmd {
	return func() tea.Msg {
		err := connection.AcceptInvite(roomID)
		return invitesAcceptResultMsg{roomName: roomName, err: err}
	}
}

func (p *InvitesPane) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case invitesPollMsg:
		return tea.Batch(p.fetch(), p.schedulePoll())
	case invitesFetchedMsg:
		if msg.err == nil {
			p.invites = msg.invites
			if p.cursor >= len(p.invites) {
				p.cursor = max(0, len(p.invites)-1)
			}
		}
	case invitesAppendMsg:
		// for reactively responding to notification
		return p.fetch()
	case invitesAcceptResultMsg:
		if msg.err != nil {
			p.status = "error: " + msg.err.Error()
			return p.fetch()
		}
		p.status = fmt.Sprintf("joined %q", msg.roomName)
		// Refetch invites and trigger a rooms list refresh.
		return tea.Batch(p.fetch(), func() tea.Msg { return roomsPollMsg{} })
	case tea.KeyPressMsg:
		if !p.focused {
			return nil
		}
		switch msg.String() {
		case "up", "ctrl+p":
			if p.cursor > 0 {
				p.cursor--
			}
		case "down", "ctrl+n":
			if p.cursor < len(p.invites)-1 {
				p.cursor++
			}
		case "enter", " ":
			if p.cursor >= 0 && p.cursor < len(p.invites) {
				inv := p.invites[p.cursor]
				p.status = fmt.Sprintf("accepting %q...", inv.RoomName)
				return p.accept(inv.RoomID, inv.RoomName)
			}
		}
	}
	return nil
}

func (p *InvitesPane) Render() string {
	title := styles.InvitesBadge.Render("INVITES")

	var lines []string

	if len(p.invites) == 0 {
		lines = append(lines, styles.Grey.Render("  no pending invites"))
	} else {
		acceptBtn := styles.InviteAcceptBtnStyle.Render("Accept")

		for i, inv := range p.invites {
			cursor := "  "
			nameStyle := styles.Grey
			selected := i == p.cursor && p.focused
			if selected {
				cursor = styles.CursorStyle.Render("› ")
				nameStyle = styles.InviteSelectedItemStyle
			}
			name := nameStyle.Render(inv.RoomName)
			// pad name to fill width, then place Accept on right
			gap := p.width - lipgloss.Width(cursor) - lipgloss.Width(inv.RoomName) - lipgloss.Width(acceptBtn) - 2
			if gap < 1 {
				gap = 1
			}
			line := cursor + name + strings.Repeat(" ", gap) + acceptBtn
			if selected {
				line = styles.SelectionRowStyle.Render(line)
			}
			lines = append(lines, line)
		}
	}

	// Clip to available content height (reserve title + border/padding + status row).
	maxLines := p.height - 5
	if maxLines < 1 {
		maxLines = 1
	}
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}

	body := title + "\n\n" + strings.Join(lines, "\n")
	if p.status != "" {
		body += "\n\n" + styles.Grey.Render(p.status)
	}

	borderColor := styles.PanelBorderColor
	if p.focused {
		borderColor = styles.PanelFocusedBorderColor
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Background(styles.ComponentBg).
		Padding(0, 1).
		Width(p.width).
		Height(p.height).
		Render(body)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
