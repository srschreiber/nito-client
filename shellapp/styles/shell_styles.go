// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package styles

import lipgloss "charm.land/lipgloss/v2"

// Dark synthwave palette
var (
	// Background fills used inside component boxes.
	ComponentBg        = lipgloss.Color("#0d0f1a")
	ComponentFocusedBg = lipgloss.Color("#0d0f1a")

	// DimText is the universal body-text style. Every non-badge text string in
	// the app uses this (or DimTextFocused) so the background is always uniform.
	// Focused components use DimTextFocused; unfocused (or focus-unaware) use DimText.
	DimText        = lipgloss.NewStyle().Foreground(lipgloss.Color("#8888b8")).Background(ComponentBg)
	DimTextFocused = lipgloss.NewStyle().Foreground(lipgloss.Color("#8888b8")).Background(ComponentFocusedBg)

	// LightText is used in contexts where no background should be set (e.g. the
	// startup/login dialog, which renders over the terminal default background).
	LightText = lipgloss.NewStyle().Foreground(lipgloss.Color("#d8d8e8"))

	// Aliases — all former text variants collapse to DimText.
	DefaultText = DimText
	SentStyle   = DimText
	LineStyle   = DimText
	HelpStyle   = DimText.MarginTop(1)
	ItemStyle   = DimText.PaddingLeft(2)

	DefaultStyle = lipgloss.NewStyle().Background(ComponentBg)

	AppStyle = DimText.
			Padding(1, 2).
			Background(ComponentBg)

	CursorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#c084fc")).
			Background(ComponentBg).
			Bold(true)

	SelectedStyle = DimText.
			Foreground(lipgloss.Color("#4ade80")).
			Bold(true)

	FocusedBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.ThickBorder(), false, false, false, true).
				BorderForeground(lipgloss.Color("#a855f7")).
				BorderBackground(ComponentBg).
				Background(ComponentBg).
				Padding(0, 1)

	UnfocusedBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.ThickBorder(), false, false, false, true).
				BorderForeground(lipgloss.Color("#4a4a7a")).
				BorderBackground(ComponentBg).
				Background(ComponentBg).
				Padding(0, 1)

	PromptStyle = DimText.
			Foreground(lipgloss.Color("#818cf8")).
			Bold(true)

	CursorHighlightStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#a855f7")).
				Foreground(lipgloss.Color("#ffffff"))

	StatusConnectedStyle = DefaultStyle.
				Foreground(lipgloss.Color("#4ade80")).
				Bold(true)

	StatusDisconnectedStyle = DefaultStyle.
				Foreground(lipgloss.Color("#f87171")).
				Bold(true)

	// SectionTitleStyle kept for any callsites not yet migrated.
	SectionTitleStyle = DefaultStyle.
				Foreground(lipgloss.Color("#c084fc")).
				Bold(true)

	// Per-section colored badge labels.
	RoomsBadge = DefaultStyle.
			Background(lipgloss.Color("#5b21b6")).
			Foreground(lipgloss.Color("#e9d5ff")).
			Bold(true).
			Padding(0, 1)

	MembersBadge = DefaultStyle.
			Background(lipgloss.Color("#1e3a8a")).
			Foreground(lipgloss.Color("#bfdbfe")).
			Bold(true).
			Padding(0, 1)

	DMsBadge = DefaultStyle.
			Background(lipgloss.Color("#14532d")).
			Foreground(lipgloss.Color("#86efac")).
			Bold(true).
			Padding(0, 1)

	KeysBadge = lipgloss.NewStyle().
			Background(lipgloss.Color("#1e3a8a")).
			Foreground(lipgloss.Color("#bfdbfe")).
			Bold(true).
			Padding(0, 1)

	CommandsBadge = lipgloss.NewStyle().
			Background(lipgloss.Color("#78350f")).
			Foreground(lipgloss.Color("#fed7aa")).
			Bold(true).
			Padding(0, 1)

	TracksBadge = lipgloss.NewStyle().
			Background(lipgloss.Color("#312e81")).
			Foreground(lipgloss.Color("#c7d2fe")).
			Bold(true).
			Padding(0, 1)

	PlayAliasesBadge = lipgloss.NewStyle().
				Background(lipgloss.Color("#134e4a")).
				Foreground(lipgloss.Color("#99f6e4")).
				Bold(true).
				Padding(0, 1)

	StatusBadge = lipgloss.NewStyle().
			Background(lipgloss.Color("#064e3b")).
			Foreground(lipgloss.Color("#6ee7b7")).
			Bold(true).
			Padding(0, 1)

	// RoomOpsBadge is the label badge for the room actions panel.
	RoomOpsBadge = lipgloss.NewStyle().
			Background(lipgloss.Color("#7f1d1d")).
			Foreground(lipgloss.Color("#fca5a5")).
			Bold(true).
			Padding(0, 1)

	// AudioPlayerBadge is the label badge for the audio player EQ settings screen title.
	AudioPlayerBadge = lipgloss.NewStyle().
				Background(lipgloss.Color("#1e1b4b")).
				Foreground(lipgloss.Color("#a5b4fc")).
				Bold(true).
				Padding(0, 1)

	// VoiceBadge is the label badge for the live voice stats section in the status panel.
	VoiceBadge = lipgloss.NewStyle().
			Background(lipgloss.Color("#14532d")).
			Foreground(lipgloss.Color("#86efac")).
			Bold(true).
			Padding(0, 1)

	// VoiceSettingsBadge is the label badge for the voice settings screen title.
	VoiceSettingsBadge = lipgloss.NewStyle().
				Background(lipgloss.Color("#0c4a6e")).
				Foreground(lipgloss.Color("#7dd3fc")).
				Bold(true).
				Padding(0, 1)

	// VoiceSettingsActiveSectionStyle highlights the active section header.
	VoiceSettingsActiveSectionStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("213")).
					Background(ComponentBg).
					Bold(true)

	// --- Shared border colors for rounded-border panels ---

	// PanelBorderColor is the unfocused border for all rounded side panels.
	PanelBorderColor = lipgloss.Color("#4a4a7a")
	// PanelFocusedBorderColor is the focused border for all rounded side panels.
	PanelFocusedBorderColor = lipgloss.Color("#a855f7")
	// DMListBorderColor is the unfocused border for the DM user-list panel.
	DMListBorderColor = lipgloss.Color("238")
	// DMListFocusedBorderColor is the focused border for the DM user-list panel.
	DMListFocusedBorderColor = lipgloss.Color("213")

	// --- List selection ---

	// InviteAcceptBtnStyle is the "Accept" button in the invites pane.
	InviteAcceptBtnStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#5b21b6")).
				Foreground(lipgloss.Color("#e9d5ff")).
				Padding(0, 1).
				Bold(true)

	// InviteSelectedItemStyle is applied to the room name on the focused invite row.
	InviteSelectedItemStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#e2e2ff")).
				Bold(true)

	// DMSelectedUserStyle is applied to the selected username in the DM user list.
	DMSelectedUserStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("213")).
				Bold(true)

	// --- Room action buttons ---

	// RoomBtnActiveStyle is a room action button with keyboard focus.
	RoomBtnActiveStyle = lipgloss.NewStyle().
				Padding(0, 1).
				MarginRight(1).
				Background(lipgloss.Color("213")).
				Foreground(lipgloss.Color("0")).
				Bold(true)

	// VoiceLeaveFocusedStyle is the "Leave Voice / Stop Test Audio" button when focused.
	VoiceLeaveFocusedStyle = lipgloss.NewStyle().
				Padding(0, 1).
				MarginRight(1).
				Background(lipgloss.Color("#7f1d1d")).
				Foreground(lipgloss.Color("#fca5a5")).
				Bold(true)

	// VoiceLeaveStyle is the "Leave Voice / Stop Test Audio" button when unfocused.
	VoiceLeaveStyle = lipgloss.NewStyle().
			PaddingLeft(1).
			Background(lipgloss.Color("#450a0a")).
			Foreground(lipgloss.Color("#f87171"))

	// --- Room members ---

	// MemberOnlineStyle renders the online presence dot.
	MemberOnlineStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#4ade80"))

	// MemberOfflineStyle renders the offline presence dot.
	MemberOfflineStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#f87171"))

	// --- Hints panel ---

	// HintKeyStyle renders keyboard shortcut key labels.
	HintKeyStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("222")).Bold(true)

	// --- Toast ---

	// ToastStyle is the floating notification popup.
	ToastStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("57")).
			Foreground(lipgloss.Color("255")).
			Bold(true).
			Padding(0, 2).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("213"))
)
