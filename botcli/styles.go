// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package botcli

import (
	"charm.land/lipgloss/v2"
)

// Palette mirrors ui/theme.go — same visual identity, translated from
// Fyne color.NRGBA to lipgloss hex strings. Kept narrow on purpose; the
// bot's TUI surface is tiny and doesn't need the full desktop palette.
var (
	styleTitle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#8b5cf6")).Bold(true)
	stylePrompt = lipgloss.NewStyle().Foreground(lipgloss.Color("#c4b5fd"))
	styleDim    = lipgloss.NewStyle().Foreground(lipgloss.Color("#8078a0"))
	styleOK     = lipgloss.NewStyle().Foreground(lipgloss.Color("#34d399"))
	styleWarn   = lipgloss.NewStyle().Foreground(lipgloss.Color("#fbbf24"))
	styleErr    = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff3b30"))
	styleCode   = lipgloss.NewStyle().Foreground(lipgloss.Color("#67e8f9")).Bold(true)
	styleLabel  = lipgloss.NewStyle().Foreground(lipgloss.Color("#cccccc"))
)
