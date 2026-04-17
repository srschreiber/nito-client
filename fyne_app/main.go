// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

// Package main is the Fyne-based UI for nito.
package main

import (
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"github.com/srschreiber/nito-client/shellapp/connection"
)

func main() {
	a := app.New()
	a.Settings().SetTheme(nitoTheme{})

	w := a.NewWindow("nito")
	w.Resize(fyne.NewSize(500, 500))

	showLoginView(a, w, func() {
		w.Resize(fyne.NewSize(1200, 720))
		showMainView(a, w)
	})

	w.ShowAndRun()
}

// showMainView builds and sets the main chat + status layout.
// Called after successful auth.
func showMainView(a fyne.App, w fyne.Window) {
	statusPanel := NewStatusPanel(w)
	chatPanel := NewChatPanel(w)

	cmdBar := NewCommandBar(func(text string) {
		// TODO: send real chat message; for now append locally and refresh.
		s := connection.CurrentSession()
		from := "you"
		if s != nil {
			from = s.UserID
		}
		mockChatMessages = append(mockChatMessages, mockMessage{
			kind: msgSelf, timestamp: "now", from: from, body: text,
		})
		chatPanel.Refresh()
		fmt.Println("cmd:", text)
	})
	chatPanel.OnDMOpen = func(username string) { cmdBar.SetDMMode(username) }

	split := container.NewHSplit(chatPanel, statusPanel)
	split.SetOffset(0.72)

	root := container.NewBorder(
		nil,
		container.NewPadded(cmdBar),
		nil, nil,
		split,
	)

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
			w.Canvas().Focus(cmdBar.Entry)
		}
	})

	w.Canvas().Focus(cmdBar.Entry)

	// Start the ping/status loop in the background.
	go pingLoop(statusPanel)
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
