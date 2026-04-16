// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

// Package main is the Fyne-based UI for nito.
// Currently a styled mock — no real backend is wired in yet.
// Components will be migrated one by one to the real backend.
package main

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
)

func main() {
	a := app.New()
	a.Settings().SetTheme(nitoTheme{})

	w := a.NewWindow("nito")
	w.Resize(fyne.NewSize(1200, 720))

	// ── Panels ────────────────────────────────────────────────────────────────
	chatPanel := NewChatPanel(w)
	statusPanel := NewStatusPanel(w)
	cmdBar := NewCommandBar(func(text string) {
		// Mock: append to chat history and refresh.
		mockChatMessages = append(mockChatMessages, mockMessage{
			kind: msgSelf, timestamp: "now", from: "you", body: text,
		})
		chatPanel.Refresh()
		fmt.Println("cmd:", text) // placeholder until real backend wired
	})
	// Wire DM mode into the command bar (after cmdBar is declared).
	chatPanel.OnDMOpen = func(username string) { cmdBar.SetDMMode(username) }

	// ── Layout ────────────────────────────────────────────────────────────────
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

	// '/' from anywhere focuses the message entry
	w.Canvas().SetOnTypedRune(func(r rune) {
		if r == '/' {
			w.Canvas().Focus(cmdBar.Entry)
		}
	})

	// Focus the command entry on launch.
	w.Canvas().Focus(cmdBar.Entry)

	w.ShowAndRun()
}
