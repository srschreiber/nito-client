// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package main

import (
	"strings"
	"time"

	apitypes "github.com/srschreiber/nito-client/shared/api_types"
	"github.com/srschreiber/nito-client/shellapp/connection"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// ── CircleStopBtn ─────────────────────────────────────────────────────────────

type CircleStopBtn struct {
	widget.BaseWidget
	circ  *canvas.Rectangle
	onTap func()
}

func newCircleStop(onTap func()) *CircleStopBtn {
	b := &CircleStopBtn{onTap: onTap}
	b.circ = canvas.NewRectangle(colAccentDark)
	b.circ.CornerRadius = 9
	b.circ.SetMinSize(fyne.NewSize(18, 18))
	b.ExtendBaseWidget(b)
	return b
}

func (b *CircleStopBtn) CreateRenderer() fyne.WidgetRenderer {
	label := txt("■", colAccent, 9, false, false)
	return widget.NewSimpleRenderer(
		container.NewStack(b.circ, container.NewCenter(label)),
	)
}

func (b *CircleStopBtn) Tapped(_ *fyne.PointEvent) {
	b.circ.FillColor = colAccent
	b.circ.Refresh()
	go func() {
		time.Sleep(120 * time.Millisecond)
		fyne.Do(func() {
			b.circ.FillColor = colAccentDark
			b.circ.Refresh()
		})
		if b.onTap != nil {
			b.onTap()
		}
	}()
}

func (b *CircleStopBtn) TappedSecondary(_ *fyne.PointEvent) {}

func (b *CircleStopBtn) MouseIn(_ *desktop.MouseEvent) {
	b.circ.FillColor = colBorderFocus
	b.circ.Refresh()
}

func (b *CircleStopBtn) MouseMoved(_ *desktop.MouseEvent) {}

func (b *CircleStopBtn) MouseOut() {
	b.circ.FillColor = colAccentDark
	b.circ.Refresh()
}

func (b *CircleStopBtn) Cursor() desktop.Cursor { return desktop.PointerCursor }

// ── Voice settings popup ──────────────────────────────────────────────────────

func fmtFloat(v float64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	whole := int(v)
	frac := int((v-float64(whole))*10 + 0.5)
	s := itoa(whole) + "." + itoa(frac)
	if neg {
		s = "-" + s
	}
	return s
}

func showVoiceSettingsPopup(w fyne.Window) {
	inputSel := widget.NewSelect([]string{"Default Microphone", "USB Headset", "Built-in Mic"}, func(string) {})
	inputSel.SetSelected("Default Microphone")
	outputSel := widget.NewSelect([]string{"Default Speaker", "USB Headset", "Built-in Output"}, func(string) {})
	outputSel.SetSelected("Default Speaker")
	noiseCheck := widget.NewCheck("Noise reduction (RNNoise)", func(bool) {})
	noiseCheck.SetChecked(true)
	aecCheck := widget.NewCheck("Echo cancellation (AEC3)", func(bool) {})
	aecCheck.SetChecked(true)

	pitchSlider := widget.NewSlider(-6, 6)
	pitchSlider.Step = 0.5
	pitchLabel := monoTxt("0.0 st", colText)
	pitchSlider.OnChanged = func(v float64) {
		sign := "+"
		if v < 0 {
			sign = ""
		}
		pitchLabel.Text = sign + fmtFloat(v) + " st"
		pitchLabel.Refresh()
	}

	testBtn := widget.NewButton("Test Voice", func() {
		showToast(w, "testing voice...", toastInfo)
	})
	testBtn.Importance = widget.LowImportance

	minWidth := canvas.NewRectangle(colTransparent)
	minWidth.SetMinSize(fyne.NewSize(340, 0))

	body := container.NewVBox(
		minWidth,
		monoTxt("Input device", colDimMid), inputSel, vspace(4),
		monoTxt("Output device", colDimMid), outputSel, vspace(4),
		hline(), vspace(4),
		noiseCheck, aecCheck, vspace(4),
		hline(), vspace(4),
		monoTxt("Pitch shift", colDimMid),
		container.NewHBox(pitchSlider, pitchLabel),
		vspace(4), hline(), vspace(4),
		testBtn,
	)
	showNitoPopup("VOICE SETTINGS", body, w)
}

// ── STATUS tab ────────────────────────────────────────────────────────────────

// buildStatusTab returns the status tab content and an update function that
// mutates the live labels. The update function must be called on the Fyne thread.
func buildStatusTab() (fyne.CanvasObject, func(connected bool, brokerURL, userID string, latencyMs int64)) {
	dot := txt("○ ", colDim, 13, false, true)
	statusLabel := monoTxt("offline", colDim)
	pingLabel := monoTxt("--", colDim)
	brokerLabel := monoTxt("--", colDim)
	userLabel := monoTxt("--", colDim)

	connSection := container.NewVBox(
		vspace(4),
		container.NewHBox(dot, statusLabel),
		container.NewHBox(monoTxt("  ping    ", colDimMid), pingLabel),
		container.NewHBox(monoTxt("  broker  ", colDimMid), brokerLabel),
		container.NewHBox(monoTxt("  user    ", colDimMid), userLabel),
	)

	statsContent := container.NewVBox(
		container.NewHBox(monoTxt("voice pkt/s  ", colDimMid), monoTxt("--", colDim)),
		container.NewHBox(monoTxt("voice loss   ", colDimMid), monoTxt("--", colDim)),
	)

	accordion := NewCollapseSection("STATS", container.NewVBox(statsContent), false)
	sep := func() fyne.CanvasObject { return container.NewVBox(vspace(8), hline(), vspace(8)) }

	area := container.NewVScroll(container.NewVBox(connSection, sep(), accordion))

	update := func(connected bool, brokerURL, userID string, latencyMs int64) {
		if connected {
			dot.Color = colGreen
			dot.Text = "● "
			statusLabel.Text = "online"
			statusLabel.Color = colText
			pingLabel.Text = itoa(int(latencyMs)) + "ms"
			pingLabel.Color = colText
			host := brokerURL
			for _, pfx := range []string{"https://", "http://", "ws://", "wss://"} {
				host = strings.TrimPrefix(host, pfx)
			}
			brokerLabel.Text = host
			brokerLabel.Color = colText
			userLabel.Text = userID
			userLabel.Color = colText
		} else {
			dot.Color = colDim
			dot.Text = "○ "
			statusLabel.Text = "offline"
			statusLabel.Color = colDim
			pingLabel.Text = "--"
			pingLabel.Color = colDim
			brokerLabel.Text = "--"
			brokerLabel.Color = colDim
			userLabel.Text = "--"
			userLabel.Color = colDim
		}
		dot.Refresh()
		statusLabel.Refresh()
		pingLabel.Refresh()
		brokerLabel.Refresh()
		userLabel.Refresh()
	}

	return area, update
}

// ── Invites tab ───────────────────────────────────────────────────────────────

// buildInvitesTab builds the INVITES tab. Returns the tab canvas object and a
// list box container that can be updated via SetInvites.
func buildInvitesTab(w fyne.Window) (fyne.CanvasObject, *fyne.Container) {
	listBox := container.NewVBox(dimTxt("no pending invites"))
	area := container.NewBorder(
		container.NewVBox(vspace(4), hline()),
		nil, nil, nil,
		container.NewVScroll(listBox),
	)
	return area, listBox
}

// ── StatusPanel ───────────────────────────────────────────────────────────────

type StatusPanel struct {
	widget.BaseWidget
	TabBar        *TabBar
	content       *fyne.Container
	tabs          []fyne.CanvasObject
	setStatus     func(connected bool, brokerURL, userID string, latencyMs int64)
	inviteListBox *fyne.Container
	w             fyne.Window
}

// UpdateStatus refreshes the STATUS tab with live connection data.
// Safe to call from any goroutine.
func (sp *StatusPanel) UpdateStatus(connected bool, brokerURL, userID string, latencyMs int64) {
	if sp.setStatus == nil {
		return
	}
	fyne.Do(func() { sp.setStatus(connected, brokerURL, userID, latencyMs) })
}

// SetInvites rebuilds the INVITES tab rows. Must be called on the Fyne thread.
func (sp *StatusPanel) SetInvites(invites []apitypes.PendingInvite) {
	if sp.inviteListBox == nil {
		return
	}
	var rows []fyne.CanvasObject
	for _, inv := range invites {
		inv := inv
		nameLabel := monoTxt(inv.RoomName, colText)
		acceptBtn := widget.NewButton("Accept", nil)
		acceptBtn.Importance = widget.LowImportance
		acceptBtn.OnTapped = func() {
			go func() {
				err := connection.AcceptInvite(inv.RoomID)
				fyne.Do(func() {
					if err != nil {
						nitoLog("accept invite failed: " + err.Error())
						showToast(sp.w, "accept invite: "+err.Error(), toastError)
						return
					}
					nitoLog("accepted invite, joined " + inv.RoomName)
					showToast(sp.w, "joined "+inv.RoomName, toastSuccess)
					// Remove from list after accepting.
					go func() {
						pending, e := connection.ListPendingInvites()
						if e == nil {
							fyne.Do(func() { sp.SetInvites(pending) })
						}
					}()
				})
			}()
		}
		rows = append(rows, container.NewPadded(container.NewHBox(
			monoTxt("◆ ", colAccent), nameLabel, acceptBtn,
		)))
	}
	if len(rows) == 0 {
		rows = append(rows, dimTxt("no pending invites"))
	}
	sp.inviteListBox.Objects = rows
	sp.inviteListBox.Refresh()
}

func NewStatusPanel(w fyne.Window) *StatusPanel {
	sp := &StatusPanel{w: w}

	statusArea, setStatus := buildStatusTab()
	sp.setStatus = setStatus

	invitesArea, inviteListBox := buildInvitesTab(w)
	sp.inviteListBox = inviteListBox

	tracksArea, tracksTick := buildTracksTab(w)
	eqArea, eqTick := buildEQTab(w)

	tabNames := []string{"STATUS", "TRACKS", "TRACK EQ", "INVITES", "NOTIF", "LOGS"}
	tabs := []fyne.CanvasObject{
		statusArea,
		tracksArea,
		eqArea,
		invitesArea,
		buildNotifTab(),
		buildLogsTab(),
	}

	// Only the active tab lives in the Stack at any time. Fyne's StackLayout
	// calls Resize on every child unconditionally, so putting all tabs in the
	// stack causes every hidden pane (including the EQ raster and tracks
	// accordion) to be laid out on every HSplit drag event.
	content := container.NewStack(tabs[0])
	sp.content = content
	sp.tabs = tabs
	sp.TabBar = NewTabBar(tabNames, 0, func(idx int) {
		content.Objects = []fyne.CanvasObject{tabs[idx]}
		content.Refresh()
	})
	sp.ExtendBaseWidget(sp)

	// 50 ms animation ticker for track meters and EQ spectrum.
	go func() {
		t := time.NewTicker(50 * time.Millisecond)
		defer t.Stop()
		for range t.C {
			fyne.Do(func() {
				tracksTick()
				eqTick()
				tickTrackWatchers()
			})
		}
	}()

	return sp
}

func (sp *StatusPanel) PrevTab() { sp.TabBar.Prev() }
func (sp *StatusPanel) NextTab() { sp.TabBar.Next() }

func (sp *StatusPanel) CreateRenderer() fyne.WidgetRenderer {
	body := container.NewBorder(
		container.NewVBox(sp.TabBar, hline()),
		nil, nil, nil,
		sp.content,
	)
	_, panel := panelStack(false, body)
	return widget.NewSimpleRenderer(panel)
}
