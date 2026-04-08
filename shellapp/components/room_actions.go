// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package components

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/srschreiber/nito-client/shellapp/commands"
	"github.com/srschreiber/nito-client/shellapp/styles"
	"github.com/srschreiber/nito-client/shellapp/types"
)

// Room action indices.
const (
	roomActionCreate = 0
	roomActionInvite = 1
	roomActionVoice  = 2
	roomActionAudio  = 3
	roomActionCount  = 4
)

type roomActionsFormMode int

const (
	actionsFormCreate roomActionsFormMode = iota
	actionsFormInvite
)

// roomsOpResultMsg is returned after a create or invite operation completes.
type roomsOpResultMsg struct {
	text    string
	err     error
	refresh bool // true when rooms list should be re-fetched (successful create)
}

// roomsVoiceResultMsg is returned after a voice join/leave attempt.
type roomsVoiceResultMsg struct {
	joined bool
	err    error
}

// roomsTestAudioResultMsg is returned after a test-audio start/stop attempt.
type roomsTestAudioResultMsg struct {
	active bool
	err    error
}

// RoomActionsComponent renders a small panel with Create / Invite / Join Voice /
// Test Audio actions. Items are navigated with up/down or ctrl+p/ctrl+n; Enter/Space activates.
type RoomActionsComponent struct {
	focused         bool
	cursor          int
	voiceActive     bool
	testAudioActive bool
	width           int
	height          int

	formActive bool
	formMode   roomActionsFormMode
	formVal    string
	formCur    int
	formErr    string
}

func NewRoomActionsComponent(width, height int) *RoomActionsComponent {
	return &RoomActionsComponent{width: width, height: height}
}

func (a *RoomActionsComponent) SetSize(width, height int) {
	a.width = width
	a.height = height
}

func (a *RoomActionsComponent) SetFocused(focused bool) {
	a.focused = focused
	if !focused {
		a.formActive = false
		a.formVal = ""
		a.formCur = 0
		a.formErr = ""
	}
}

func (a *RoomActionsComponent) Init() tea.Cmd { return nil }

func (a *RoomActionsComponent) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case roomsOpResultMsg:
		a.formActive = false
		a.formVal = ""
		a.formCur = 0
		a.formErr = ""
		if msg.err != nil {
			errText := msg.err.Error()
			return func() tea.Msg { return ShowToastMsg{Text: errText} }
		}
		if msg.refresh {
			return tea.Batch(
				func() tea.Msg { return types.RoomsFetchMsg{} },
				func() tea.Msg { return NewChatResponseAppendMsg(msg.text) },
			)
		}
		return func() tea.Msg { return NewChatResponseAppendMsg(msg.text) }

	case roomsVoiceResultMsg:
		if msg.err != nil {
			errText := msg.err.Error()
			return func() tea.Msg { return ShowToastMsg{Text: "voice: " + errText} }
		}
		a.voiceActive = msg.joined
		action := "joined voice chat"
		if !msg.joined {
			action = "left voice chat"
		}
		return func() tea.Msg { return NewChatResponseAppendMsg(action) }

	case roomsTestAudioResultMsg:
		if msg.err != nil {
			errText := msg.err.Error()
			return func() tea.Msg { return ShowToastMsg{Text: "test audio: " + errText} }
		}
		a.testAudioActive = msg.active
		action := "test audio started"
		if !msg.active {
			action = "test audio stopped"
		}
		return func() tea.Msg { return NewChatResponseAppendMsg(action) }

	case tea.KeyPressMsg:
		if !a.focused {
			return nil
		}
		if a.formActive {
			return a.updateForm(msg)
		}
		return a.updateNav(msg)
	}
	return nil
}

func (a *RoomActionsComponent) updateNav(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "up", "ctrl+p":
		if a.cursor > 0 {
			a.cursor--
		}
	case "down", "ctrl+n":
		if a.cursor < roomActionCount-1 {
			a.cursor++
		}
	case "enter", " ":
		return a.activate()
	}
	return nil
}

func (a *RoomActionsComponent) activate() tea.Cmd {
	switch a.cursor {
	case roomActionCreate:
		a.formActive = true
		a.formMode = actionsFormCreate
		a.formVal = ""
		a.formCur = 0
		a.formErr = ""
	case roomActionInvite:
		a.formActive = true
		a.formMode = actionsFormInvite
		a.formVal = ""
		a.formCur = 0
		a.formErr = ""
	case roomActionVoice:
		if a.testAudioActive {
			return nil // mutually exclusive with test audio
		}
		joining := !a.voiceActive
		return func() tea.Msg {
			if joining {
				err := commands.VoiceJoinDirect()
				return roomsVoiceResultMsg{joined: true, err: err}
			}
			err := commands.VoiceLeaveDirect()
			return roomsVoiceResultMsg{joined: false, err: err}
		}
	case roomActionAudio:
		if a.voiceActive {
			return nil // mutually exclusive with voice chat
		}
		starting := !a.testAudioActive
		return func() tea.Msg {
			if starting {
				err := commands.VoiceTestAudioDirect()
				return roomsTestAudioResultMsg{active: true, err: err}
			}
			err := commands.VoiceLeaveTestAudioDirect()
			return roomsTestAudioResultMsg{active: false, err: err}
		}
	}
	return nil
}

func (a *RoomActionsComponent) submitForm() tea.Cmd {
	val := strings.TrimSpace(a.formVal)
	if val == "" {
		if a.formMode == actionsFormCreate {
			a.formErr = "room name required"
		} else {
			a.formErr = "username required"
		}
		return nil
	}
	a.formErr = ""
	mode := a.formMode
	return func() tea.Msg {
		var text string
		var err error
		var refresh bool
		if mode == actionsFormCreate {
			text, err = commands.CreateRoomDirect(val)
			refresh = err == nil
		} else {
			text, err = commands.InviteUserDirect(val)
		}
		return roomsOpResultMsg{text: text, err: err, refresh: refresh}
	}
}

func (a *RoomActionsComponent) updateForm(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		a.formActive = false
		a.formErr = ""
	case "enter":
		return a.submitForm()
	case "backspace":
		runes := []rune(a.formVal)
		if a.formCur > 0 {
			a.formVal = string(append(runes[:a.formCur-1], runes[a.formCur:]...))
			a.formCur--
		}
	case "left", "ctrl+b":
		if a.formCur > 0 {
			a.formCur--
		}
	case "right", "ctrl+f":
		if a.formCur < len([]rune(a.formVal)) {
			a.formCur++
		}
	case "ctrl+a":
		a.formCur = 0
	case "ctrl+e":
		a.formCur = len([]rune(a.formVal))
	default:
		text := msg.Key().Text
		if text != "" {
			runes := []rune(a.formVal)
			a.formVal = string(runes[:a.formCur]) + text + string(runes[a.formCur:])
			a.formCur += len([]rune(text))
			a.formErr = ""
		}
	}
	return nil
}

func (a *RoomActionsComponent) Render() string {
	title := styles.RoomOpsBadge.Render("ROOM OPS")
	var body string
	if a.formActive {
		label := "Room name: "
		if a.formMode == actionsFormInvite {
			label = "Username:  "
		}
		runes := []rune(a.formVal)
		var fieldText string
		if a.formCur >= len(runes) {
			fieldText = a.formVal + styles.FormCursorStyle.Render(" ")
		} else {
			fieldText = string(runes[:a.formCur]) +
				styles.FormCursorStyle.Render(string(runes[a.formCur])) +
				string(runes[a.formCur+1:])
		}
		line1 := styles.Grey.Render(label) + fieldText
		var line2 string
		if a.formErr != "" {
			line2 = styles.FormErrorStyle.Render(a.formErr)
		} else {
			line2 = styles.Grey.Render("enter submit  •  esc cancel")
		}
		body = line1 + "\n" + line2
	} else {
		type actionItem struct {
			label    string
			inactive string // override label when voice/audio is active
			disabled bool
		}
		voiceLabel := "Join Voice"
		if a.voiceActive {
			voiceLabel = "Leave Voice"
		}
		audioLabel := "Test Audio"
		if a.testAudioActive {
			audioLabel = "Stop Test Audio"
		}
		items := []string{
			"Create",
			"Invite",
			voiceLabel,
			audioLabel,
		}
		disabled := [roomActionCount]bool{
			false,
			false,
			a.testAudioActive, // voice disabled when test audio running
			a.voiceActive,     // audio disabled when voice active
		}
		lines := make([]string, roomActionCount)
		for i, lbl := range items {
			sel := a.focused && a.cursor == i
			cur := "  "
			if sel {
				cur = styles.CursorStyle.Render("▶ ")
			}
			var itemStr string
			if disabled[i] {
				itemStr = styles.ItemStyle.Render(cur + styles.Grey.Render(lbl))
			} else if sel {
				var rendered string
				switch i {
				case roomActionVoice:
					if a.voiceActive {
						rendered = styles.VoiceLeaveFocusedStyle.Render(lbl)
					} else {
						rendered = styles.RoomBtnActiveStyle.Render(lbl)
					}
				case roomActionAudio:
					if a.testAudioActive {
						rendered = styles.VoiceLeaveFocusedStyle.Render(lbl)
					} else {
						rendered = styles.RoomBtnActiveStyle.Render(lbl)
					}
				default:
					rendered = styles.RoomBtnActiveStyle.Render(lbl)
				}
				itemStr = cur + rendered
			} else {
				var rendered string
				switch i {
				case roomActionVoice:
					if a.voiceActive {
						rendered = styles.VoiceLeaveStyle.Render(lbl)
					} else {
						rendered = styles.ItemStyle.Render(lbl)
					}
				case roomActionAudio:
					if a.testAudioActive {
						rendered = styles.VoiceLeaveStyle.Render(lbl)
					} else {
						rendered = styles.ItemStyle.Render(lbl)
					}
				default:
					rendered = styles.ItemStyle.Render(lbl)
				}
				itemStr = cur + rendered
			}
			lines[i] = itemStr
		}
		body = strings.Join(lines, "\n")
	}

	body = title + "\n\n" + body

	borderColor := styles.PanelBorderColor
	bg := styles.ComponentBg
	if a.focused {
		borderColor = styles.PanelFocusedBorderColor
		bg = styles.ComponentFocusedBg
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Background(bg).
		Padding(0, 1).
		Width(a.width).
		Height(a.height).
		Render(body)
}
