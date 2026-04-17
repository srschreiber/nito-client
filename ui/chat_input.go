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

type ChatInput struct {
	widget.BaseWidget
	Entry     *widget.Entry
	modeLabel *canvas.Text
	onSubmit  func(string)
}

func NewChatInput(onSubmit func(string)) *ChatInput {
	ci := &ChatInput{onSubmit: onSubmit}
	ci.modeLabel = txt("> ", liveAccent, 13, false, true)

	entry := widget.NewEntry()
	entry.SetPlaceHolder("type a message")
	entry.TextStyle = fyne.TextStyle{Monospace: true}
	entry.OnSubmitted = func(text string) {
		if strings.TrimSpace(text) == "" {
			return
		}
		if ci.onSubmit != nil {
			ci.onSubmit(strings.TrimSpace(text))
		}
		entry.SetText("")
	}
	ci.Entry = entry
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
	inputRow := container.NewBorder(nil, nil, container.NewPadded(ci.modeLabel), nil, ci.Entry)

	inputBg := canvas.NewRectangle(liveInputBg)
	inputBg.CornerRadius = 6
	inputBg.StrokeColor = liveBorder
	inputBg.StrokeWidth = 1

	return widget.NewSimpleRenderer(container.NewStack(inputBg, container.NewPadded(inputRow)))
}
