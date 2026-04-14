// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package styles

import lipgloss "charm.land/lipgloss/v2"

// Dark synthwave palette
var (
	// Background fills used inside component boxes.
	ComponentBg        = lipgloss.Color("#0d0f1a")
	ComponentFocusedBg = lipgloss.Color("#161928")
	PanelBg            = lipgloss.Color("#111320")

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
				BorderBackground(ComponentBg).
				Background(ComponentBg).
				Padding(0, 1)

	UnfocusedBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.ThickBorder(), false, false, false, true).
				BorderForeground(lipgloss.Color("#4a4a7a")).
				BorderBackground(ComponentBg).
				Background(ComponentBg).
				Padding(0, 1)

	PromptStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#818cf8")).
			Bold(true)

	// DimText is the standard dim label style — used for hints, descriptions,
	// form labels, placeholder text, and secondary info throughout the UI.
	// Background is explicitly cleared so it never inherits from a parent container.
	DimText = lipgloss.NewStyle().Foreground(lipgloss.Color("#8888b8")).Background(lipgloss.NoColor{})

	ResponseStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#c8c8e8"))

	// SentStyle is used for outgoing message lines ("[you]: …") in chat history.
	SentStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#9090c8"))

	LineStyle = DimText

	CursorHighlightStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#a855f7")).
				Foreground(lipgloss.Color("#ffffff"))

	StatusConnectedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#4ade80")).
				Bold(true)

	StatusDisconnectedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#f87171")).
				Bold(true)

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

	InvitesBadge = lipgloss.NewStyle().
			Background(lipgloss.Color("#831843")).
			Foreground(lipgloss.Color("#fbcfe8")).
			Bold(true).
			Padding(0, 1)

	// RoomOpsBadge is the label badge for the room actions panel.
	RoomOpsBadge = lipgloss.NewStyle().
			Background(lipgloss.Color("#7f1d1d")).
			Foreground(lipgloss.Color("#fca5a5")).
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

	// SelectionRowStyle is a subtle background highlight for the focused row in any list.
	SelectionRowStyle = lipgloss.NewStyle().Background(lipgloss.Color("#1e2235"))

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

	// RoomBtnStyle is a room action button without keyboard focus.
	RoomBtnStyle = lipgloss.NewStyle().
			Padding(0, 1).
			MarginRight(1).
			Background(lipgloss.Color("238")).
			Foreground(lipgloss.Color("250"))

	// BtnDisabledStyle is a disabled action button.
	BtnDisabledStyle = lipgloss.NewStyle().
				Padding(0, 1).
				MarginRight(1).
				Background(lipgloss.Color("236")).
				Foreground(lipgloss.Color("240"))

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

	// --- Inline form fields ---

	// FormCursorStyle highlights the character under the cursor in inline form fields.
	FormCursorStyle = lipgloss.NewStyle().Background(lipgloss.Color("213"))

	// FormErrorStyle renders inline form validation errors.
	FormErrorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))

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
