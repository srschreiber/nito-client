// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package components

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/srschreiber/nito-client/shellapp/clientlog"
	"github.com/srschreiber/nito-client/shellapp/commands"
	"github.com/srschreiber/nito-client/shellapp/connection"
	"github.com/srschreiber/nito-client/shellapp/styles"
	"github.com/srschreiber/nito-client/shellapp/types"
	"github.com/srschreiber/nito-client/shellapp/voice"
)

// Room action indices.
const (
	roomActionCreate   = 0
	roomActionInvite   = 1
	roomActionVoice    = 2
	roomActionSettings = 3
	roomActionCount    = 4
)

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
// Voice Settings actions. Items are navigated with up/down or ctrl+p/ctrl+n; Enter/Space activates.
type RoomActionsComponent struct {
	focused         bool
	cursor          int
	spinnerFrame    int
	testAudioActive bool // mirrors VoiceSettingsScreen state for voice mutual exclusion
	width           int
	height          int
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
}

func (a *RoomActionsComponent) Init() tea.Cmd { return nil }

func (a *RoomActionsComponent) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case vsSpinnerTickMsg:
		if voice.IsConnecting() {
			a.spinnerFrame = (a.spinnerFrame + 1) % len(spinnerFrames)
			return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return vsSpinnerTickMsg{} })
		}

	case roomsVoiceResultMsg:
		if msg.err != nil {
			errText := msg.err.Error()
			return func() tea.Msg { return ShowToastMsg{Text: "voice: " + errText} }
		}
		username := connection.GetSessionUserID()
		if msg.joined {
			action := "joined voice chat"
			return tea.Batch(
				func() tea.Msg { return NewChatResponseAppendMsg(action) },
				func() tea.Msg { return types.UserJoinedVoiceChatMsg{Username: username} },
				func() tea.Msg { return StopAudioMsg{Track: -1} },
			)
		}
		action := "left voice chat"
		return tea.Batch(
			func() tea.Msg { return NewChatResponseAppendMsg(action) },
			func() tea.Msg { return types.UserLeftVoiceChatMsg{Username: username} },
		)

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
		return func() tea.Msg { return PreFillCommandMsg{Text: ".createroom --name ", CursorPos: -1} }
	case roomActionInvite:
		return func() tea.Msg { return PreFillCommandMsg{Text: ".invite --user ", CursorPos: -1} }
	case roomActionVoice:
		if a.testAudioActive || voice.IsConnecting() {
			return nil
		}
		if !voice.IsActive() {
			a.spinnerFrame = 0
			return tea.Batch(
				tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return vsSpinnerTickMsg{} }),
				func() tea.Msg {
					clientlog.Info("joining voice chat")
					err := commands.VoiceJoinDirect()
					if err != nil {
						clientlog.Error("voice join failed: %v", err)
					}
					return roomsVoiceResultMsg{joined: err == nil, err: err}
				},
			)
		}
		return func() tea.Msg {
			clientlog.Info("leaving voice chat")
			err := commands.VoiceLeaveDirect()
			if err != nil {
				clientlog.Error("voice leave failed: %v", err)
			}
			return roomsVoiceResultMsg{joined: false, err: err}
		}
	case roomActionSettings:
		return func() tea.Msg { return ShowVoiceSettingsMsg{} }
	}
	return nil
}

func (a *RoomActionsComponent) Render() string {
	title := styles.RoomOpsBadge.Render("ROOM OPS")

	voiceConnecting := voice.IsConnecting()
	voiceActive := voice.IsActive()
	voiceLabel := "Join Voice"
	if voiceConnecting {
		voiceLabel = spinnerFrames[a.spinnerFrame] + " Connecting..."
	} else if voiceActive {
		voiceLabel = "Leave Voice"
	}
	items := []string{
		"Create",
		"Invite",
		voiceLabel,
		"Voice Settings",
	}
	inRoom := connection.GetSessionRoomID() != nil
	disabled := [roomActionCount]bool{
		false,
		false,
		!inRoom || voiceConnecting || voiceActive,
		false,
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
			itemStr = styles.ItemStyle.Render(cur + styles.DimText.Render(lbl))
		} else if sel {
			var rendered string
			if i == roomActionVoice && voiceActive {
				rendered = styles.VoiceLeaveFocusedStyle.Render(lbl)
			} else {
				rendered = styles.RoomBtnActiveStyle.Render(lbl)
			}
			itemStr = cur + rendered
		} else {
			var rendered string
			if i == roomActionVoice && voiceActive {
				rendered = styles.VoiceLeaveStyle.Render(lbl)
			} else {
				rendered = styles.ItemStyle.Render(lbl)
			}
			itemStr = cur + rendered
		}
		lines[i] = itemStr
	}
	body := title + "\n" + strings.Join(lines, "\n")

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
		Width(a.width + 4).
		Height(a.height).
		Render(body)
}
