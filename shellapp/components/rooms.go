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
	"github.com/srschreiber/nito-client/shellapp/commands"
	"github.com/srschreiber/nito-client/shellapp/connection"
	"github.com/srschreiber/nito-client/shellapp/styles"
	"github.com/srschreiber/nito-client/shellapp/types"
)

type roomsPollMsg struct{}

type roomsOpResultMsg struct {
	text string
	err  error
	// refresh signals rooms list should be re-fetched after this op.
	refresh bool
}

type roomsVoiceResultMsg struct {
	joined bool
	err    error
}

// roomsArea tracks which part of the component has focus.
type roomsArea int

const (
	roomsAreaList      roomsArea = iota // navigating room list
	roomsAreaCreateBtn                  // Create button highlighted
	roomsAreaInviteBtn                  // Invite button highlighted
	roomsAreaForm                       // text input for create/invite
	roomsAreaVoiceBtn                   // Voice Chat button highlighted
)

type roomsFormMode int

const (
	roomsFormCreate roomsFormMode = iota
	roomsFormInvite
)

type RoomsComponent struct {
	rooms    []apitypes.RoomEntry
	selected *string
	cursor   int
	focused  bool
	width    int
	height   int

	area        roomsArea
	formMode    roomsFormMode
	formVal     string
	formCur     int
	formErr     string
	voiceActive bool // true when currently in voice chat
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
	if !focused {
		// reset to list area when focus leaves
		r.area = roomsAreaList
		r.formVal = ""
		r.formCur = 0
		r.formErr = ""
	}
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

func (r *RoomsComponent) submitForm() tea.Cmd {
	val := strings.TrimSpace(r.formVal)
	if val == "" {
		if r.formMode == roomsFormCreate {
			r.formErr = "room name required"
		} else {
			r.formErr = "username required"
		}
		return nil
	}
	r.formErr = ""
	mode := r.formMode
	return func() tea.Msg {
		var text string
		var err error
		var refresh bool
		if mode == roomsFormCreate {
			text, err = commands.CreateRoomDirect(val)
			refresh = err == nil
		} else {
			text, err = commands.InviteUserDirect(val)
		}
		return roomsOpResultMsg{text: text, err: err, refresh: refresh}
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
	case roomsOpResultMsg:
		r.formVal = ""
		r.formCur = 0
		r.formErr = ""
		r.area = roomsAreaList
		text := msg.text
		if msg.err != nil {
			text = "error: " + msg.err.Error()
		}
		if msg.refresh {
			return tea.Batch(r.fetch(), func() tea.Msg { return NewChatResponseAppendMsg(text) })
		}
		return func() tea.Msg { return NewChatResponseAppendMsg(text) }
	case roomsVoiceResultMsg:
		if msg.err != nil {
			errText := "voice: " + msg.err.Error()
			return func() tea.Msg { return NewChatResponseAppendMsg(errText) }
		}
		r.voiceActive = msg.joined
		action := "joined voice chat"
		if !msg.joined {
			action = "left voice chat"
		}
		return func() tea.Msg { return NewChatResponseAppendMsg(action) }

	case tea.KeyPressMsg:
		if !r.focused {
			return nil
		}
		switch r.area {
		case roomsAreaList:
			return r.updateList(msg)
		case roomsAreaCreateBtn, roomsAreaInviteBtn, roomsAreaVoiceBtn:
			return r.updateButtons(msg)
		case roomsAreaForm:
			return r.updateForm(msg)
		}
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
	case "tab":
		r.area = roomsAreaCreateBtn
		r.formErr = ""
	}
	return nil
}

func (r *RoomsComponent) updateButtons(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		r.area = roomsAreaList
	case "enter", " ":
		switch r.area {
		case roomsAreaVoiceBtn:
			joining := !r.voiceActive
			return func() tea.Msg {
				if joining {
					err := commands.VoiceJoinDirect()
					return roomsVoiceResultMsg{joined: true, err: err}
				}
				err := commands.VoiceLeaveDirect()
				return roomsVoiceResultMsg{joined: false, err: err}
			}
		case roomsAreaCreateBtn:
			r.formMode = roomsFormCreate
			r.formVal = ""
			r.formCur = 0
			r.formErr = ""
			r.area = roomsAreaForm
		case roomsAreaInviteBtn:
			r.formMode = roomsFormInvite
			r.formVal = ""
			r.formCur = 0
			r.formErr = ""
			r.area = roomsAreaForm
		}
	}
	return nil
}

func (r *RoomsComponent) updateForm(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		r.area = roomsAreaCreateBtn
		if r.formMode == roomsFormInvite {
			r.area = roomsAreaInviteBtn
		}
		r.formErr = ""
		return nil
	case "enter":
		return r.submitForm()
	case "backspace":
		runes := []rune(r.formVal)
		if r.formCur > 0 {
			r.formVal = string(append(runes[:r.formCur-1], runes[r.formCur:]...))
			r.formCur--
		}
	case "left", "ctrl+b":
		if r.formCur > 0 {
			r.formCur--
		}
	case "right", "ctrl+f":
		if r.formCur < len([]rune(r.formVal)) {
			r.formCur++
		}
	case "ctrl+a":
		r.formCur = 0
	case "ctrl+e":
		r.formCur = len([]rune(r.formVal))
	default:
		text := msg.Key().Text
		if text != "" {
			runes := []rune(r.formVal)
			r.formVal = string(runes[:r.formCur]) + text + string(runes[r.formCur:])
			r.formCur += len([]rune(text))
			r.formErr = ""
		}
	}
	return nil
}

// listHeight returns the number of lines available for the room list.
// Reserved: title(1) + two lines used by buttons or the inline form(2) = 3.
func (r *RoomsComponent) listHeight() int {
	reserved := 10
	h := r.height - reserved
	if h < 1 {
		h = 1
	}
	return h
}

func (r *RoomsComponent) Render() string {
	title := styles.RoomsBadge.Render("ROOMS")

	// Room list.
	listH := r.listHeight()
	var listLines []string
	if len(r.rooms) == 0 {
		listLines = append(listLines, styles.Grey.Render("  no rooms"))
	} else {
		for i, room := range r.rooms {
			name := room.Name
			if room.IsOwner {
				name += " " + styles.Grey.Render("(owner)")
			}
			cursor := "  "
			if room.ID == utils.DerefOrZero(r.selected) {
				name = fmt.Sprintf("%s %s", name, styles.SelectedStyle.Render("✓"))
			}
			if i == r.cursor && (r.area == roomsAreaList || !r.focused) {
				cursor = styles.CursorStyle.Render("› ")
			}
			listLines = append(listLines, styles.ItemStyle.Render(cursor+name))
		}
	}
	// Clip to available height.
	if len(listLines) > listH {
		listLines = listLines[len(listLines)-listH:]
	}
	// Pad to keep layout stable.
	for len(listLines) < listH {
		listLines = append(listLines, "")
	}

	// Two lines below the list: either buttons or the active form.
	var line1, line2 string
	if r.area == roomsAreaForm {
		label := "Room name: "
		if r.formMode == roomsFormInvite {
			label = "Username:  "
		}
		runes := []rune(r.formVal)
		var fieldText string
		if r.formCur >= len(runes) {
			fieldText = r.formVal + lipgloss.NewStyle().
				Background(lipgloss.Color("213")).Render(" ")
		} else {
			fieldText = string(runes[:r.formCur]) +
				lipgloss.NewStyle().Background(lipgloss.Color("213")).Render(string(runes[r.formCur])) +
				string(runes[r.formCur+1:])
		}
		line1 = styles.Grey.Render(label) + fieldText
		if r.formErr != "" {
			line2 = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render(r.formErr)
		} else {
			line2 = styles.Grey.Render("enter  submit  •  esc  cancel")
		}
	} else {
		line1 = renderRoomBtn("+ Create", r.focused && r.area == roomsAreaCreateBtn)
		line2 = renderRoomBtn("+ Invite", r.focused && r.area == roomsAreaInviteBtn)
	}

	voiceBtn := renderVoiceBtn(r.voiceActive, r.focused && r.area == roomsAreaVoiceBtn)

	body := title + "\n" +
		strings.Join(listLines, "\n") + "\n" +
		line1 + "\n" +
		"\n" +
		line2 + "\n" +
		"\n" +
		voiceBtn

	borderColor := lipgloss.Color("#4a4a7a")
	if r.focused {
		borderColor = lipgloss.Color("#a855f7")
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Background(styles.ComponentBg).
		Padding(0, 1).
		Width(r.width).
		Height(r.height).
		Render(body)
}

func renderRoomBtn(label string, active bool) string {
	s := lipgloss.NewStyle().Padding(0, 1).MarginRight(1)
	if active {
		s = s.Background(lipgloss.Color("213")).Foreground(lipgloss.Color("0")).Bold(true)
	} else {
		s = s.Background(lipgloss.Color("238")).Foreground(lipgloss.Color("250"))
	}
	return s.Render(label)
}

func renderVoiceBtn(inVoice, active bool) string {
	var label string
	s := lipgloss.NewStyle().Padding(0, 1).MarginRight(1)
	if inVoice {
		label = "* Leave Voice"
		if active {
			s = s.Background(lipgloss.Color("#7f1d1d")).Foreground(lipgloss.Color("#fca5a5")).Bold(true)
		} else {
			s = s.Background(lipgloss.Color("#450a0a")).Foreground(lipgloss.Color("#f87171"))
		}
	} else {
		label = "> Join Voice"
		if active {
			s = s.Background(lipgloss.Color("213")).Foreground(lipgloss.Color("0")).Bold(true)
		} else {
			s = s.Background(lipgloss.Color("238")).Foreground(lipgloss.Color("250"))
		}
	}
	return s.Render(label)
}
