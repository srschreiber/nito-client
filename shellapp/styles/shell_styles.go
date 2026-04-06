// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package styles

import lipgloss "charm.land/lipgloss/v2"

// Dark synthwave palette
var (
	// Background fills used inside component boxes.
	ComponentBg = lipgloss.Color("#0d0f1a")
	PanelBg     = lipgloss.Color("#111320")

	AppStyle = lipgloss.NewStyle().
			Padding(1, 2)

	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#c084fc"))

	CursorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#c084fc")).
			Bold(true)

	ItemStyle = lipgloss.NewStyle().
			PaddingLeft(2)

	SelectedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#4ade80")).
			Bold(true)

	HelpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#3a3a5a")).
			MarginTop(1)

	BoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#5b21b6")).
			Padding(1, 2)

	FocusedBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.ThickBorder(), false, false, false, true).
				BorderForeground(lipgloss.Color("#a855f7")).
				Background(ComponentBg).
				Padding(0, 1)

	UnfocusedBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.ThickBorder(), false, false, false, true).
				BorderForeground(lipgloss.Color("#4a4a7a")).
				Background(ComponentBg).
				Padding(0, 1)

	PromptStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#818cf8")).
			Bold(true)

	Grey = lipgloss.NewStyle().Foreground(lipgloss.Color("#5a5a8a"))

	// DimText is slightly brighter than Grey — used for description labels in
	// KEYS, sent-message lines in history, and status info like broker/userID.
	DimText = lipgloss.NewStyle().Foreground(lipgloss.Color("#8888b8"))

	ResponseStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#c8c8e8"))

	// SentStyle is used for outgoing message lines ("[you]: …") in chat history.
	SentStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#9090c8"))

	LineStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8080a8"))

	CursorHighlightStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#a855f7")).
				Foreground(lipgloss.Color("#ffffff"))

	StatusConnectedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#4ade80")).
				Bold(true)

	StatusDisconnectedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#f87171")).
				Bold(true)

	StatusLabelStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#3a3a5a")).
				Faint(true)

	// SectionTitleStyle kept for any callsites not yet migrated.
	SectionTitleStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#c084fc")).
				Bold(true)

	// Per-section colored badge labels.
	RoomsBadge = lipgloss.NewStyle().
			Background(lipgloss.Color("#5b21b6")).
			Foreground(lipgloss.Color("#e9d5ff")).
			Bold(true).
			Padding(0, 1)

	MembersBadge = lipgloss.NewStyle().
			Background(lipgloss.Color("#1e3a8a")).
			Foreground(lipgloss.Color("#bfdbfe")).
			Bold(true).
			Padding(0, 1)

	DMsBadge = lipgloss.NewStyle().
			Background(lipgloss.Color("#14532d")).
			Foreground(lipgloss.Color("#86efac")).
			Bold(true).
			Padding(0, 1)

	KeysBadge = lipgloss.NewStyle().
			Background(lipgloss.Color("#1e3a8a")).
			Foreground(lipgloss.Color("#bfdbfe")).
			Bold(true).
			Padding(0, 1)

	StatusBadge = lipgloss.NewStyle().
			Background(lipgloss.Color("#064e3b")).
			Foreground(lipgloss.Color("#6ee7b7")).
			Bold(true).
			Padding(0, 1)

	InvitesBadge = lipgloss.NewStyle().
			Background(lipgloss.Color("#831843")).
			Foreground(lipgloss.Color("#fbcfe8")).
			Bold(true).
			Padding(0, 1)
)
