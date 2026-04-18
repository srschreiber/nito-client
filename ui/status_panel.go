// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package main

import (
	"strings"
	"time"

	"github.com/srschreiber/nito-client/engine/commands"
	"github.com/srschreiber/nito-client/engine/connection"
	"github.com/srschreiber/nito-client/engine/keys"
	"github.com/srschreiber/nito-client/engine/sounds"
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
	devices := sounds.ListAudioInputs()
	deviceLabels := make([]string, len(devices))
	deviceIDs := make([]string, len(devices))
	for i, d := range devices {
		deviceLabels[i] = d.Label
		deviceIDs[i] = d.ID
	}

	currentID := sounds.SelectedInputDevice()
	currentLabel := "System Default"
	for i, id := range deviceIDs {
		if id == currentID {
			currentLabel = deviceLabels[i]
			break
		}
	}

	inputSel := widget.NewSelect(deviceLabels, func(selected string) {
		for i, lbl := range deviceLabels {
			if lbl == selected {
				if deviceIDs[i] == sounds.SelectedInputDevice() {
					return
				}
				sounds.SetInputDevice(deviceIDs[i])
				clientlog.Info("selected input device: " + deviceLabels[i])
				go sounds.RestartCapture()
				return
			}
		}
	})
	inputSel.SetSelected(currentLabel)
	if sounds.IsActive() || sounds.IsConnecting() {
		inputSel.Disable()
	}

	// ── Noise / AEC ───────────────────────────────────────────────────────────
	noiseCheck := widget.NewCheck("Noise reduction (RNNoise)", func(b bool) {
		sounds.SetDenoiseOutboundEnabled(b)
	})
	noiseCheck.SetChecked(sounds.DenoiseOutboundEnabled())

	aecCheck := widget.NewCheck("Echo cancellation (AEC3)", func(b bool) {
		sounds.SetAECEnabled(b)
	})
	aecCheck.SetChecked(sounds.AECEnabled())

	// ── Test sounds toggle ─────────────────────────────────────────────────────
	var testBtn *nitoBtn
	testBtn = newBtn("Test Voice", func() {
		if sounds.IsConnecting() {
			return
		}
		if sounds.ActiveRoomID() == sounds.SelfRoomID {
			go func() {
				if err := commands.VoiceLeaveTestAudioDirect(); err != nil {
					fyne.Do(func() { showToast(w, "test audio: "+err.Error(), toastError) })
				}
			}()
		} else if !sounds.IsActive() {
			go func() {
				if err := commands.VoiceTestAudioDirect(); err != nil {
					fyne.Do(func() { showToast(w, "test audio: "+err.Error(), toastError) })
				}
			}()
		}
	})
	testBtn.Importance = widget.LowImportance

	minWidth := canvas.NewRectangle(colTransparent)
	minWidth.SetMinSize(fyne.NewSize(340, 0))

	body := container.NewVBox(
		minWidth,
		monoTxt("Input device", liveDimMid),
		withPointerCursor(inputSel),
		vspace(4),
		hline(), vspace(4),
		withPointerCursor(noiseCheck),
		withPointerCursor(aecCheck),
		vspace(4), hline(), vspace(4),
		testBtn,
	)
	pop := showNitoPopup("VOICE SETTINGS", body, w)

	// Drive inputSel and testBtn from live voice state so both reflect
	// connecting/active regardless of which path initiated the call.
	go func() {
		t := time.NewTicker(50 * time.Millisecond)
		defer t.Stop()
		tick := 0
		frames := [4]string{"|", "/", "-", "\\"}
		for range t.C {
			if pop.Hidden {
				return
			}
			active := sounds.IsActive()
			connecting := sounds.IsConnecting()
			selfTest := sounds.ActiveRoomID() == sounds.SelfRoomID
			t0 := tick
			tick++
			fyne.Do(func() {
				// Input selector: disabled whenever voice is active or connecting.
				if (active || connecting) && !inputSel.Disabled() {
					inputSel.Disable()
				} else if !active && !connecting && inputSel.Disabled() {
					inputSel.Enable()
				}
				// Test button state machine.
				switch {
				case connecting:
					newText := frames[(t0/4)%4] + " Connecting…"
					if testBtn.Text != newText || !testBtn.Disabled() {
						testBtn.Text = newText
						testBtn.Importance = widget.LowImportance
						testBtn.Disable()
						testBtn.Refresh()
					}
				case active && selfTest:
					if testBtn.Text != "Stop Test" {
						testBtn.Text = "Stop Test"
						testBtn.Importance = widget.DangerImportance
						testBtn.Enable()
						testBtn.Refresh()
					}
				case active:
					if testBtn.Text != "Test Voice" || !testBtn.Disabled() {
						testBtn.Text = "Test Voice"
						testBtn.Importance = widget.LowImportance
						testBtn.Disable()
						testBtn.Refresh()
					}
				default:
					if testBtn.Text != "Test Voice" || testBtn.Disabled() {
						testBtn.Text = "Test Voice"
						testBtn.Importance = widget.LowImportance
						testBtn.Enable()
						testBtn.Refresh()
					}
				}
			})
		}
	}()
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
		container.NewHBox(monoTxt("sounds pkt/s  ", liveDimMid), monoTxt("--", liveDim)),
		container.NewHBox(monoTxt("sounds loss   ", liveDimMid), monoTxt("--", liveDim)),
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

// ── Requests tab ─────────────────────────────────────────────────────────────

// buildRequestsTab builds the REQUESTS tab. Returns the tab canvas object and
// two list-box containers: one for room invites, one for key-verify requests.
func buildRequestsTab(w fyne.Window) (fyne.CanvasObject, *fyne.Container, *fyne.Container) {
	inviteListBox := container.NewVBox()
	verifyListBox := container.NewVBox()

	listBox := container.NewVBox(
		monoTxt("ROOM INVITES", liveAccent), vspace(2),
		inviteListBox,
		vspace(8),
		monoTxt("VERIFICATION REQUESTS", liveAccent), vspace(2),
		verifyListBox,
	)

	area := container.NewBorder(
		container.NewVBox(vspace(4), hline()),
		nil, nil, nil,
		container.NewVScroll(listBox),
	)
	return area, inviteListBox, verifyListBox
}

// ── StatusPanel ───────────────────────────────────────────────────────────────

type StatusPanel struct {
	widget.BaseWidget
	TabBar        *TabBar
	content       *fyne.Container
	tabs          []fyne.CanvasObject
	setStatus     func(connected bool, brokerURL, userID string, latencyMs int64)
	inviteListBox *fyne.Container
	verifyListBox *fyne.Container
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

// SetInvites rebuilds the room-invite rows in the REQUESTS tab. Must be called on the Fyne thread.
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

// AddVerifyRequest appends a key-verification request row to the REQUESTS tab.
// initiatorPubPEM is pk_A, extracted from the incoming challenge; it is used
// both to sign B's response and (later) to verify A's confirm.
// The row auto-removes itself when expiresAt passes; if expiresAt is already in
// the past, the request is silently dropped. Must be called on the Fyne thread.
func (sp *StatusPanel) AddVerifyRequest(fromUsername, sessionID, initiatorPubPEM string, expiresAt time.Time) {
	if sp.verifyListBox == nil {
		return
	}
	if !expiresAt.IsZero() && time.Now().After(expiresAt) {
		return
	}

	codeEntry := widget.NewEntry()
	codeEntry.SetPlaceHolder("6-digit code")

	var row *fyne.Container

	submitBtn := newBtn("Submit", nil)
	submitBtn.Importance = widget.LowImportance
	submitBtn.OnTapped = func() {
		code := strings.TrimSpace(codeEntry.Text)
		if code == "" {
			return
		}
		if !expiresAt.IsZero() && time.Now().After(expiresAt) {
			showToast(sp.w, "verification request expired", toastWarn)
			if row != nil {
				sp.removeVerifyRow(row)
			}
			return
		}
		myUsername := connection.GetSessionUserID()
		go func() {
			responderPubPEM, sig, err := keys.SignVerificationResponse(code, sessionID, initiatorPubPEM, myUsername)
			if err != nil {
				fyne.Do(func() { showToast(sp.w, "sign verification: "+err.Error(), toastError) })
				return
			}
			if err := connection.SendKeyVerifyResponse(fromUsername, sessionID, responderPubPEM, sig); err != nil {
				fyne.Do(func() { showToast(sp.w, "send verification: "+err.Error(), toastError) })
				return
			}
			// Remember the context so the incoming confirm can be verified.
			ttl := 10 * time.Minute
			if !expiresAt.IsZero() {
				ttl = time.Until(expiresAt) + 5*time.Minute
			}
			keys.RememberConfirmContext(sessionID, fromUsername, code, initiatorPubPEM, responderPubPEM, ttl)
			clientlog.Info("verify: sent response to %s (session %s), waiting for confirm", fromUsername, sessionID)
			fyne.Do(func() {
				showToast(sp.w, "response sent to "+fromUsername+" — waiting for confirmation", toastInfo)
				if row != nil {
					sp.removeVerifyRow(row)
				}
			})
		}()
	}

	dismissBtn := newBtn("Dismiss", nil)
	dismissBtn.Importance = widget.LowImportance
	dismissBtn.OnTapped = func() {
		if row != nil {
			sp.removeVerifyRow(row)
		}
	}

	row = container.NewVBox(
		container.NewHBox(monoTxt("◆ ", liveAccent), monoTxt(fromUsername+" wants to verify your key", colText)),
		container.NewHBox(codeEntry, submitBtn, dismissBtn),
		vspace(4),
	)

	sp.verifyListBox.Add(row)
	sp.verifyListBox.Refresh()

	if !expiresAt.IsZero() {
		time.AfterFunc(time.Until(expiresAt), func() {
			fyne.Do(func() {
				if row != nil {
					sp.removeVerifyRow(row)
				}
			})
		})
	}
}

func (sp *StatusPanel) removeVerifyRow(row *fyne.Container) {
	objs := sp.verifyListBox.Objects
	for i, o := range objs {
		if o == row {
			sp.verifyListBox.Objects = append(objs[:i], objs[i+1:]...)
			sp.verifyListBox.Refresh()
			return
		}
	}
}

func NewStatusPanel(w fyne.Window) *StatusPanel {
	sp := &StatusPanel{w: w}

	statusArea, setStatus := buildStatusTab()
	sp.setStatus = setStatus

	requestsArea, inviteListBox, verifyListBox := buildRequestsTab(w)
	sp.inviteListBox = inviteListBox
	sp.verifyListBox = verifyListBox

	tracksArea, tracksTick := buildTracksTab(w)

	tabNames := []string{"STATUS", "TRACKS", "REQUESTS", "LOGS"}
	tabs := []fyne.CanvasObject{
		statusArea,
		tracksArea,
		requestsArea,
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

	// 50 ms animation ticker for track meters, EQ spectrum, and sounds state.
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
