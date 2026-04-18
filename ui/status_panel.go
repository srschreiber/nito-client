// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package main

import (
	"strings"
	"time"

	"github.com/srschreiber/nito-client/engine/commands"
	"github.com/srschreiber/nito-client/engine/connection"
	"github.com/srschreiber/nito-client/engine/voice"
	apitypes "github.com/srschreiber/nito-client/shared/api_types"
	"github.com/srschreiber/nito-client/ui/clientlog"

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
	label *canvas.Text
	onTap func()
}

func newCircleStop(onTap func()) *CircleStopBtn {
	b := &CircleStopBtn{onTap: onTap}
	b.circ = canvas.NewRectangle(liveAccentDark)
	b.circ.CornerRadius = 9
	b.circ.SetMinSize(fyne.NewSize(18, 18))
	b.label = txt("■", liveAccent, 9, false, false)
	b.ExtendBaseWidget(b)
	return b
}

func (b *CircleStopBtn) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(
		container.NewStack(b.circ, container.NewCenter(b.label)),
	)
}

func (b *CircleStopBtn) Refresh() {
	b.circ.FillColor = liveAccentDark
	b.circ.Refresh()
	b.label.Color = liveAccent
	b.label.Refresh()
	b.BaseWidget.Refresh()
}

func (b *CircleStopBtn) Tapped(_ *fyne.PointEvent) {
	b.circ.FillColor = liveAccent
	b.circ.Refresh()
	go func() {
		time.Sleep(120 * time.Millisecond)
		fyne.Do(func() {
			b.circ.FillColor = liveAccentDark
			b.circ.Refresh()
		})
		if b.onTap != nil {
			b.onTap()
		}
	}()
}

func (b *CircleStopBtn) TappedSecondary(_ *fyne.PointEvent) {}

func (b *CircleStopBtn) MouseIn(_ *desktop.MouseEvent) {
	b.circ.FillColor = liveBorderFocus
	b.circ.Refresh()
}

func (b *CircleStopBtn) MouseMoved(_ *desktop.MouseEvent) {}

func (b *CircleStopBtn) MouseOut() {
	b.circ.FillColor = liveAccentDark
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
	// ── Input device ──────────────────────────────────────────────────────────
	devices := voice.ListAudioInputs()
	deviceLabels := make([]string, len(devices))
	deviceIDs := make([]string, len(devices))
	for i, d := range devices {
		deviceLabels[i] = d.Label
		deviceIDs[i] = d.ID
	}

	currentID := voice.SelectedInputDevice()
	currentLabel := "System Default"
	for i, id := range deviceIDs {
		if id == currentID {
			currentLabel = deviceLabels[i]
			break
		}
	}

	deviceStatusLabel := monoTxt("", liveDimMid)

	var inputSel *widget.Select
	inputSel = widget.NewSelect(deviceLabels, func(selected string) {
		for i, lbl := range deviceLabels {
			if lbl == selected {
				if deviceIDs[i] == voice.SelectedInputDevice() {
					return
				}
				voice.SetInputDevice(deviceIDs[i])
				clientlog.Info("selected input device: " + deviceLabels[i])
				inputSel.Disable()
				deviceStatusLabel.Text = "please wait while we switch your input device…"
				deviceStatusLabel.Refresh()
				go func() {
					voice.RestartCapture()
					time.Sleep(4 * time.Second)
					fyne.Do(func() {
						deviceStatusLabel.Text = ""
						deviceStatusLabel.Refresh()
						inputSel.Enable()
					})
				}()
				return
			}
		}
	})
	inputSel.SetSelected(currentLabel)

	// ── Noise / AEC ───────────────────────────────────────────────────────────
	noiseCheck := widget.NewCheck("Noise reduction (RNNoise)", func(b bool) {
		voice.SetDenoiseOutboundEnabled(b)
	})
	noiseCheck.SetChecked(voice.DenoiseOutboundEnabled())

	aecCheck := widget.NewCheck("Echo cancellation (AEC3)", func(b bool) {
		voice.SetAECEnabled(b)
	})
	aecCheck.SetChecked(voice.AECEnabled())

	// ── Test voice toggle ─────────────────────────────────────────────────────
	var testBtn *nitoBtn
	testBtn = newBtn("Test Voice", func() {
		if voice.ActiveRoomID() == voice.SelfRoomID {
			// Currently testing — stop it.
			go func() {
				_ = commands.VoiceLeaveTestAudioDirect()
				fyne.Do(func() {
					testBtn.Text = "Test Voice"
					testBtn.Importance = widget.LowImportance
					testBtn.Enable()
					testBtn.Refresh()
				})
			}()
		} else {
			// Start test — show spinner while connecting.
			fyne.Do(func() {
				testBtn.Text = "| Connecting…"
				testBtn.Importance = widget.LowImportance
				testBtn.Disable()
				testBtn.Refresh()
				inputSel.Disable()
			})
			done := make(chan struct{})
			go func() {
				frames := [4]string{"|", "/", "-", "\\"}
				t := time.NewTicker(120 * time.Millisecond)
				defer t.Stop()
				i := 0
				for {
					select {
					case <-done:
						return
					case <-t.C:
						i++
						frame := frames[i%4]
						fyne.Do(func() {
							testBtn.Text = frame + " Connecting…"
							testBtn.Refresh()
						})
					}
				}
			}()
			go func() {
				err := commands.VoiceTestAudioDirect()
				close(done)
				fyne.Do(func() {
					testBtn.Enable()
					inputSel.Enable()
					if err != nil {
						showToast(w, "test audio: "+err.Error(), toastError)
						testBtn.Text = "Test Voice"
						testBtn.Importance = widget.LowImportance
					} else {
						testBtn.Text = "Stop Test"
						testBtn.Importance = widget.DangerImportance
					}
					testBtn.Refresh()
				})
			}()
		}
	})
	if voice.ActiveRoomID() == voice.SelfRoomID {
		testBtn.Text = "Stop Test"
		testBtn.Importance = widget.DangerImportance
	} else {
		testBtn.Importance = widget.LowImportance
	}

	minWidth := canvas.NewRectangle(colTransparent)
	minWidth.SetMinSize(fyne.NewSize(340, 0))

	body := container.NewVBox(
		minWidth,
		monoTxt("Input device", liveDimMid),
		withPointerCursor(inputSel),
		deviceStatusLabel, vspace(4),
		hline(), vspace(4),
		withPointerCursor(noiseCheck),
		withPointerCursor(aecCheck),
		vspace(4), hline(), vspace(4),
		testBtn,
	)
	showNitoPopup("VOICE SETTINGS", body, w)
}

// ── Theme section ─────────────────────────────────────────────────────────────

// buildThemeSection returns a grid of four colour-swatch buttons. Tapping one
// calls applyColorProfile which updates all col* globals and triggers a Fyne
// theme refresh. A registered themeListener keeps the active-ring indicators
// in sync without rebuilding the widget tree.
func buildThemeSection() fyne.CanvasObject {
	borders := make([]*canvas.Rectangle, len(colorProfiles))
	nameLabels := make([]*canvas.Text, len(colorProfiles))

	var cells []fyne.CanvasObject
	for i, p := range colorProfiles {
		i, p := i, p

		// Inner filled circle — always the profile's accent colour.
		inner := canvas.NewRectangle(p.Accent)
		inner.CornerRadius = 14
		inner.SetMinSize(fyne.NewSize(28, 28))

		// Outer ring — white when active, transparent otherwise.
		ring := canvas.NewRectangle(colTransparent)
		ring.CornerRadius = 17
		ring.StrokeWidth = 2.5
		ring.SetMinSize(fyne.NewSize(34, 34))
		if p.Name == activeProfileName {
			ring.StrokeColor = colText
		}
		borders[i] = ring

		nameLabel := monoTxt(p.Name, liveDimMid)
		nameLabels[i] = nameLabel

		swatch := container.NewCenter(
			container.NewStack(ring, container.NewCenter(inner)),
		)
		cell := NewTappable(
			container.NewVBox(swatch, container.NewCenter(nameLabel)),
			func() { applyColorProfile(p.Name) },
		)
		cells = append(cells, cell)
	}

	// Update rings and label colours whenever the profile changes.
	registerThemeListener(func() {
		for i, p := range colorProfiles {
			if p.Name == activeProfileName {
				borders[i].StrokeColor = colText
			} else {
				borders[i].StrokeColor = colTransparent
			}
			borders[i].Refresh()
			nameLabels[i].Color = liveDimMid
			nameLabels[i].Refresh()
		}
	})

	// ── Brightness slider ─────────────────────────────────────────────────
	brightLabel := monoTxt(fmtBrightness(brightnessScale), liveDimMid)

	slider := widget.NewSlider(0.5, 1.8)
	slider.Step = 0.05
	slider.Value = float64(brightnessScale)
	slider.OnChanged = func(v float64) {
		brightLabel.Text = fmtBrightness(float32(v))
		brightLabel.Refresh()
		applyBrightness(float32(v))
	}

	brightnessRow := container.NewBorder(nil, nil,
		monoTxt("brightness", liveDim),
		brightLabel,
		withPointerCursor(slider),
	)

	return container.NewVBox(
		vspace(2),
		newResponsiveGrid(4, 60, cells...),
		vspace(6),
		brightnessRow,
		vspace(4),
	)
}

func fmtBrightness(scale float32) string {
	pct := int(scale*100 + 0.5)
	return itoa(pct) + "%"
}

// ── STATUS tab ────────────────────────────────────────────────────────────────

// buildStatusTab returns the status tab content and an update function that
// mutates the live labels. The update function must be called on the Fyne thread.
func buildStatusTab() (fyne.CanvasObject, func(connected bool, brokerURL, userID string, latencyMs int64)) {
	dot := txt("○ ", liveDim, 13, false, true)
	statusLabel := monoTxt("offline", liveDim)
	pingLabel := monoTxt("--", liveDim)
	brokerLabel := monoTxt("--", liveDim)
	userLabel := monoTxt("--", liveDim)

	connSection := container.NewVBox(
		vspace(4),
		container.NewHBox(dot, statusLabel),
		container.NewHBox(monoTxt("  ping    ", liveDimMid), pingLabel),
		container.NewHBox(monoTxt("  broker  ", liveDimMid), brokerLabel),
		container.NewHBox(monoTxt("  user    ", liveDimMid), userLabel),
	)

	statsContent := container.NewVBox(
		container.NewHBox(monoTxt("voice pkt/s  ", liveDimMid), monoTxt("--", liveDim)),
		container.NewHBox(monoTxt("voice loss   ", liveDimMid), monoTxt("--", liveDim)),
	)

	accordion := NewCollapseSection("STATS", container.NewVBox(statsContent), false)
	sep := func() fyne.CanvasObject { return container.NewVBox(vspace(8), hline(), vspace(8)) }

	themeBody := buildThemeSection()
	themeSection := NewCollapseSection("COLOUR PRESET", themeBody, false)

	area := container.NewVScroll(container.NewVBox(connSection, sep(), accordion, sep(), themeSection))

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
			dot.Color = liveDim
			dot.Text = "○ "
			statusLabel.Text = "offline"
			statusLabel.Color = liveDim
			pingLabel.Text = "--"
			pingLabel.Color = liveDim
			brokerLabel.Text = "--"
			brokerLabel.Color = liveDim
			userLabel.Text = "--"
			userLabel.Color = liveDim
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
	cp            *ChatPanel
}

// SetChatPanel wires the ChatPanel so the status panel's animation ticker can
// call TickVoice. Call once after both panels are created.
func (sp *StatusPanel) SetChatPanel(cp *ChatPanel) { sp.cp = cp }

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
		acceptBtn := newBtn("Accept", nil)
		acceptBtn.Importance = widget.LowImportance
		acceptBtn.OnTapped = func() {
			go func() {
				err := connection.AcceptInvite(inv.RoomID)
				fyne.Do(func() {
					if err != nil {
						clientlog.Error("accept invite failed: " + err.Error())
						showToast(sp.w, "accept invite: "+err.Error(), toastError)
						return
					}
					clientlog.Info("accepted invite, joined " + inv.RoomName)
					showToast(sp.w, "joined "+inv.RoomName, toastSuccess)
					// Refresh both room list and invite list immediately.
					go func() {
						if sp.cp != nil {
							if rooms, e := connection.ListRooms(); e == nil {
								fyne.Do(func() { sp.cp.SetRooms(rooms) })
							}
						}
						if pending, e := connection.ListPendingInvites(); e == nil {
							fyne.Do(func() { sp.SetInvites(pending) })
						}
					}()
				})
			}()
		}
		rows = append(rows, container.NewPadded(container.NewHBox(
			monoTxt("◆ ", liveAccent), nameLabel, acceptBtn,
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

	tabNames := []string{"STATUS", "TRACKS", "INVITES", "LOGS"}
	tabs := []fyne.CanvasObject{
		statusArea,
		tracksArea,
		invitesArea,
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

	// 50 ms animation ticker for track meters, EQ spectrum, and voice state.
	go func() {
		t := time.NewTicker(50 * time.Millisecond)
		defer t.Stop()
		for range t.C {
			fyne.Do(func() {
				tracksTick()
				tickTrackWatchers()
				if sp.cp != nil {
					sp.cp.TickVoice()
				}
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
