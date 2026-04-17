// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package main

import (
	"image/color"
	"strings"
	"time"

	apitypes "github.com/srschreiber/nito-client/shared/api_types"
	"github.com/srschreiber/nito-client/shellapp/commands"
	"github.com/srschreiber/nito-client/shellapp/connection"
	"github.com/srschreiber/nito-client/shellapp/voice"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// ── Message model ─────────────────────────────────────────────────────────────

type msgKind int

const (
	msgChat msgKind = iota
	msgSystem
	msgSelf
	msgDM
)

type chatMessage struct {
	kind      msgKind
	timestamp string // "15:04"
	date      string // "2006-01-02" — used for day separator injection
	from      string
	body      string
}

// ── Message renderer ──────────────────────────────────────────────────────────

func renderMessage(m chatMessage) fyne.CanvasObject {
	var row fyne.CanvasObject
	msgBody := func(text string) *widget.Label {
		l := widget.NewLabel(text)
		l.Selectable = true
		l.TextStyle = fyne.TextStyle{Monospace: true}
		l.Wrapping = fyne.TextWrapWord
		return l
	}
	msgRow := func(ts, from string, fromCol color.Color, text string) fyne.CanvasObject {
		meta := container.NewHBox(
			txt(ts+" ", colDim, 12, false, true),
			txt(from+"  ", fromCol, 13, false, true),
		)
		return container.NewPadded(container.NewBorder(nil, nil, meta, nil, msgBody(text)))
	}

	switch m.kind {
	case msgSystem:
		return container.NewPadded(monoTxt("  "+m.body, colMuted))
	case msgSelf:
		row = msgRow(m.timestamp, "you", colCyan, m.body)
	case msgDM:
		row = msgRow(m.timestamp, m.from, colLavender, m.body)
	default:
		row = msgRow(m.timestamp, m.from, colLavender, m.body)
	}

	// Append embeds for YouTube and audio URLs.
	ytURLs := findYouTubeURLs(m.body)
	audioURLs := findAudioURLs(m.body)
	if len(ytURLs) == 0 && len(audioURLs) == 0 {
		return row
	}
	parts := []fyne.CanvasObject{row}
	for _, u := range ytURLs {
		if embed := buildYouTubeEmbed(u); embed != nil {
			parts = append(parts, embed)
		}
	}
	for _, u := range audioURLs {
		if embed := buildAudioEmbed(u); embed != nil {
			parts = append(parts, embed)
		}
	}
	return container.NewVBox(parts...)
}

// renderDaySep renders a muted centred date divider for the given "2006-01-02" date.
func renderDaySep(date string) fyne.CanvasObject {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return nil
	}
	today := time.Now().Format("2006-01-02")
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	var label string
	switch date {
	case today:
		label = "— today —"
	case yesterday:
		label = "— yesterday —"
	default:
		label = "— " + t.Format("Monday, January 2") + " —"
	}
	return container.NewPadded(monoTxt("  "+label, colMuted))
}

// ── Invites / Notif / Logs tabs (rendered by StatusPanel) ────────────────────

func buildNotifTab() fyne.CanvasObject {
	rows := []fyne.CanvasObject{
		container.NewPadded(dimTxt("notifications will appear here")),
	}
	return container.NewBorder(
		container.NewVBox(vspace(4), hline()),
		nil, nil, nil,
		container.NewVScroll(container.NewVBox(rows...)),
	)
}

// ── ChatPanel ─────────────────────────────────────────────────────────────────

type ChatPanel struct {
	widget.BaseWidget
	w fyne.Window

	// Room area
	content      *fyne.Container
	msgBox       *fyne.Container   // VBox of room messages
	msgScroll    *container.Scroll // VScroll wrapping msgBox
	roomHeader   *canvas.Text      // "# roomname"
	roomListBox  *fyne.Container   // VBox of room sidebar rows
	memberBox    *fyne.Container   // VBox of member rows
	chatRight    *fyne.Container   // Stack: roomArea + DM views
	inputWrapper *fyne.Container   // Bottom input bar placeholder

	// Current room
	messages        []chatMessage
	currentRoomID   string
	currentRoomName string
	roomArea        fyne.CanvasObject

	// DM views
	dmViews    map[string]fyne.CanvasObject
	dmMsgBoxes map[string]*fyne.Container
	dmMessages map[string][]chatMessage

	OnDMOpen func(username string)

	voiceBtn *nitoBtn
}

func NewChatPanel(w fyne.Window) *ChatPanel {
	cp := &ChatPanel{
		w:          w,
		dmViews:    make(map[string]fyne.CanvasObject),
		dmMsgBoxes: make(map[string]*fyne.Container),
		dmMessages: make(map[string][]chatMessage),
	}

	// ── Room chat area ────────────────────────────────────────────────────────
	cp.roomHeader = sectionBadge("# —")
	cp.msgBox = container.NewVBox()
	cp.msgScroll = container.NewVScroll(cp.msgBox)
	header := container.NewVBox(
		container.NewHBox(cp.roomHeader),
		vspace(2), hline(),
	)
	cp.roomArea = container.NewBorder(header, nil, nil, nil, cp.msgScroll)

	// ── Stack: room area + (DM views added dynamically) ──────────────────────
	cp.chatRight = container.NewStack(cp.roomArea)

	// ── Input bar placeholder (filled by SetInputBar) ─────────────────────────
	cp.inputWrapper = container.NewVBox()

	// ── Room sidebar ──────────────────────────────────────────────────────────
	cp.roomListBox = container.NewVBox(dimTxt("loading rooms…"))
	cp.memberBox = container.NewVBox()

	sidebar := cp.buildSidebar()

	chatArea := container.NewBorder(nil, cp.inputWrapper, nil, nil, cp.chatRight)
	split := container.NewHSplit(sidebar, chatArea)
	split.SetOffset(0.18)

	cp.content = container.NewStack(split)
	cp.ExtendBaseWidget(cp)
	return cp
}

// buildSidebar constructs the sidebar once; room/member rows are updated via
// SetRooms / SetMembers. Buttons wire to real backend operations.
func (cp *ChatPanel) buildSidebar() fyne.CanvasObject {
	w := cp.w

	// ── Create room popup ─────────────────────────────────────────────────────
	createBtn := newBtn("+ Create", nil)
	createBtn.Importance = widget.LowImportance
	createBtn.OnTapped = func() {
		nameEntry := widget.NewEntry()
		nameEntry.SetPlaceHolder("room name")
		confirmBtn := newBtn("Create", nil)
		cancelBtn := newBtn("Cancel", nil)
		cancelBtn.Importance = widget.LowImportance
		var pop *widget.PopUp
		confirmBtn.OnTapped = func() {
			name := strings.TrimSpace(nameEntry.Text)
			if pop != nil {
				pop.Hide()
			}
			if name == "" {
				return
			}
			go func() {
				_, err := commands.CreateRoomDirect(name)
				fyne.Do(func() {
					if err != nil {
						nitoLog("create room failed: " + err.Error())
						showToast(w, "create room: "+err.Error(), toastError)
						return
					}
					nitoLog("created room: " + name)
					showToast(w, "room created: "+name, toastSuccess)
					go func() {
						rooms, err := connection.ListRooms()
						if err == nil {
							fyne.Do(func() { cp.SetRooms(rooms) })
						}
					}()
				})
			}()
		}
		cancelBtn.OnTapped = func() {
			if pop != nil {
				pop.Hide()
			}
		}
		body := container.NewVBox(
			monoTxt("room name", colDimMid), nameEntry, vspace(6),
			container.NewHBox(confirmBtn, cancelBtn),
		)
		pop = showNitoPopup("CREATE ROOM", body, w)
		w.Canvas().Focus(nameEntry)
	}

	// ── Invite popup ──────────────────────────────────────────────────────────
	inviteBtn := newBtn("+ Invite", nil)
	inviteBtn.Importance = widget.LowImportance
	inviteBtn.OnTapped = func() {
		if cp.currentRoomID == "" {
			showToast(w, "select a room first", toastWarn)
			return
		}
		userEntry := widget.NewEntry()
		userEntry.SetPlaceHolder("username")
		confirmBtn := newBtn("Invite", nil)
		cancelBtn := newBtn("Cancel", nil)
		cancelBtn.Importance = widget.LowImportance
		var pop *widget.PopUp
		confirmBtn.OnTapped = func() {
			username := strings.TrimSpace(userEntry.Text)
			if pop != nil {
				pop.Hide()
			}
			if username == "" {
				return
			}
			go func() {
				_, err := commands.InviteUserDirect(username)
				fyne.Do(func() {
					if err != nil {
						nitoLog("invite failed: " + err.Error())
						showToast(w, "invite: "+err.Error(), toastError)
						return
					}
					nitoLog("invited " + username + " to " + cp.currentRoomName)
					showToast(w, "invited "+username, toastSuccess)
				})
			}()
		}
		cancelBtn.OnTapped = func() {
			if pop != nil {
				pop.Hide()
			}
		}
		body := container.NewVBox(
			monoTxt("username", colDimMid), userEntry, vspace(6),
			container.NewHBox(confirmBtn, cancelBtn),
		)
		pop = showNitoPopup("INVITE TO ROOM", body, w)
		w.Canvas().Focus(userEntry)
	}

	// ── Voice join/leave button (state updated by TickVoice every 50 ms) ──────
	cp.voiceBtn = newBtn("Join Voice", func() {
		if voice.IsActive() {
			go func() {
				if err := commands.VoiceLeaveDirect(); err != nil {
					fyne.Do(func() { showToast(w, "voice: "+err.Error(), toastError) })
				} else {
					nitoLog("left voice chat")
				}
			}()
		} else if !voice.IsConnecting() {
			go func() {
				if err := commands.VoiceJoinDirect(); err != nil {
					fyne.Do(func() { showToast(w, "voice: "+err.Error(), toastError) })
				} else {
					nitoLog("joined voice chat")
				}
			}()
		}
	})
	cp.voiceBtn.Importance = widget.LowImportance

	settingsBtn := newBtn("⚙ Voice Settings", func() { showVoiceSettingsPopup(w) })
	settingsBtn.Importance = widget.LowImportance
	footer := container.NewVBox(hline(), cp.voiceBtn, settingsBtn, vspace(4))

	// ── Room list section (dynamic) ───────────────────────────────────────────
	roomSection := container.NewVBox(
		txt("my rooms", colDimMid, 11, false, true),
		cp.roomListBox,
		vspace(4),
		createBtn,
		inviteBtn,
	)

	// ── Member list section (dynamic) ─────────────────────────────────────────
	memberSection := container.NewVBox(
		vspace(8), hline(), vspace(4),
		cp.memberBox,
	)

	scrollBody := container.NewVScroll(container.NewVBox(roomSection, memberSection))
	bg := canvas.NewRectangle(colSurface2)
	bg.CornerRadius = 4
	return container.NewStack(bg, container.NewPadded(
		container.NewBorder(nil, footer, nil, nil, scrollBody),
	))
}

// ── Dynamic update methods ────────────────────────────────────────────────────

// SetRooms rebuilds the room list rows. Must be called on the Fyne thread.
func (cp *ChatPanel) SetRooms(rooms []apitypes.RoomEntry) {
	var rows []fyne.CanvasObject

	var owned, joined []apitypes.RoomEntry
	for _, r := range rooms {
		if r.IsOwner {
			owned = append(owned, r)
		} else {
			joined = append(joined, r)
		}
	}

	for _, r := range owned {
		r := r
		icon := monoTxt("◆ ", colAccent)
		name := truncLabel(r.Name, colText, true)
		row := container.NewPadded(container.NewBorder(nil, nil, icon, nil, name))
		rows = append(rows, NewHoverRow(row, func() { cp.selectRoom(r.ID, r.Name) }))
	}

	if len(joined) > 0 {
		rows = append(rows, vspace(4), txt("joined rooms", colDimMid, 11, false, true))
		for _, r := range joined {
			r := r
			icon := monoTxt("• ", colText)
			name := truncLabel(r.Name, colText, false)
			row := container.NewPadded(container.NewBorder(nil, nil, icon, nil, name))
			rows = append(rows, NewHoverRow(row, func() { cp.selectRoom(r.ID, r.Name) }))
		}
	}

	if len(rows) == 0 {
		rows = append(rows, dimTxt("no rooms yet"))
	}

	cp.roomListBox.Objects = rows
	cp.roomListBox.Refresh()
}

// SetMembers rebuilds the member rows. Must be called on the Fyne thread.
func (cp *ChatPanel) SetMembers(members []apitypes.RoomMemberEntry) {
	myID := connection.GetSessionUserID()
	var rows []fyne.CanvasObject
	for _, m := range members {
		m := m
		var dot *canvas.Text
		if m.Online {
			dot = txt("● ", colGreen, 12, false, true)
		} else {
			dot = txt("○ ", colDim, 12, false, true)
		}
		nameCol := colText
		if !m.Online {
			nameCol = colDim
		}
		nameLabel := truncLabel(m.Username, nameCol, false)
		row := container.NewPadded(container.NewBorder(nil, nil, dot, nil, nameLabel))
		if m.Online && m.Username != myID {
			username := m.Username
			rows = append(rows, NewHoverRow(row, func() { cp.openDM(username) }))
		} else {
			rows = append(rows, row)
		}
	}
	cp.memberBox.Objects = rows
	cp.memberBox.Refresh()
}

// selectRoom is called when the user clicks a room row.
func (cp *ChatPanel) selectRoom(roomID, roomName string) {
	go func() {
		if err := connection.SetSessionRoom(roomID); err != nil {
			nitoLog("join room failed: " + err.Error())
			fyne.Do(func() { showToast(cp.w, "room: "+err.Error(), toastError) })
			return
		}
		nitoLog("joined room: " + roomName)

		members, _ := connection.ListRoomMembers(roomID)

		// Load historical messages
		var histMsgs []chatMessage
		resp, err := connection.GetRoomMessages(roomID, 50)
		if err == nil {
			histMsgs = decryptHistoricalMessages(resp)
		}

		joinMsg := chatMessage{
			kind:      msgSystem,
			timestamp: time.Now().Format("15:04"),
			date:      time.Now().Format("2006-01-02"),
			body:      "— joined room: " + roomName + " —",
		}
		allMsgs := append(histMsgs, joinMsg)

		fyne.Do(func() {
			cp.roomHeader.Text = "# " + roomName
			cp.roomHeader.Refresh()
			cp.currentRoomID = roomID
			cp.currentRoomName = roomName
			cp.messages = allMsgs
			cp.rebuildMsgBox()
			cp.showRoomArea()
			if members != nil {
				cp.SetMembers(members)
			}
		})
	}()
}

// AppendMessage adds a message to the current room view. Fyne thread.
func (cp *ChatPanel) AppendMessage(m chatMessage) {
	if m.date != "" {
		n := len(cp.messages)
		if n == 0 || cp.messages[n-1].date != m.date {
			if sep := renderDaySep(m.date); sep != nil {
				cp.msgBox.Objects = append(cp.msgBox.Objects, sep)
			}
		}
	}
	cp.messages = append(cp.messages, m)
	cp.msgBox.Objects = append(cp.msgBox.Objects, renderMessage(m))
	cp.msgBox.Refresh()
	cp.msgScroll.ScrollToBottom()
}

func (cp *ChatPanel) rebuildMsgBox() {
	var objs []fyne.CanvasObject
	lastDate := ""
	for _, m := range cp.messages {
		if m.date != "" && m.date != lastDate {
			if sep := renderDaySep(m.date); sep != nil {
				objs = append(objs, sep)
			}
			lastDate = m.date
		}
		objs = append(objs, renderMessage(m))
	}
	cp.msgBox.Objects = objs
	cp.msgBox.Refresh()
	cp.msgScroll.ScrollToBottom()
}

// AppendDMMessage adds or creates a DM view for fromUser. Fyne thread.
func (cp *ChatPanel) AppendDMMessage(fromUser string, m chatMessage) {
	msgs := cp.dmMessages[fromUser]
	if m.date != "" {
		n := len(msgs)
		if n == 0 || msgs[n-1].date != m.date {
			cp.dmMessages[fromUser] = append(msgs, m)
			if _, ok := cp.dmViews[fromUser]; !ok {
				cp.createDMView(fromUser)
			}
			box := cp.dmMsgBoxes[fromUser]
			if sep := renderDaySep(m.date); sep != nil {
				box.Objects = append(box.Objects, sep)
			}
			box.Objects = append(box.Objects, renderMessage(m))
			box.Refresh()
			return
		}
	}
	cp.dmMessages[fromUser] = append(msgs, m)
	if _, ok := cp.dmViews[fromUser]; !ok {
		cp.createDMView(fromUser)
	}
	box := cp.dmMsgBoxes[fromUser]
	box.Objects = append(box.Objects, renderMessage(m))
	box.Refresh()
}

func (cp *ChatPanel) createDMView(username string) {
	msgBox := container.NewVBox()
	// Populate with any already-received messages, injecting day separators.
	lastDate := ""
	for _, m := range cp.dmMessages[username] {
		if m.date != "" && m.date != lastDate {
			if sep := renderDaySep(m.date); sep != nil {
				msgBox.Objects = append(msgBox.Objects, sep)
			}
			lastDate = m.date
		}
		msgBox.Objects = append(msgBox.Objects, renderMessage(m))
	}
	cp.dmMsgBoxes[username] = msgBox

	view := cp.buildDMView(msgBox, username)
	view.Hide()
	cp.dmViews[username] = view
	cp.chatRight.Objects = append(cp.chatRight.Objects, view)
	cp.chatRight.Refresh()
}

func (cp *ChatPanel) buildDMView(msgBox *fyne.Container, username string) fyne.CanvasObject {
	backLabel := NewTappable(
		container.NewHBox(
			txt("← ", colAccent, 13, false, true),
			txt("#"+cp.currentRoomName, colDimMid, 12, false, true),
		),
		func() { cp.showRoomArea() },
	)
	header := container.NewVBox(
		container.NewHBox(backLabel, vspace(8), sectionBadge("@ "+username)),
		vspace(2), hline(),
	)
	return container.NewBorder(header, nil, nil, nil,
		container.NewVScroll(msgBox))
}

func (cp *ChatPanel) openDM(username string) {
	if _, ok := cp.dmViews[username]; !ok {
		cp.createDMView(username)
	}
	cp.roomArea.Hide()
	for u, v := range cp.dmViews {
		if u == username {
			v.Show()
		} else {
			v.Hide()
		}
	}
	if cp.OnDMOpen != nil {
		cp.OnDMOpen(username)
	}
}

func (cp *ChatPanel) showRoomArea() {
	cp.roomArea.Show()
	for _, v := range cp.dmViews {
		v.Hide()
	}
	if cp.OnDMOpen != nil {
		cp.OnDMOpen("")
	}
}

// SetInputBar places bar at the bottom of the conversation area.
// Call once after the ChatPanel is created.
func (cp *ChatPanel) SetInputBar(bar fyne.CanvasObject) {
	cp.inputWrapper.Objects = []fyne.CanvasObject{hline(), container.NewPadded(bar)}
	cp.inputWrapper.Refresh()
}

// Refresh rebuilds the message box from cp.messages (called after external appends).
func (cp *ChatPanel) Refresh() {
	cp.rebuildMsgBox()
	cp.BaseWidget.Refresh()
}

func (cp *ChatPanel) CreateRenderer() fyne.WidgetRenderer {
	_, panel := panelStack(false, cp.content)
	return widget.NewSimpleRenderer(panel)
}

// TickVoice updates the join/leave button to reflect current voice state.
// Called every ~50 ms from the StatusPanel animation ticker on the Fyne thread.
func (cp *ChatPanel) TickVoice() {
	if cp.voiceBtn == nil {
		return
	}
	active := voice.IsActive()
	connecting := voice.IsConnecting()
	inRoom := cp.currentRoomID != ""

	var label string
	var imp widget.Importance
	switch {
	case connecting:
		label = "Connecting…"
		imp = widget.LowImportance
	case active:
		label = "Leave Voice"
		imp = widget.DangerImportance
	default:
		label = "Join Voice"
		imp = widget.LowImportance
	}

	changed := cp.voiceBtn.Text != label || cp.voiceBtn.Importance != imp
	if changed {
		cp.voiceBtn.Text = label
		cp.voiceBtn.Importance = imp
		cp.voiceBtn.Refresh()
	}

	shouldDisable := !inRoom || connecting
	if shouldDisable && !cp.voiceBtn.Disabled() {
		cp.voiceBtn.Disable()
	} else if !shouldDisable && cp.voiceBtn.Disabled() {
		cp.voiceBtn.Enable()
	}
}
