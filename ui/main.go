// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

// Package main is the Fyne-based UI for nito.
package main

import (
	"log"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"github.com/srschreiber/nito-client/engine/commands"
	"github.com/srschreiber/nito-client/engine/connection"
	"github.com/srschreiber/nito-client/engine/voice"
	"github.com/srschreiber/nito-client/ui/clientlog"
)

func main() {
	clientlog.Info("nito starting up...")
	a := app.NewWithID("com.srschreiber.nito")
	loadSavedProfile() // apply saved palette before first draw
	a.Settings().SetTheme(nitoTheme{})

	w := a.NewWindow("nito")
	w.Resize(fyne.NewSize(500, 500))

	showLoginView(a, w, func() {
		w.Resize(fyne.NewSize(1200, 720))
		showMainView(a, w)
	})

	w.ShowAndRun()
}

// appWindow is the main application window, set once auth succeeds.
// Used by widgets (e.g. audio embeds) that need to show popups.
var appWindow fyne.Window

// showMainView builds and sets the main chat + status layout.
// Called after successful auth.
func showMainView(a fyne.App, w fyne.Window) {
	appWindow = w
	statusPanel := NewStatusPanel(w)

	// Create chatInput before chatPanel so the closure captures the var reference.
	var chatPanel *ChatPanel
	var chatInput *ChatInput
	chatInput = NewChatInput(func(text string) {
		if chatPanel == nil {
			return
		}
		dmTarget := chatInput.DMTarget()
		if dmTarget != "" {
			// DM mode — send encrypted direct message.
			myID := connection.GetSessionUserID()
			m := chatMessage{
				kind:      msgSelf,
				timestamp: time.Now().Format("15:04"),
				date:      time.Now().Format("2006-01-02"),
				from:      myID,
				body:      text,
			}
			fyne.Do(func() { chatPanel.AppendDMMessage(dmTarget, m) })
			go func() {
				if err := commands.SendDirectMessage(dmTarget, text); err != nil {
					clientlog.Error("dm to " + dmTarget + " failed: " + err.Error())
					fyne.Do(func() { showToast(w, "dm: "+err.Error(), toastError) })
				} else {
					clientlog.Info("dm sent to " + dmTarget)
				}
			}()
		} else {
			// Room chat mode — send encrypted room message.
			myID := connection.GetSessionUserID()
			m := chatMessage{
				kind:      msgSelf,
				timestamp: time.Now().Format("15:04"),
				date:      time.Now().Format("2006-01-02"),
				from:      myID,
				body:      text,
			}
			fyne.Do(func() { chatPanel.AppendMessage(m) })
			go func() {
				if err := commands.SendRoomMessage(text); err != nil {
					clientlog.Error("send message failed: " + err.Error())
					fyne.Do(func() { showToast(w, "send: "+err.Error(), toastError) })
				}
			}()
		}
	})

	chatPanel = NewChatPanel(w)
	chatPanel.SetInputBar(chatInput)
	chatPanel.OnDMOpen = func(username string) { chatInput.SetDMMode(username) }
	statusPanel.SetChatPanel(chatPanel)

	split := container.NewHSplit(chatPanel, statusPanel)
	split.SetOffset(0.72)

	root := container.NewBorder(nil, nil, nil, nil, split)

	bg := canvas.NewRectangle(colBg)
	w.SetContent(container.NewStack(bg, container.NewPadded(root)))

	// ctrl+[ / ctrl+] — switch right-panel tabs
	w.Canvas().AddShortcut(&desktop.CustomShortcut{
		KeyName:  fyne.KeyLeftBracket,
		Modifier: fyne.KeyModifierControl,
	}, func(_ fyne.Shortcut) { statusPanel.PrevTab() })
	w.Canvas().AddShortcut(&desktop.CustomShortcut{
		KeyName:  fyne.KeyRightBracket,
		Modifier: fyne.KeyModifierControl,
	}, func(_ fyne.Shortcut) { statusPanel.NextTab() })

	// '/' focuses the command bar from anywhere
	w.Canvas().SetOnTypedRune(func(r rune) {
		if r == '/' {
			w.Canvas().Focus(chatInput.Entry)
		}
	})

	w.Canvas().Focus(chatInput.Entry)

	// Start background goroutines.
	go pingLoop(statusPanel)
	go startBackend(chatPanel, statusPanel, w)
}

// pingLoop pings the broker every second and updates the STATUS tab.
func pingLoop(sp *StatusPanel) {
	for {
		time.Sleep(time.Second)
		start := time.Now()
		err := connection.PingBroker()
		latencyMs := time.Since(start).Milliseconds()
		connected := err == nil

		s := connection.CurrentSession()
		var brokerURL, userID string
		if s != nil {
			brokerURL = s.BrokerURL
			userID = s.UserID
		}
		sp.UpdateStatus(connected, brokerURL, userID, latencyMs)
	}
}

func init() {
	initLogging()
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	voice.LoadAudioSettings()
	clientlog.Info("nito started")
}
