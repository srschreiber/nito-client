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

type CommandBar struct {
	widget.BaseWidget
	Entry     *widget.Entry
	modeLabel *canvas.Text
	onSubmit  func(string)
}

func NewCommandBar(onSubmit func(string)) *CommandBar {
	cb := &CommandBar{onSubmit: onSubmit}
	cb.modeLabel = txt("> ", colAccent, 13, false, true)

	entry := widget.NewEntry()
	entry.SetPlaceHolder("type a message  (. for audio commands)")
	entry.TextStyle = fyne.TextStyle{Monospace: true}
	entry.OnSubmitted = func(text string) {
		if strings.TrimSpace(text) == "" {
			return
		}
		if cb.onSubmit != nil {
			cb.onSubmit(strings.TrimSpace(text))
		}
		entry.SetText("")
	}
	cb.Entry = entry
	cb.ExtendBaseWidget(cb)
	return cb
}

func (cb *CommandBar) SetChatMode(on bool) {
	if on {
		cb.modeLabel.Text = "chat › "
		cb.modeLabel.Color = colLavender
	} else {
		cb.modeLabel.Text = "> "
		cb.modeLabel.Color = colAccent
	}
	cb.modeLabel.Refresh()
}

func (cb *CommandBar) SetDMMode(username string) {
	if username == "" {
		cb.modeLabel.Text = "> "
		cb.modeLabel.Color = colAccent
	} else {
		cb.modeLabel.Text = "dm " + username + " › "
		cb.modeLabel.Color = colCyan
	}
	cb.modeLabel.Refresh()
}

// DMTarget returns the username when in DM mode, otherwise "".
func (cb *CommandBar) DMTarget() string {
	text := cb.modeLabel.Text
	if !strings.HasPrefix(text, "dm ") {
		return ""
	}
	return strings.TrimSuffix(strings.TrimPrefix(text, "dm "), " › ")
}

func (cb *CommandBar) CreateRenderer() fyne.WidgetRenderer {
	inputRow := container.NewBorder(nil, nil, container.NewPadded(cb.modeLabel), nil, cb.Entry)

	hints := txt(
		"ctrl+[/]  switch tab    enter  send    esc  clear",
		colDim, 11, false, true,
	)

	inputBg := canvas.NewRectangle(colInputBg)
	inputBg.CornerRadius = 6
	inputBg.StrokeColor = colBorder
	inputBg.StrokeWidth = 1

	body := container.NewVBox(
		container.NewStack(inputBg, container.NewPadded(inputRow)),
		container.NewPadded(hints),
	)
	return widget.NewSimpleRenderer(body)
}
