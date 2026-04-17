// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package main

import (
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

var (
	logMu      sync.Mutex
	logLines   []string
	logRefresh func()
)

// nitoLog appends a timestamped line to the in-app log and refreshes the LOGS
// tab if it is currently visible.  Safe to call from any goroutine.
func nitoLog(msg string) {
	ts := time.Now().Format("15:04:05")
	line := ts + "  " + msg

	logMu.Lock()
	logLines = append(logLines, line)
	if len(logLines) > 1000 {
		logLines = logLines[len(logLines)-1000:]
	}
	logMu.Unlock()

	if logRefresh != nil {
		fyne.Do(logRefresh)
	}
}

// buildLogsTab returns the LOGS tab widget.  It registers a refresh callback
// so that nitoLog calls update the view live.
func buildLogsTab() fyne.CanvasObject {
	entry := widget.NewMultiLineEntry()
	entry.Wrapping = fyne.TextWrapOff
	entry.TextStyle = fyne.TextStyle{Monospace: true}

	refresh := func() {
		logMu.Lock()
		text := strings.Join(logLines, "\n")
		logMu.Unlock()
		entry.SetText(text)
		entry.CursorRow = len(logLines)
	}
	logRefresh = refresh

	// Seed with anything already logged before the tab was built.
	refresh()

	return container.NewBorder(
		container.NewVBox(vspace(4), hline()),
		nil, nil, nil,
		entry,
	)
}
