// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	wstypes "github.com/srschreiber/nito-client/shared/websocket_types"
	"github.com/srschreiber/nito-client/shellapp/clientlog"
	"github.com/srschreiber/nito-client/shellapp/commands"
	"github.com/srschreiber/nito-client/shellapp/components"
	"github.com/srschreiber/nito-client/shellapp/connection"
	"github.com/srschreiber/nito-client/shellapp/keys"
	"github.com/srschreiber/nito-client/shellapp/styles"
	"github.com/srschreiber/nito-client/shellapp/types"
	"github.com/srschreiber/nito-client/shellapp/voice"
	"github.com/srschreiber/nito-client/sounds"
)

// Box overhead constants (lipgloss borders + padding).
// History/Rooms/Status: RoundedBorder (all 4 sides) + Padding(0,1) → 4 wide, 2 tall.
// Command: ThickBorder (left only) + Padding(0,1) → 3 wide, 0 tall.
// AppStyle: Padding(1,2) → 4 wide, 2 tall.
const (
	histBoxOverheadW  = 4
	histBoxOverheadH  = 2
	rightBoxOverheadW = 4
	cmdBoxOverheadW   = 3
	appPaddingW       = 4
)

// layout holds computed content dimensions for each component.
type layout struct {
	histW, histH int
	statW, statH int
	cmdW         int
	histBoxW     int // outer visual width of the history block (histW + 4)
}

// pHistBoxW is the fraction of usable width given to the history block.
// The status column takes the remainder.
const pHistBoxW = 0.70

// computeLayout derives component content dimensions from the terminal size.
func computeLayout(termW, termH int) layout {
	if termW < 30 {
		termW = 30
	}
	if termH < 12 {
		termH = 12
	}

	usableW := termW - appPaddingW
	pHistBoxH := .9
	histBoxH := int(float64(termH) * pHistBoxH)

	histBoxW := int(float64(usableW) * pHistBoxW)
	rightBoxW := usableW - histBoxW
	if rightBoxW < 10 {
		rightBoxW = 10
		histBoxW = usableW - rightBoxW
	}

	histW := histBoxW - histBoxOverheadW
	histH := histBoxH - histBoxOverheadH

	// Status takes the full right column.
	statW := rightBoxW - rightBoxOverheadW
	statH := histH

	// Pin cmdW to exactly the top-row visual width so they always align.
	cmdW := histBoxW + rightBoxW - cmdBoxOverheadW

	if histW < 10 {
		histW = 10
	}
	if histH < 3 {
		histH = 3
	}
	if statW < 5 {
		statW = 5
	}
	if cmdW < 10 {
		cmdW = 10
	}

	return layout{
		histW: histW, histH: histH,
		statW: statW, statH: statH,
		cmdW:     cmdW,
		histBoxW: histBoxW,
	}
}

type model struct {
	history                 *components.ConversationTabs
	status                  *components.StatusComponent
	command                 *components.CommandComponent
	hints                   *components.HintsComponent
	toast                   *components.ToastComponent
	voiceSettings           *components.VoiceSettingsScreen
	showVoiceSettings       bool
	audioPlayerSettings     *components.AudioPlayerSettingsScreen
	showAudioPlayerSettings bool
	audioPlayerPresets      *components.AudioPlayerPresetsScreen
	showAudioPlayerPresets  bool
	comps                   []components.Component
	focusable               []int
	focusedComponent        int
	termW, termH            int
	histBoxW                int  // outer visual width of the history block
	offlineStreak           int  // consecutive failed pings
	kickedToLogin           bool // set true when kicked back to login screen
	// audioTracks holds per-track cancellation. Index 0–2 correspond to tracks 0–2.
	// A nil cancel means that track is idle.
	audioTracks         [3]context.CancelFunc
	audioTrackStartedBy [3]string // username who started each track; "" if idle
	audioTrackBroadcast [3]bool   // true if the track was network-broadcast
}

// startupSuccessMsg holds the auth result message to display on first launch.
var startupSuccessMsg string

func initialModel() model {
	termW, termH := 120, 40
	l := computeLayout(termW, termH)
	history := components.NewConversationTabs(l.histW, l.histH)
	status := components.NewStatusComponent(l.statW, l.statH)
	command := components.NewCommandComponent(l.cmdW)
	hints := components.NewHintsComponent(0, 0)
	toast := components.NewToastComponent()

	// comps: 0=history, 1=status, 2=command, 3=hints (display-only)
	m := model{
		history:             history,
		status:              status,
		command:             command,
		hints:               hints,
		toast:               toast,
		voiceSettings:       components.NewVoiceSettingsScreen(termW, termH),
		audioPlayerSettings: components.NewAudioPlayerSettingsScreen(termW, termH),
		audioPlayerPresets:  components.NewAudioPlayerPresetsScreen(termW, termH),
		comps:               []components.Component{history, status, command, hints},
		focusable:           []int{0, 1, 2}, // status always navigable (TRACKS always visible)
		focusedComponent:    2,              // index into focusable → comps[2] = command
		termW:               termW,
		termH:               termH,
		histBoxW:            l.histBoxW,
	}
	m.comps[m.focusable[m.focusedComponent]].SetFocused(true)
	m.hints.SetFocusedComp(m.focusable[m.focusedComponent])
	return m
}

func (m *model) relayout(termW, termH int) {
	m.termW, m.termH = termW, termH
	l := computeLayout(termW, termH)
	m.histBoxW = l.histBoxW
	m.history.SetSize(l.histW, l.histH)
	// hints component is not rendered; no resize needed
	m.status.SetSize(l.statW, l.statH)
	m.command.SetWidth(l.cmdW)
	m.voiceSettings.SetSize(termW, termH)
	m.audioPlayerSettings.SetSize(termW, termH)
	m.audioPlayerPresets.SetSize(termW, termH)
}

// notificationMsg is delivered to the model when the readLoop routes a
// server-push notification from the broker.
type notificationMsg wstypes.NotificationPayload
type echoWsMsg wstypes.EchoPayload
type roomMessageWsMsg wstypes.RoomMessagePayload

// dmReceivedMsg carries a decrypted incoming direct message.
type dmReceivedMsg struct {
	FromUser string
	Text     string
}

const pingInterval = 2500 * time.Millisecond

// maxOfflineStreak is the number of consecutive ping failures before kicking to login.
// Each failure triggers an immediate reconnect attempt, so this is effectively
// (maxOfflineStreak - 1) reconnect attempts before giving up.
const maxOfflineStreak = 4

type pingTickMsg struct{}
type pingResultMsg struct {
	connected bool
	latencyMs int64
}
type reconnectResultMsg struct{ err error }

func waitPingTick() tea.Cmd {
	return tea.Tick(pingInterval, func(time.Time) tea.Msg { return pingTickMsg{} })
}

func doPing() tea.Cmd {
	return func() tea.Msg {
		start := time.Now()
		err := connection.PingBroker()
		return pingResultMsg{connected: err == nil, latencyMs: time.Since(start).Milliseconds()}
	}
}

func doReconnect() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		err := connection.Reconnect(ctx)
		if err == nil {
			clientlog.Info("reconnected to broker")
		} else {
			clientlog.Warn("reconnect failed: %v", err)
		}
		return reconnectResultMsg{err: err}
	}
}

// waitNotification blocks on the notification channel the readLoop feeds and
// returns the text as a notificationMsg. The model re-arms this after each hit.
func waitNotification() tea.Cmd {
	return func() tea.Msg {
		ch := connection.NotifChan()
		if ch == nil {
			return nil
		}
		text, ok := <-ch
		if !ok {
			return nil
		}

		// conv to notificationMsg for type safety and to avoid string conversions in the readLoop.'
		var payload wstypes.NotificationPayload
		if err := json.Unmarshal([]byte(text), &payload); err != nil {
			fmt.Printf("waitNotification: unmarshal payload: %v\n", err)
			// to rearm
			return notificationMsg{}
		}
		return notificationMsg(payload)
	}
}

func waitRoomMessages() tea.Cmd {
	return func() tea.Msg {
		ch := connection.RoomMessageChan()
		if ch == nil {
			return nil
		}
		data, ok := <-ch
		if !ok {
			return nil
		}

		var payload wstypes.RoomMessagePayload
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			fmt.Printf("waitRoomMessages: unmarshal payload: %v\n", err)
			// to rearm
			return roomMessageWsMsg{}
		}

		ukc, err := connection.GetOrCreateRoomKeyChain()
		if err != nil {
			fmt.Printf("waitRoomMessages: get room key chain: %v\n", err)
			return roomMessageWsMsg{}
		}

		ciphertext, err := base64.StdEncoding.DecodeString(payload.EncryptedText)
		if err != nil {
			fmt.Printf("waitRoomMessages: decode ciphertext: %v\n", err)
			return roomMessageWsMsg{}
		}
		plaintext, err := ukc.DecryptMessageWithRoomKey(ciphertext, payload.FromUsername, &payload.SenderMessageCount)
		if err != nil {
			fmt.Printf("waitRoomMessages: decrypt message: %v\n", err)
			return roomMessageWsMsg{}
		}
		payload.EncryptedText = string(plaintext)
		return roomMessageWsMsg(payload)
	}
}

func waitEcho() tea.Cmd {
	return func() tea.Msg {
		ch := connection.EchoChan()
		if ch == nil {
			return nil
		}
		data, ok := <-ch
		if !ok {
			return nil
		}

		var payload wstypes.EchoPayload
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			fmt.Printf("waitEcho: unmarshal payload: %v\n", err)
			// to rearm
			return echoWsMsg{}
		}
		return echoWsMsg(payload)
	}
}

func waitDM() tea.Cmd {
	return func() tea.Msg {
		ch := connection.DMChan()
		if ch == nil {
			return nil
		}
		data, ok := <-ch
		if !ok {
			return nil
		}
		var dm struct {
			FromUsername string `json:"fromUsername"`
			PlainText    string `json:"plainText"`
		}
		if err := json.Unmarshal(data, &dm); err != nil {
			fmt.Printf("waitDM: unmarshal: %v\n", err)
			return dmReceivedMsg{}
		}
		return dmReceivedMsg{FromUser: dm.FromUsername, Text: dm.PlainText}
	}
}

func (m model) Init() tea.Cmd {
	var cmds []tea.Cmd
	for _, c := range m.comps {
		if cmd := c.Init(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if cmd := m.toast.Init(); cmd != nil {
		cmds = append(cmds, cmd)
	}
	cmds = append(cmds, waitNotification(), waitEcho(), waitRoomMessages(), waitDM(), waitPingTick(), doPing(), m.trackStateCmd())
	if startupSuccessMsg != "" {
		msg := startupSuccessMsg
		cmds = append(cmds, func() tea.Msg { return components.NewResponseAppendMsg(msg) })
	}
	return tea.Batch(cmds...)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.relayout(msg.Width, msg.Height)
		return m, nil
	case pingTickMsg:
		return m, tea.Batch(doPing(), waitPingTick())
	case reconnectResultMsg:
		if msg.err == nil {
			m.offlineStreak = 0
			// New channels were created by Reconnect; re-arm all channel waiters.
			return m, tea.Batch(waitNotification(), waitEcho(), waitRoomMessages(), waitDM())
		}
		// Reconnect failed; streak was already incremented on the ping failure that
		// triggered this. The next ping tick will try again or kick if over the limit.
		return m, nil

	case pingResultMsg:
		if msg.connected {
			m.offlineStreak = 0
		} else {
			m.offlineStreak++
			if m.offlineStreak >= maxOfflineStreak {
				// Too many failures — leave voice chat and return to login.
				go voice.LeaveIfActive()
				m.kickedToLogin = true
				return m, tea.Quit
			}
			// Attempt to silently reconnect before the next ping tick.
			return m, doReconnect()
		}
		userID := ""
		if s := connection.CurrentSession(); s != nil {
			userID = s.UserID
		}
		connMsg := types.ConnectionStatusMsg{
			Connected: msg.connected,
			BrokerURL: connection.BrokerURL(),
			UserID:    userID,
			LatencyMs: msg.latencyMs,
		}
		for _, c := range m.comps {
			c.Update(connMsg)
		}
		return m, nil
	case dmReceivedMsg:
		if msg.FromUser == "" {
			return m, waitDM()
		}
		from := msg.FromUser
		text := fmt.Sprintf("[%s]: %s", from, msg.Text)
		notifText := fmt.Sprintf("[DM from %s]: %s", from, msg.Text)
		toastText := from + " messaged you"
		clientlog.Info("DM from %s: %s", from, msg.Text)
		return m, tea.Batch(
			func() tea.Msg { return components.NewDMResponseAppendMsg(from, text) },
			func() tea.Msg { return components.NewNotificationAppendMsg(notifText) },
			func() tea.Msg { return components.ShowToastMsg{Text: toastText} },
			waitDM(),
		)
	case components.AudioTrackDoneMsg:
		// Track finished naturally — free the slot.
		if msg.Track >= 0 && msg.Track <= 2 {
			m.audioTracks[msg.Track] = nil
			m.audioTrackStartedBy[msg.Track] = ""
			m.audioTrackBroadcast[msg.Track] = false
		}
		return m, m.trackStateCmd()
	case components.AudioPlaybackErrorMsg:
		// Playback failed — free the slot and show the error toast.
		if msg.Track >= 0 && msg.Track <= 2 {
			m.audioTracks[msg.Track] = nil
			m.audioTrackStartedBy[msg.Track] = ""
			m.audioTrackBroadcast[msg.Track] = false
		}
		text := msg.Text
		return m, tea.Batch(
			m.trackStateCmd(),
			func() tea.Msg { return components.ShowToastMsg{Text: text} },
		)
	case components.StopAudioMsg:
		if msg.Track < 0 {
			for i := range m.audioTracks {
				if m.audioTracks[i] != nil {
					m.audioTracks[i]()
					m.audioTracks[i] = nil
				}
				m.audioTrackStartedBy[i] = ""
				m.audioTrackBroadcast[i] = false
			}
		} else if msg.Track <= 2 {
			if m.audioTracks[msg.Track] != nil {
				m.audioTracks[msg.Track]()
				m.audioTracks[msg.Track] = nil
			}
			m.audioTrackStartedBy[msg.Track] = ""
			m.audioTrackBroadcast[msg.Track] = false
		}
		return m, m.trackStateCmd()
	case components.RefreshTrackStateMsg:
		return m, m.trackStateCmd()
	case components.PreFillCommandMsg:
		// Switch focus to command so the user can complete the pre-filled text.
		m.comps[m.focusable[m.focusedComponent]].SetFocused(false)
		for i, fi := range m.focusable {
			if m.comps[fi] == m.command {
				m.focusedComponent = i
				break
			}
		}
		m.comps[m.focusable[m.focusedComponent]].SetFocused(true)
		m.hints.SetFocusedComp(m.focusable[m.focusedComponent])
		// Also deliver the message to command so it sets the text.
		return m, m.command.Update(msg)
	case components.PlayAudioMsg:
		track := m.resolveTrack(msg.Track)
		url := msg.URL
		if m.audioTracks[track] != nil {
			m.audioTracks[track]() // cancel existing on this track
		}
		ctx, cancel := context.WithCancel(context.Background())
		m.audioTracks[track] = cancel
		roomID := voice.ActiveRoomID()
		var extraCmds []tea.Cmd
		if msg.Broadcast && roomID != "" {
			// Broadcast to voice room: send RPC and let the broker echo start local playback.
			if err := commands.PlayAudioDirect(url, track); err != nil {
				m.audioTracks[track] = nil
				cancel()
				errMsg := err.Error()
				return m, tea.Batch(
					m.trackStateCmd(),
					func() tea.Msg { return components.ShowToastMsg{Text: ".play: " + errMsg} },
				)
			}
			// The broker echo (SoundClip notification) will set startedBy + start playback.
			extraCmds = append(extraCmds, m.trackStateCmd())
		} else {
			// Local play only (default): play directly without any broker RPC.
			m.audioTrackStartedBy[track] = connection.GetSessionUserID()
			m.audioTrackBroadcast[track] = false
			var playCmd tea.Cmd
			if isLocalAudioPath(url) {
				playCmd = components.PlayAudioFromFile(ctx, url, track)
			} else {
				localRoom := roomID
				if localRoom == "" {
					localRoom = voice.SelfRoomID
				}
				playCmd = components.PlayAudioFromURL(ctx, localRoom, url, track)
			}
			extraCmds = append(extraCmds, m.trackStateCmd(), playCmd)
		}
		return m, tea.Batch(extraCmds...)
	case types.ConnectedMsg:
		return m, tea.Batch(waitNotification(), waitEcho(), waitRoomMessages(), waitDM())
	case notificationMsg:
		text := msg.Text
		clientlog.Info("notification: %s [type=%s]", text, msg.Type)
		cmds := []tea.Cmd{
			func() tea.Msg { return components.NewNotificationAppendMsg(text) },
			waitNotification(),
		}
		switch msg.Type {
		case wstypes.NotificationTypeSoundClip:
			var p wstypes.PlayAudioPayload
			raw, _ := json.Marshal(msg.Data)
			if err := json.Unmarshal(raw, &p); err != nil || p.AudioURL == "" {
				clientlog.Error("sound clip notification: bad payload: %v", err)
				cmds = append(cmds, func() tea.Msg {
					return components.ShowToastMsg{Text: "play: received malformed sound clip notification"}
				})
			} else {
				track := p.Track
				if track < 0 || track > 2 {
					track = 0
				}
				if m.audioTracks[track] != nil {
					m.audioTracks[track]()
				}
				ctx, cancel := context.WithCancel(context.Background())
				m.audioTracks[track] = cancel
				roomID := p.RoomID
				audioURL := p.AudioURL
				fromUser := p.FromUsername
				m.audioTrackStartedBy[track] = fromUser
				m.audioTrackBroadcast[track] = true
				playMsg := fmt.Sprintf("%s is playing %s (track %d)", fromUser, audioURL, track)
				cmds = append(cmds,
					func() tea.Msg { return components.NewChatResponseAppendMsg(playMsg) },
					m.trackStateCmd(),
					components.PlayAudioFromURL(ctx, roomID, audioURL, track),
				)
			}

		case wstypes.NotificationTypeUserAddedToRoom:
			cmds = append(cmds, func() tea.Msg {
				return components.ShowToastMsg{Text: "You have a new room invite — check Notifications tab"}
			})
			cmds = append(cmds, func() tea.Msg { return components.NewInvitesAppendMsg() })
		case wstypes.NotificationTypeUserJoinedRoom:
			sounds.PlayEnter()
			toastText := text
			cmds = append(cmds,
				func() tea.Msg { return components.ShowToastMsg{Text: toastText} },
				func() tea.Msg { return types.MembersUpdatedMsg{} },
			)
		case wstypes.NotificationTypeUserLeftRoom:
			sounds.PlayExit()
			toastText := text
			cmds = append(cmds,
				func() tea.Msg { return components.ShowToastMsg{Text: toastText} },
				func() tea.Msg { return types.MembersUpdatedMsg{} },
			)
		case wstypes.NotificationTypeUserJoinedVoiceChat:
			toastText := text
			username := msg.Username
			cmds = append(cmds,
				func() tea.Msg { return components.ShowToastMsg{Text: toastText} },
				func() tea.Msg { return types.UserJoinedVoiceChatMsg{Username: username} },
			)
		case wstypes.NotificationTypeUserLeftVoiceChat:
			username := msg.Username
			cmds = append(cmds,
				func() tea.Msg { return types.UserLeftVoiceChatMsg{Username: username} },
			)
		}
		return m, tea.Batch(cmds...)
	case echoWsMsg:
		text := msg.Text
		// dispatch a new append message to the history component, and re-arm the echo wait.
		return m, tea.Batch(
			func() tea.Msg { return components.NewResponseAppendMsg("echo response: " + text) },
			waitEcho(),
		)
	case roomMessageWsMsg:
		var cmds []tea.Cmd
		cmds = append(cmds, waitRoomMessages())
		sessionRoomID := connection.GetSessionRoomID()
		if sessionRoomID != nil && msg.RoomID == *sessionRoomID {
			if msg.MessageType == wstypes.MessageTypeImage {
				header := fmt.Sprintf("[%s] sent an image:", msg.FromUsername)
				ascii := msg.EncryptedText // already decrypted by waitRoomMessages
				cmds = append(cmds, func() tea.Msg {
					return components.NewChatImageAppendMsg(header, ascii)
				})
			} else {
				text := fmt.Sprintf("[%s]: %s", msg.FromUsername, msg.EncryptedText)
				cmds = append(cmds, func() tea.Msg { return components.NewChatResponseAppendMsg(text) })
			}
		}
		return m, tea.Batch(cmds...)
	case types.RoomSelectedMsg:
		roomID := msg.RoomID
		go commands.SendRoomEnter(roomID)
		// Leave voice chat if active in the previous room.
		go voice.LeaveIfActive()
		// Cancel all playing tracks when joining a new room.
		for i := range m.audioTracks {
			if m.audioTracks[i] != nil {
				m.audioTracks[i]()
				m.audioTracks[i] = nil
			}
		}
		m.setFocusable([]int{0, 1, 2}) // keep all three focusable
		var cmds []tea.Cmd
		cmds = append(cmds, m.trackStateCmd())
		for _, c := range m.comps {
			if cmd := c.Update(msg); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		cmds = append(cmds, loadRoomHistory(roomID))
		return m, tea.Batch(cmds...)
	case types.RoomDeselectedMsg:
		go commands.SendRoomLeave(msg.RoomID)
		m.setFocusable([]int{0, 1, 2}) // keep all three focusable
		return m, m.trackStateCmd()
	case components.ShowVoiceSettingsMsg:
		m.showVoiceSettings = true
		m.voiceSettings.Reset()
		return m, nil
	case components.HideVoiceSettingsMsg:
		m.showVoiceSettings = false
		return m, nil
	case components.ShowAudioPlayerSettingsMsg:
		m.showAudioPlayerSettings = true
		return m, nil
	case components.HideAudioPlayerSettingsMsg:
		m.showAudioPlayerSettings = false
		return m, nil
	case components.ShowAudioPlayerPresetsMsg:
		m.showAudioPlayerPresets = true
		m.audioPlayerPresets.Refresh()
		return m, nil
	case components.HideAudioPlayerPresetsMsg:
		m.showAudioPlayerPresets = false
		return m, nil
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseMiddle {
			return m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		}
		return m, nil
	case tea.KeyPressMsg:
		if m.showAudioPlayerPresets {
			if msg.String() == "ctrl+c" {
				m.command.SaveHistory()
				return m, tea.Quit
			}
			return m, m.audioPlayerPresets.Update(msg)
		}
		if m.showAudioPlayerSettings {
			if msg.String() == "ctrl+c" {
				m.command.SaveHistory()
				return m, tea.Quit
			}
			return m, m.audioPlayerSettings.Update(msg)
		}
		if m.showVoiceSettings {
			if msg.String() == "ctrl+c" {
				m.command.SaveHistory()
				return m, tea.Quit
			}
			return m, m.voiceSettings.Update(msg)
		}
		switch msg.String() {
		case "ctrl+c":
			m.command.SaveHistory()
			return m, tea.Quit
		case "/":
			// Always focus the command component on /; don't type the character.
			if m.comps[m.focusable[m.focusedComponent]] != m.command {
				m.comps[m.focusable[m.focusedComponent]].SetFocused(false)
				for i, fi := range m.focusable {
					if m.comps[fi] == m.command {
						m.focusedComponent = i
						break
					}
				}
				m.comps[m.focusable[m.focusedComponent]].SetFocused(true)
				m.hints.SetFocusedComp(m.focusable[m.focusedComponent])
				return m, nil
			}
			return m, m.comps[m.focusable[m.focusedComponent]].Update(msg)
		case "ctrl+[", "esc":
			// ctrl+[ sends ESC in most terminals; both switch to the previous tab.
			return m, m.history.Update(msg)
		case "ctrl+]":
			// Switch to the next tab from any focused component.
			return m, m.history.Update(msg)
		case "left", "right":
			// Route left/right to history for tab switching only when the command
			// component is not focused — in command mode they move the cursor.
			if m.comps[m.focusable[m.focusedComponent]] != m.command {
				return m, m.history.Update(msg)
			}
			return m, m.command.Update(msg)
		case "tab":
			if m.command.HasSuggestion() {
				return m, m.comps[m.focusable[m.focusedComponent]].Update(msg)
			}
			// Let history consume tab for internal sub-focus cycling (rooms → buttons → history).
			if m.history.CanConsumeTab() {
				return m, m.history.Update(msg)
			}
			m.comps[m.focusable[m.focusedComponent]].SetFocused(false)
			m.focusedComponent = (m.focusedComponent + 1) % len(m.focusable)
			// Skip history if the active tab has no scrollable content to focus.
			if m.comps[m.focusable[m.focusedComponent]] == m.history && !m.history.CanFocus() {
				m.focusedComponent = (m.focusedComponent + 1) % len(m.focusable)
			}
			m.comps[m.focusable[m.focusedComponent]].SetFocused(true)
			m.hints.SetFocusedComp(m.focusable[m.focusedComponent])
			return m, nil
		default:
			return m, m.comps[m.focusable[m.focusedComponent]].Update(msg)
		}
	case types.ErrorMsg:
		text := fmt.Sprintf("error: %s", msg.Message)
		cmdOut := func() tea.Msg { return components.NewResponseAppendMsg(text) }
		toast := func() tea.Msg { return components.ShowToastMsg{Text: text[:64]} }
		return m, tea.Batch(cmdOut, toast)
	default:
		// Broadcast non-key messages to all components.
		var cmds []tea.Cmd
		for _, c := range m.comps {
			if cmd := c.Update(msg); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		if cmd := m.voiceSettings.Update(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if cmd := m.toast.Update(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)
	}
}

// isLocalAudioPath reports whether url is an absolute or home-relative file
// path rather than an http(s) URL (e.g. "/home/user/song.mp3" or "~/music.mp3").
func isLocalAudioPath(url string) bool {
	return strings.HasPrefix(url, "/") || strings.HasPrefix(url, "~/")
}

// resolveTrack returns the track to use for playback. hint -1 means auto: pick
// the first idle track, or 0 if all are busy. hint 0–2 is used directly.
func (m *model) resolveTrack(hint int) int {
	if hint >= 0 && hint <= 2 {
		return hint
	}
	for i, cancel := range m.audioTracks {
		if cancel == nil {
			return i
		}
	}
	return 0 // all busy; preempt track 0
}

// setFocusable replaces the focusable list and keeps the focused component
// consistent. If the currently focused component is no longer in the new list,
// focus falls back to the command component.
func (m *model) setFocusable(newFocusable []int) {
	currentComp := m.comps[m.focusable[m.focusedComponent]]
	m.comps[m.focusable[m.focusedComponent]].SetFocused(false)
	m.focusable = newFocusable
	// Try to keep focus on the same component.
	for i, fi := range newFocusable {
		if m.comps[fi] == currentComp {
			m.focusedComponent = i
			m.comps[m.focusable[m.focusedComponent]].SetFocused(true)
			m.hints.SetFocusedComp(m.focusable[m.focusedComponent])
			return
		}
	}
	// Fall back to command.
	for i, fi := range newFocusable {
		if m.comps[fi] == m.command {
			m.focusedComponent = i
			break
		}
	}
	m.comps[m.focusable[m.focusedComponent]].SetFocused(true)
	m.hints.SetFocusedComp(m.focusable[m.focusedComponent])
}

// trackStateCmd returns a Cmd that broadcasts the current track playing state
// and alias list to all components (primarily the status panel).
func (m *model) trackStateCmd() tea.Cmd {
	var playing [3]bool
	startedBy := m.audioTrackStartedBy
	broadcast := m.audioTrackBroadcast
	for i, c := range m.audioTracks {
		playing[i] = c != nil
	}
	inRoom := connection.GetSessionRoomID() != nil
	return func() tea.Msg {
		aliasMap, _ := voice.ListAudioAliases()
		aliases := make([]components.AliasEntry, 0, len(aliasMap))
		for name := range aliasMap {
			aliases = append(aliases, components.AliasEntry{Name: name})
		}
		sort.Slice(aliases, func(i, j int) bool { return aliases[i].Name < aliases[j].Name })
		// Pad to exactly 5 slots; empty Name signals an unfilled slot.
		for len(aliases) < 5 {
			aliases = append(aliases, components.AliasEntry{})
		}
		return components.TrackStateMsg{
			Playing:   playing,
			InRoom:    inRoom,
			Aliases:   aliases,
			StartedBy: startedBy,
			Broadcast: broadcast,
		}
	}
}

func (m model) View() tea.View {
	if m.showAudioPlayerPresets {
		v := tea.NewView(styles.AppStyle.Render(m.audioPlayerPresets.Render()))
		v.AltScreen = true
		return v
	}
	if m.showAudioPlayerSettings {
		v := tea.NewView(styles.AppStyle.Render(m.audioPlayerSettings.Render()))
		v.AltScreen = true
		return v
	}
	if m.showVoiceSettings {
		v := tea.NewView(styles.AppStyle.Render(m.voiceSettings.Render()))
		v.AltScreen = true
		return v
	}

	rightCol := lipgloss.JoinHorizontal(lipgloss.Top, m.status.Render() /*, m.hints.Render()*/)
	// Force history to exactly histBoxW columns so JoinVertical trailing-space padding
	// (from a narrower active-tab content block) does not create a visual gap before rightCol.
	// Explicit ComponentBg prevents ANSI bleed from the focused history panel spilling into
	// the padding cells and then into the status panel to its right.
	histStr := lipgloss.NewStyle().Width(m.histBoxW).Background(styles.ComponentBg).Render(m.history.Render())
	topRow := lipgloss.JoinHorizontal(lipgloss.Left, histStr, rightCol)
	s := topRow + "\n" + m.command.Render()

	helpText := styles.HelpStyle.Render(
		"  tab        switch focus\n" +
			"  ctrl+[/]   switch tabs\n" +
			"  ctrl+c     quit")
	if m.toast.Visible() {
		toastStr := m.toast.Render()
		usableW := m.termW - appPaddingW
		toastW := lipgloss.Width(toastStr)
		padW := usableW - lipgloss.Width(helpText) - toastW
		if padW < 1 {
			padW = 1
		}
		pad := styles.DimText.Width(padW).Render("")
		s += "\n" + lipgloss.JoinHorizontal(lipgloss.Top, helpText, pad, toastStr)
	} else {
		s += "\n" + helpText
	}

	v := tea.NewView(styles.AppStyle.Render(s))
	v.AltScreen = true
	return v
}

// loadRoomHistory fetches the most recent 50 messages for roomID, decrypts them,
// and returns an AppendHistoryMsg containing the plaintext lines.
func loadRoomHistory(roomID string) tea.Cmd {
	return func() tea.Msg {
		resp, err := connection.GetRoomMessages(roomID, 50)
		if err != nil {
			return components.NewChatResponseAppendMsg("(history unavailable: " + err.Error() + ")")
		}
		if len(resp.UserMessages) == 0 {
			return components.NewChatResponseAppendMsg("(no message history)")
		}

		// Build one RoomKeyChain per key version using the user's encrypted copies.
		keyChains := make(map[int]*keys.RoomKeyChain)
		for _, rk := range resp.RoomKeys {
			rawKey, err := keys.DecryptRoomKey(rk.EncryptedRoomKey, connection.GetSessionUserID())
			if err != nil {
				continue
			}
			keyChains[rk.KeyVersion] = keys.NewRoomKeyChain(rawKey)
		}

		var entries []components.AppendHistoryMsg
		for _, msg := range resp.UserMessages {
			chain, ok := keyChains[msg.RoomKeyVersion]
			if !ok {
				continue
			}
			ct, err := base64.StdEncoding.DecodeString(msg.EncryptedMessage)
			if err != nil {
				continue
			}
			plaintext, err := chain.DecryptHistoricalMessage(ct, msg.SenderUsername, &msg.SenderMessageCount)
			if err != nil {
				continue
			}
			if msg.MessageType == wstypes.MessageTypeImage {
				header := fmt.Sprintf("[%s] sent an image:", msg.SenderUsername)
				entries = append(entries, components.NewChatImageAppendMsg(header, string(plaintext)))
			} else {
				entries = append(entries, components.NewChatResponseAppendMsg(fmt.Sprintf("[%s]: %s", msg.SenderUsername, string(plaintext))))
			}
		}

		if len(entries) == 0 {
			return nil
		}
		// Flatten all entries into one AppendHistoryMsg targeted at the Chat tab.
		combined := components.AppendHistoryMsg{Tab: components.TabChat}
		for _, e := range entries {
			combined.Entries = append(combined.Entries, e.Entries...)
		}
		return combined
	}
}

func main() {
	for {
		// Run the startup auth form.
		fp := tea.NewProgram(newStartupModel())
		fm, err := fp.Run()
		if err != nil {
			fmt.Println("error:", err)
			os.Exit(1)
		}
		form := fm.(startupModel)
		if !form.done {
			// User quit the form without completing auth.
			return
		}
		startupSuccessMsg = form.successMsg

		prefs := loadLoginPrefs()
		components.ShowCmdTab = prefs.ShowCmdTab
		voice.LoadAudioSettings()

		p := tea.NewProgram(initialModel())
		clientlog.Init(func(msg any) { go p.Send(msg) })
		result, err := p.Run()
		if err != nil {
			fmt.Println("error:", err)
			os.Exit(1)
		}
		// If the main TUI was kicked back to login, loop back to the auth form.
		if m, ok := result.(model); ok && m.kickedToLogin {
			voice.LeaveIfActive() // ensure voice session is torn down before re-auth
			connection.Disconnect()
			continue
		}
		voice.LeaveIfActive() // tear down mic stream before process exits
		return
	}
}
