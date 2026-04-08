// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

// Package clientlog provides a lightweight structured logger that forwards
// log entries to the Client Logs tab in the TUI via a pluggable sender.
// It is safe to call from any goroutine, including the voice and connection
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

var (
	mu      sync.RWMutex
	sender  func(any)
	logFile *os.File
)

// Init registers the sender that the logger will use to forward entries to the TUI.
// It also opens logs.txt in the current working directory for append-only writing.
// Call this once from main after the bubbletea program is created, passing p.Send.
func Init(s func(any)) {
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
	if s != nil {
		s(Msg{Level: level, Text: text, At: now})
	}
	if f != nil {
		line := fmt.Sprintf("%s [%s] %s\n", now.Format("15:04:05"), level, text)
		_, _ = f.WriteString(line)
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
