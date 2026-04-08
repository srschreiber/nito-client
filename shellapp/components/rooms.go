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
	"github.com/srschreiber/nito-client/shared/utils"
	"github.com/srschreiber/nito-client/shellapp/connection"
	"github.com/srschreiber/nito-client/shellapp/styles"
	"github.com/srschreiber/nito-client/shellapp/types"
)

type roomsPollMsg struct{}

type RoomsComponent struct {
	rooms    []apitypes.RoomEntry
	selected *string
	cursor   int
	focused  bool
	width    int
	height   int
}

func NewRoomsComponent(width, height int) *RoomsComponent {
	return &RoomsComponent{width: width, height: height}
}

func (r *RoomsComponent) SetSize(width, height int) {
	r.width = width
	r.height = height
}

func (r *RoomsComponent) SetFocused(focused bool) {
	r.focused = focused
}

func (r *RoomsComponent) Init() tea.Cmd {
	return tea.Batch(r.fetch(), r.schedulePoll())
}

func (r *RoomsComponent) schedulePoll() tea.Cmd {
	return tea.Tick(30*time.Second, func(time.Time) tea.Msg {
		return roomsPollMsg{}
	})
}

func (r *RoomsComponent) fetch() tea.Cmd {
	return func() tea.Msg {
		rooms, err := connection.ListRooms()
		if err != nil {
			return nil
		}
		return types.RoomsUpdatedMsg{Rooms: rooms}
	}
}

func (r *RoomsComponent) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case roomsPollMsg:
		return tea.Batch(r.fetch(), r.schedulePoll())
	case types.RoomsFetchMsg:
		return r.fetch()
	case types.ConnectionStatusMsg:
		if msg.Connected {
			return r.fetch()
		}
	case types.RoomsUpdatedMsg:
		r.rooms = msg.Rooms
		if r.cursor >= len(r.rooms) {
			r.cursor = 0
		}
	case types.RoomSelectedMsg:
		r.selected = &msg.RoomID
	case tea.KeyPressMsg:
		if !r.focused {
			return nil
		}
		return r.updateList(msg)
	}
	return nil
}

func (r *RoomsComponent) updateList(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "up", "ctrl+p":
		if r.cursor > 0 {
			r.cursor--
		}
	case "down", "ctrl+n":
		if r.cursor < len(r.rooms)-1 {
			r.cursor++
		}
	case "enter":
		if len(r.rooms) > 0 {
			room := r.rooms[r.cursor]
			selected := room.ID
			r.selected = &selected
			if err := connection.SetSessionRoom(room.ID); err != nil {
				return func() tea.Msg {
					return types.ErrorMsg{Message: fmt.Sprintf("Failed to select room: %v", err)}
				}
			}
			return func() tea.Msg { return types.RoomSelectedMsg{RoomID: room.ID} }
		}
	}
	return nil
}

// listHeight returns the number of content lines available for the room list.
func (r *RoomsComponent) listHeight() int {
	h := r.height - 3 // outer border(2) + title(1)
	if h < 1 {
		h = 1
	}
	return h
}

func (r *RoomsComponent) Render() string {
	title := styles.RoomsBadge.Render("ROOMS")

	listH := r.listHeight()
	var listLines []string
	if len(r.rooms) == 0 {
		listLines = append(listLines, styles.DimText.Render("  no rooms"))
	} else {
		for i, room := range r.rooms {
			name := room.Name
			if room.IsOwner {
				name += " " + styles.DimText.Render("(owner)")
			}
			cursor := "  "
			if room.ID == utils.DerefOrZero(r.selected) {
				name = fmt.Sprintf("%s %s", name, styles.SelectedStyle.Render("◆"))
			}
			if i == r.cursor && r.focused {
				cursor = styles.CursorStyle.Render("▶ ")
			}
			listLines = append(listLines, styles.ItemStyle.Render(cursor+name))
		}
	}
	if len(listLines) > listH {
		listLines = listLines[len(listLines)-listH:]
	}
	for len(listLines) < listH {
		listLines = append(listLines, "")
	}

	body := title + "\n" + strings.Join(listLines, "\n")

	borderColor := styles.PanelBorderColor
	bg := styles.ComponentBg
	if r.focused {
		borderColor = styles.PanelFocusedBorderColor
		bg = styles.ComponentFocusedBg
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Background(bg).
		Padding(0, 1).
		Width(r.width).
		Height(r.height).
		Render(body)
}
