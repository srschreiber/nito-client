// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

// Package clientlog provides a lightweight structured logger that forwards
// log entries to the Client Logs tab in the TUI via a pluggable sender.
// It is safe to call from any goroutine, including the sounds and connection
// packages, because they cannot import components without a circular import.
package clientlog

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// Level represents the severity of a log entry.
type Level int

const (
	LevelInfo Level = iota
	LevelWarn
	LevelError
)

func (l Level) String() string {
	switch l {
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	default:
		return "INFO"
	}
}

// Msg is the tea message type forwarded to the TUI's logs tab.
type Msg struct {
	Level Level
	Text  string
	At    time.Time
}

func (m Msg) String() string {
	return fmt.Sprintf("%s [%s] %s", m.At.Format("15:04:05"), m.Level, m.Text)
}

var (
	mu      sync.RWMutex
	sender  func(msg Msg)
	logFile *os.File
)

// Init registers the sender that the logger will use to forward entries to the TUI.
// It also opens logs.txt in the current working directory for append-only writing.
// Call this once from main after the program is created
func Init(s func(msg Msg)) {
	mu.Lock()
	sender = s
	f, err := os.OpenFile("logs.txt", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err == nil {
		logFile = f
	}
	mu.Unlock()
}

func send(level Level, text string) {
	now := time.Now()
	mu.RLock()
	s := sender
	f := logFile
	mu.RUnlock()
	m := Msg{Level: level, Text: text, At: now}
	if s != nil {
		s(m)
	}
	if f != nil {
		_, _ = f.WriteString(m.String() + "\n")
	}
}

// Info logs an informational message.
func Info(format string, args ...any) {
	if len(args) == 0 {
		send(LevelInfo, format)
	} else {
		send(LevelInfo, fmt.Sprintf(format, args...))
	}
}

// Warn logs a warning message.
func Warn(format string, args ...any) {
	if len(args) == 0 {
		send(LevelWarn, format)
	} else {
		send(LevelWarn, fmt.Sprintf(format, args...))
	}
}

// Error logs an error message.
func Error(format string, args ...any) {
	if len(args) == 0 {
		send(LevelError, format)
	} else {
		send(LevelError, fmt.Sprintf(format, args...))
	}
}
