// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package main

import (
	"strings"

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

type mockMessage struct {
	kind      msgKind
	timestamp string
	from      string
	body      string
}

// ── Mock data ─────────────────────────────────────────────────────────────────

var mockChatMessages = []mockMessage{
	{msgSystem, "09:01", "", "— joined room: general —"},
	{msgChat, "09:02", "alice", "hey, you around?"},
	{msgSelf, "09:02", "you", "yeah what's up"},
	{msgChat, "09:03", "alice", "want to hop on a call?"},
	{msgSelf, "09:03", "you", "sure give me 2 mins"},
	{msgSystem, "09:04", "", "— alice joined voice —"},
	{msgSystem, "09:04", "", "— you joined voice —"},
	{msgChat, "09:05", "alice", "can you hear me?"},
	{msgSelf, "09:05", "you", "yeah loud and clear"},
	{msgSelf, "09:08", "you", ".play https://archive.org/example.mp3"},
	{msgSystem, "09:08", "", "— track 0 started (broadcasting) —"},
	{msgChat, "09:09", "alice", "nice tune"},
}

var mockDMConvos = []struct {
	with     string
	preview  string
	unread   int
	messages []mockMessage
}{
	{
		"alice", "nice tune", 0,
		[]mockMessage{
			{msgSelf, "09:01", "you", "hey"},
			{msgDM, "09:01", "alice", "hey! what's up"},
			{msgDM, "09:05", "alice", "nice tune"},
		},
	},
	{
		"bob", "see you in the room", 2,
		[]mockMessage{
			{msgDM, "08:55", "bob", "yo"},
			{msgDM, "08:56", "bob", "see you in the room"},
		},
	},
}

var mockRooms = []struct {
	name    string
	members int
	joined  bool
	owner   bool
}{
	{"general", 3, true, true},
	{"dev", 1, false, false},
	{"music", 2, false, false},
}

var mockMembers = []struct {
	name   string
	online bool
}{
	{"alice", true},
	{"bob", true},
	{"carol", false},
}

var mockLogs = []string{
	"[INFO] connected to broker nito.example.com",
	"[INFO] joined room: general",
	"[INFO] voice: peer connection created",
	"[INFO] audio_player: m3u resolved 1 track(s)",
	"[INFO] track 0 started: I♥CHILLHOP",
}

var mockInvites = []struct {
	from string
	room string
}{
	{"bob", "dev"},
}

// ── Message renderer ──────────────────────────────────────────────────────────

func renderMessage(m mockMessage) fyne.CanvasObject {
	switch m.kind {
	case msgSystem:
		return container.NewPadded(monoTxt("  "+m.body, colMuted))
	case msgSelf:
		return container.NewPadded(container.NewHBox(
			txt(m.timestamp+" ", colDim, 12, false, true),
			txt("you  ", colCyan, 13, false, true),
			monoTxt(m.body, colText),
		))
	case msgDM:
		return container.NewPadded(container.NewHBox(
			txt(m.timestamp+" ", colDim, 12, false, true),
			txt(m.from+"  ", colLavender, 13, false, true),
			monoTxt(m.body, colText),
		))
	default:
		return container.NewPadded(container.NewHBox(
			txt(m.timestamp+" ", colDim, 12, false, true),
			txt(m.from+"  ", colLavender, 13, false, true),
			monoTxt(m.body, colText),
		))
	}
}

// ── Room sidebar ──────────────────────────────────────────────────────────────

func buildRoomSidebar(w fyne.Window, onRoomTap func(), onMemberTap func(string)) fyne.CanvasObject {
	var rows []fyne.CanvasObject

	// ── Rooms ──
	rows = append(rows, txt("my rooms", colDimMid, 11, false, true))
	for _, r := range mockRooms {
		if !r.owner {
			continue
		}
		icon := monoTxt("◆ ", colAccent)
		name := txt(r.name, colText, 13, true, true)
		row := container.NewPadded(container.NewHBox(icon, name))
		rows = append(rows, NewHoverRow(row, onRoomTap))
	}

	// + create room — pinned after owned rooms
	createBtn := widget.NewButton("+ Create", nil)
	createBtn.Importance = widget.LowImportance
	createBtn.OnTapped = func() {
		nameEntry := widget.NewEntry()
		nameEntry.SetPlaceHolder("room name")
		confirmBtn := widget.NewButton("Create", nil)
		cancelBtn := widget.NewButton("Cancel", nil)
		cancelBtn.Importance = widget.LowImportance
		var pop *widget.PopUp
		confirmBtn.OnTapped = func() {
			name := strings.TrimSpace(nameEntry.Text)
			if pop != nil {
				pop.Hide()
			}
			if name != "" {
				showToast(w, "room created: "+name, toastSuccess)
			}
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
	rows = append(rows, createBtn, vspace(4))

	// Invite button — shown when in a room
	inviteBtn := widget.NewButton("+ Invite", nil)
	inviteBtn.Importance = widget.LowImportance
	inviteBtn.OnTapped = func() {
		userEntry := widget.NewEntry()
		userEntry.SetPlaceHolder("username")
		confirmBtn := widget.NewButton("Invite", nil)
		cancelBtn := widget.NewButton("Cancel", nil)
		cancelBtn.Importance = widget.LowImportance
		var pop *widget.PopUp
		confirmBtn.OnTapped = func() {
			username := strings.TrimSpace(userEntry.Text)
			if pop != nil {
				pop.Hide()
			}
			if username != "" {
				showToast(w, "invited "+username, toastInfo)
			}
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
	rows = append(rows, inviteBtn)

	rows = append(rows, vspace(4), txt("joined rooms", colDimMid, 11, false, true))
	for _, r := range mockRooms {
		if r.owner || !r.joined {
			continue
		}
		icon := monoTxt("• ", colText)
		name := monoTxt(r.name, colText)
		row := container.NewPadded(container.NewHBox(icon, name))
		rows = append(rows, NewHoverRow(row, onRoomTap))
	}
	for _, r := range mockRooms {
		if r.joined || r.owner {
			continue
		}
		icon := monoTxt("  ", colText)
		name := monoTxt(r.name, colText)
		row := container.NewPadded(container.NewHBox(icon, name))
		rows = append(rows, NewHoverRow(row, onRoomTap))
	}

	// ── Members ──
	rows = append(rows, vspace(8), hline(), vspace(4))
	for _, m := range mockMembers {
		memberName := m.name
		var dot *canvas.Text
		if m.online {
			dot = txt("● ", colGreen, 12, false, true)
		} else {
			dot = txt("○ ", colDim, 12, false, true)
		}
		nameCol := colText
		if !m.online {
			nameCol = colDim
		}
		nameLabel := monoTxt(memberName, nameCol)
		row := container.NewPadded(container.NewHBox(dot, nameLabel))
		if m.online {
			rows = append(rows, NewHoverRow(row, func() {
				if onMemberTap != nil {
					onMemberTap(memberName)
				}
			}))
		} else {
			rows = append(rows, row)
		}
	}

	scrollBody := container.NewVScroll(container.NewVBox(rows...))

	// Voice settings pinned to footer
	voiceBtn := widget.NewButton("Voice Settings", func() { showVoiceSettingsPopup(w) })
	voiceBtn.Importance = widget.LowImportance
	footer := container.NewVBox(hline(), voiceBtn, vspace(4))

	content := container.NewBorder(nil, footer, nil, nil, scrollBody)
	bg := canvas.NewRectangle(colSurface2)
	bg.CornerRadius = 4
	return container.NewStack(bg, container.NewPadded(content))
}

// ── Chat message area ─────────────────────────────────────────────────────────

func buildChatArea(messages []mockMessage, roomName string) fyne.CanvasObject {
	header := container.NewHBox(
		sectionBadge("# " + roomName),
	)

	list := widget.NewList(
		func() int { return len(messages) },
		func() fyne.CanvasObject {
			return container.NewPadded(widget.NewLabel(""))
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			obj.(*fyne.Container).Objects[0] = renderMessage(messages[id])
			obj.Refresh()
		},
	)

	return container.NewBorder(
		container.NewVBox(header, vspace(2), hline()),
		nil, nil, nil,
		list,
	)
}

// ── DM view (inline, replaces chat area) ─────────────────────────────────────

func buildDMView(messages []mockMessage, username string, onBack func()) fyne.CanvasObject {
	backLabel := NewTappable(
		container.NewHBox(
			txt("← ", colAccent, 13, false, true),
			txt("#general", colDimMid, 12, false, true),
		),
		onBack,
	)
	header := container.NewVBox(
		container.NewHBox(backLabel, vspace(8), sectionBadge("@ "+username)),
		vspace(2), hline(),
	)

	list := widget.NewList(
		func() int { return len(messages) },
		func() fyne.CanvasObject { return container.NewPadded(widget.NewLabel("")) },
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			obj.(*fyne.Container).Objects[0] = renderMessage(messages[id])
			obj.Refresh()
		},
	)

	return container.NewBorder(header, nil, nil, nil, list)
}

// ── Invites tab (used by status panel) ───────────────────────────────────────

func buildInvitesTab(w fyne.Window) fyne.CanvasObject {
	if len(mockInvites) == 0 {
		return container.NewBorder(
			container.NewVBox(vspace(4), hline()),
			nil, nil, nil,
			container.NewCenter(dimTxt("no pending invites")),
		)
	}
	var rows []fyne.CanvasObject
	for _, inv := range mockInvites {
		room := inv.room
		acceptBtn := widget.NewButton("Accept", func() {
			showToast(w, "joined room: "+room, toastSuccess)
		})
		acceptBtn.Importance = widget.HighImportance
		declineBtn := widget.NewButton("Decline", func() {
			showToast(w, "invite declined", toastWarn)
		})
		declineBtn.Importance = widget.LowImportance
		info := container.NewHBox(
			monoTxt(inv.from+" invited you to #"+inv.room, colText),
		)
		row := container.NewPadded(
			container.NewBorder(nil, nil, info, container.NewHBox(acceptBtn, declineBtn), nil),
		)
		rows = append(rows, row, hline())
	}
	return container.NewBorder(
		container.NewVBox(vspace(4), hline()),
		nil, nil, nil,
		container.NewVScroll(container.NewVBox(rows...)),
	)
}

// ── Notif tab (used by status panel) ─────────────────────────────────────────

func buildNotifTab() fyne.CanvasObject {
	rows := []fyne.CanvasObject{
		container.NewPadded(monoTxt("  bob invited you to room: dev", colText)),
		hline(),
		container.NewPadded(monoTxt("  alice sent you a DM", colText)),
		hline(),
	}
	return container.NewBorder(
		container.NewVBox(vspace(4), hline()),
		nil, nil, nil,
		container.NewVScroll(container.NewVBox(rows...)),
	)
}

// ── Logs tab (used by status panel) ──────────────────────────────────────────

func buildLogsTab() fyne.CanvasObject {
	text := ""
	for i, line := range mockLogs {
		if i > 0 {
			text += "\n"
		}
		text += line
	}
	entry := widget.NewMultiLineEntry()
	entry.SetText(text)
	entry.Wrapping = fyne.TextWrapOff
	entry.TextStyle = fyne.TextStyle{Monospace: true}
	// Read-only: user can select+copy but not edit
	entry.OnChanged = func(string) { entry.SetText(text) }
	return entry
}

// ── ChatPanel (left panel — sidebar + chat area, no tabs) ─────────────────────

type ChatPanel struct {
	widget.BaseWidget
	content  *fyne.Container
	OnDMOpen func(username string)
}

func NewChatPanel(w fyne.Window) *ChatPanel {
	cp := &ChatPanel{}

	roomArea := buildChatArea(mockChatMessages, "general")

	dmViews := make(map[string]fyne.CanvasObject)
	for _, convo := range mockDMConvos {
		name := convo.with
		msgs := convo.messages
		view := buildDMView(msgs, name, func() {
			roomArea.Show()
			for _, v := range dmViews {
				v.Hide()
			}
			if cp.OnDMOpen != nil {
				cp.OnDMOpen("")
			}
		})
		view.Hide()
		dmViews[name] = view
	}

	rightObjs := []fyne.CanvasObject{roomArea}
	for _, v := range dmViews {
		rightObjs = append(rightObjs, v)
	}
	chatRight := container.NewStack(rightObjs...)

	backToRoom := func() {
		roomArea.Show()
		for _, v := range dmViews {
			v.Hide()
		}
		if cp.OnDMOpen != nil {
			cp.OnDMOpen("")
		}
	}

	roomSidebar := buildRoomSidebar(w, backToRoom, func(memberName string) {
		roomArea.Hide()
		for n, v := range dmViews {
			if n == memberName {
				v.Show()
			} else {
				v.Hide()
			}
		}
		if cp.OnDMOpen != nil {
			cp.OnDMOpen(memberName)
		}
	})

	chatSplit := container.NewHSplit(roomSidebar, chatRight)
	chatSplit.SetOffset(0.25)

	cp.content = container.NewStack(chatSplit)
	cp.ExtendBaseWidget(cp)
	return cp
}

func (cp *ChatPanel) CreateRenderer() fyne.WidgetRenderer {
	_, panel := panelStack(false, cp.content)
	return widget.NewSimpleRenderer(panel)
}
