// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package main

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// ── multilineEntry ────────────────────────────────────────────────────────────

// multilineEntry submits on plain Enter and inserts a newline on Shift+Enter.
type multilineEntry struct {
	widget.Entry
	shiftHeld bool
	onSubmit  func(string)
}

var _ desktop.Keyable = (*multilineEntry)(nil)

func newMultilineEntry(onSubmit func(string)) *multilineEntry {
	e := &multilineEntry{onSubmit: onSubmit}
	e.MultiLine = true
	e.SetPlaceHolder("type a message")
	e.TextStyle = fyne.TextStyle{Monospace: true}
	e.ExtendBaseWidget(e)
	return e
}

func (e *multilineEntry) KeyDown(key *fyne.KeyEvent) {
	if key.Name == desktop.KeyShiftLeft || key.Name == desktop.KeyShiftRight {
		e.shiftHeld = true
	}
	e.Entry.KeyDown(key)
}

func (e *multilineEntry) KeyUp(key *fyne.KeyEvent) {
	if key.Name == desktop.KeyShiftLeft || key.Name == desktop.KeyShiftRight {
		e.shiftHeld = false
	}
	e.Entry.KeyUp(key)
}

func (e *multilineEntry) TypedKey(key *fyne.KeyEvent) {
	if key.Name == fyne.KeyReturn {
		if e.shiftHeld {
			e.Entry.TypedKey(key)
			return
		}
		text := strings.TrimSpace(e.Text)
		if text != "" && e.onSubmit != nil {
			e.onSubmit(text)
		}
		e.SetText("")
		return
	}
	e.Entry.TypedKey(key)
}

// appendText appends s to the current entry text (places cursor at end).
func (e *multilineEntry) appendText(s string) {
	e.SetText(e.Text + s)
}

// ── Emoji picker ──────────────────────────────────────────────────────────────

var chatEmojis = []string{
	"😀", "😂", "😅", "😎", "😢", "😮", "😡", "🤔",
	"❤️", "👍", "👎", "🎉", "🔥", "💯", "✅", "❌",
	"👋", "💪", "🙏", "🤣", "🥳", "💀", "🚀", "⭐",
	"😏", "🥺", "😤", "🫡", "🎵", "🎶", "🤩", "😴",
}

func showEmojiPicker(w fyne.Window, entry *multilineEntry) {
	var pop *widget.PopUp

	const cols = 8
	cells := make([]fyne.CanvasObject, len(chatEmojis))
	for i, emoji := range chatEmojis {
		e := emoji
		lbl := widget.NewLabel(e)
		cell := NewHoverRow(container.NewCenter(lbl), func() {
			entry.appendText(e)
			if pop != nil {
				pop.Hide()
			}
			w.Canvas().Focus(entry)
		})
		cells[i] = cell
	}

	grid := container.NewGridWithColumns(cols, cells...)

	bg := canvas.NewRectangle(liveSurface2)
	bg.CornerRadius = 8
	bg.StrokeColor = liveBorder
	bg.StrokeWidth = 1

	card := container.NewStack(bg, container.NewPadded(grid))
	pop = widget.NewPopUp(card, w.Canvas())
	pop.Show()

	cs := w.Canvas().Size()
	ms := pop.MinSize()
	pop.Move(fyne.NewPos(cs.Width-ms.Width-8, cs.Height-ms.Height-64))
}

// ── ChatInput ─────────────────────────────────────────────────────────────────

type ChatInput struct {
	widget.BaseWidget
	Entry     *multilineEntry
	modeLabel *canvas.Text
	w         fyne.Window
	onSubmit  func(string)
}

func NewChatInput(w fyne.Window, onSubmit func(string)) *ChatInput {
	ci := &ChatInput{w: w, onSubmit: onSubmit}
	ci.modeLabel = txt("> ", liveAccent, 13, false, true)

	ci.Entry = newMultilineEntry(func(text string) {
		if ci.onSubmit != nil {
			ci.onSubmit(text)
		}
	})
	ci.ExtendBaseWidget(ci)
	return ci
}

func (ci *ChatInput) SetChatMode(on bool) {
	if on {
		ci.modeLabel.Text = "chat › "
		ci.modeLabel.Color = colLavender
	} else {
		ci.modeLabel.Text = "> "
		ci.modeLabel.Color = liveAccent
	}
	ci.modeLabel.Refresh()
}

func (ci *ChatInput) SetDMMode(username string) {
	if username == "" {
		ci.modeLabel.Text = "> "
		ci.modeLabel.Color = liveAccent
	} else {
		ci.modeLabel.Text = "dm " + username + " › "
		ci.modeLabel.Color = colCyan
	}
	ci.modeLabel.Refresh()
}

// DMTarget returns the username when in DM mode, otherwise "".
func (ci *ChatInput) DMTarget() string {
	text := ci.modeLabel.Text
	if !strings.HasPrefix(text, "dm ") {
		return ""
	}
	return strings.TrimSuffix(strings.TrimPrefix(text, "dm "), " › ")
}

func (ci *ChatInput) CreateRenderer() fyne.WidgetRenderer {
	emojiBtn := NewTappable(
		container.NewCenter(txt("☺", liveDimMid, 16, false, false)),
		func() { showEmojiPicker(ci.w, ci.Entry) },
	)

	inputRow := container.NewBorder(nil, nil, container.NewPadded(ci.modeLabel), container.NewPadded(emojiBtn), ci.Entry)

	inputBg := canvas.NewRectangle(liveInputBg)
	inputBg.CornerRadius = 6
	inputBg.StrokeColor = liveBorder
	inputBg.StrokeWidth = 1

	return widget.NewSimpleRenderer(container.NewStack(inputBg, container.NewPadded(inputRow)))
}
