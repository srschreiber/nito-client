// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
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
	histW, histH   int
	hintsW, hintsH int
	statW, statH   int
	cmdW           int
}

// computeLayout derives component content dimensions from the terminal size.
// The history pane (which now includes the rooms/members side panel internally)
// takes ~75% of width; the right column holds hints + status.
func computeLayout(termW, termH int) layout {
	if termW < 30 {
		termW = 30
	}
	if termH < 12 {
		termH = 12
	}

	usableW := termW - appPaddingW
	pHistBoxW := .80
	pHistBoxH := .9
	histBoxW := int(float64(usableW) * pHistBoxW)
	histBoxH := int(float64(termH) * pHistBoxH)

	histW := histBoxW - histBoxOverheadW
	histH := histBoxH - histBoxOverheadH

	rightBoxW := usableW - histBoxW

	// Hints and status sit side by side, each half the right column width.
	halfRight := rightBoxW / 2
	hintsW := halfRight - rightBoxOverheadW
	statW := rightBoxW - halfRight - rightBoxOverheadW
	hintsH := histH
	statH := histH

	// Pin cmdW to exactly the top-row visual width so they always align.
	cmdW := histBoxW + rightBoxW - cmdBoxOverheadW

	if histW < 10 {
		histW = 10
	}
	if histH < 3 {
		histH = 3
	}
	if hintsW < 5 {
		hintsW = 5
	}
	if statW < 5 {
		statW = 5
	}
	if cmdW < 10 {
		cmdW = 10
	}

	return layout{
		histW: histW, histH: histH,
		hintsW: hintsW, hintsH: hintsH,
		statW: statW, statH: statH,
		cmdW: cmdW,
	}
}

type model struct {
	history           *components.ConversationTabs
	status            *components.StatusComponent
	command           *components.CommandComponent
	hints             *components.HintsComponent
	toast             *components.ToastComponent
	voiceSettings     *components.VoiceSettingsScreen
	showVoiceSettings bool
	comps             []components.Component
	focusable         []int
	focusedComponent  int
	termW, termH      int
	offlineStreak     int  // consecutive failed pings
	kickedToLogin     bool // set true when kicked back to login screen
	// audioTracks holds per-track cancellation. Index 0–2 correspond to tracks 0–2.
	// A nil cancel means that track is idle.
	audioTracks [3]context.CancelFunc
}

// startupSuccessMsg holds the auth result message to display on first launch.
var startupSuccessMsg string

func initialModel() model {
	termW, termH := 120, 40
	l := computeLayout(termW, termH)
	history := components.NewConversationTabs(l.histW, l.histH)
	status := components.NewStatusComponent(l.statW, l.statH)
	command := components.NewCommandComponent(l.cmdW)
	hints := components.NewHintsComponent(l.hintsW, l.hintsH)
	toast := components.NewToastComponent()

	// comps: 0=history, 1=status (display-only), 2=command, 3=hints (display-only)
	m := model{
		history:          history,
		status:           status,
		command:          command,
		hints:            hints,
		toast:            toast,
		voiceSettings:    components.NewVoiceSettingsScreen(termW, termH),
		comps:            []components.Component{history, status, command, hints},
		focusable:        []int{0, 2},
		focusedComponent: 1, // index into focusable → comps[2] = command
		termW:            termW,
		termH:            termH,
	}
	m.comps[m.focusable[m.focusedComponent]].SetFocused(true)
	m.hints.SetFocusedComp(m.focusable[m.focusedComponent])
	return m
}

func (m *model) relayout(termW, termH int) {
	m.termW, m.termH = termW, termH
	l := computeLayout(termW, termH)
	m.history.SetSize(l.histW, l.histH)
	m.hints.SetSize(l.hintsW, l.hintsH)
	m.status.SetSize(l.statW, l.statH)
	m.command.SetWidth(l.cmdW)
	m.voiceSettings.SetSize(termW, termH)
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

const pingInterval = 10 * time.Second

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
	cmds = append(cmds, waitNotification(), waitEcho(), waitRoomMessages(), waitDM(), waitPingTick(), doPing())
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
		}
		return m, nil
	case components.StopAudioMsg:
		if msg.Track < 0 {
			for i := range m.audioTracks {
				if m.audioTracks[i] != nil {
					m.audioTracks[i]()
					m.audioTracks[i] = nil
				}
			}
		} else if msg.Track <= 2 {
			if m.audioTracks[msg.Track] != nil {
				m.audioTracks[msg.Track]()
				m.audioTracks[msg.Track] = nil
			}
		}
		return m, nil
	case components.PlayAudioMsg:
		track := m.resolveTrack(msg.Track)
		url := msg.URL
		if m.audioTracks[track] != nil {
			m.audioTracks[track]() // cancel existing on this track
		}
		ctx, cancel := context.WithCancel(context.Background())
		m.audioTracks[track] = cancel
		roomID := voice.ActiveRoomID()
		if roomID == "" {
			// Not in a voice call — play locally only, no broker RPC.
			roomID = voice.SelfRoomID
		} else {
			if err := commands.PlayAudioDirect(url, track); err != nil {
				m.audioTracks[track] = nil
				cancel()
				errMsg := err.Error()
				return m, func() tea.Msg {
					return components.ShowToastMsg{Text: ".play: " + errMsg}
				}
			}
		}
		return m, components.PlayAudioFromURL(ctx, roomID, url, track)
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
				cmds = append(cmds,
					func() tea.Msg { return components.ShowToastMsg{Text: text} },
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
		var cmds []tea.Cmd
		for _, c := range m.comps {
			if cmd := c.Update(msg); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		cmds = append(cmds, loadRoomHistory(roomID))
		return m, tea.Batch(cmds...)
	case types.RoomDeselectedMsg:
		go commands.SendRoomLeave(msg.RoomID)
		return m, nil
	case components.ShowVoiceSettingsMsg:
		m.showVoiceSettings = true
		m.voiceSettings.Reset()
		return m, nil
	case components.HideVoiceSettingsMsg:
		m.showVoiceSettings = false
		return m, nil
	case tea.KeyPressMsg:
		if m.showVoiceSettings {
			if msg.String() == "ctrl+c" {
				return m, tea.Quit
			}
			return m, m.voiceSettings.Update(msg)
		}
		switch msg.String() {
		case "ctrl+c":
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
		case "left", "right":
			// Always route left/right to history for tab switching regardless of focus.
			return m, m.history.Update(msg)
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

func (m model) View() tea.View {
	if m.showVoiceSettings {
		v := tea.NewView(styles.AppStyle.Render(m.voiceSettings.Render()))
		v.AltScreen = true
		return v
	}

	rightCol := lipgloss.JoinHorizontal(lipgloss.Top, m.hints.Render(), m.status.Render())
	topRow := lipgloss.JoinHorizontal(lipgloss.Top, m.history.Render(), rightCol)
	s := topRow + "\n" + m.command.Render()

	helpText := styles.HelpStyle.Render("  tab  switch focus  •  /  focus cmd  •  ctrl+c  quit")
	if m.toast.Visible() {
		toastStr := m.toast.Render()
		usableW := m.termW - appPaddingW
		toastW := lipgloss.Width(toastStr)
		padW := usableW - lipgloss.Width(helpText) - toastW
		if padW < 1 {
			padW = 1
		}
		pad := lipgloss.NewStyle().Width(padW).Render("")
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
		return
	}
}
