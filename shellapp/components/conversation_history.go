// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package components

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/srschreiber/nito-client/shellapp/clientlog"
	"github.com/srschreiber/nito-client/shellapp/styles"
)

type historyEntry struct {
	text       string // raw text, may contain newlines/tabs; wrapped at render time
	isResponse bool
	isRaw      bool // if true, emit lines as-is (preserves ANSI color codes, e.g. ASCII art)
}

// HistoryTab identifies which conversation tab a message should be appended to.
type HistoryTab int

const (
	TabCmd           HistoryTab = 0 // default: command output and system messages
	TabChat          HistoryTab = 1 // room chat messages
	TabDM            HistoryTab = 2 // direct messages (routed via AppendDMHistoryMsg, not AppendHistoryMsg)
	TabNotifications HistoryTab = 3 // server notifications
	TabInvites       HistoryTab = 4 // interactive pending invites list
	TabLogs          HistoryTab = 5 // client-side structured logs
)

// AppendHistoryMsg is emitted by CommandComponent when entries should be added.
type AppendHistoryMsg struct {
	Entries []historyEntry
	Tab     HistoryTab // 0 = CMD (default), 1 = Chat
}

// ClearHistoryMsg is emitted by CommandComponent when history should be cleared.
type ClearHistoryMsg struct{}

// JumpScrollMsg requests that the history viewport jump to a specific line (1-indexed from top).
type JumpScrollMsg struct{ Line int }

// ModeChangedMsg is emitted when the command input switches between command and chat mode.
type ModeChangedMsg struct{ ChatMode bool }

// SwitchTabMsg requests a specific tab to become active.
type SwitchTabMsg struct{ Tab HistoryTab }

// NewResponseAppendMsg builds an AppendHistoryMsg for a single server-response entry (CMD tab).
func NewResponseAppendMsg(text string) AppendHistoryMsg {
	return AppendHistoryMsg{Entries: []historyEntry{{text: text, isResponse: true}}}
}

// NewChatResponseAppendMsg builds an AppendHistoryMsg for a single response entry in the Chat tab.
func NewChatResponseAppendMsg(text string) AppendHistoryMsg {
	return AppendHistoryMsg{Entries: []historyEntry{{text: text, isResponse: true}}, Tab: TabChat}
}

// NewBulkResponseAppendMsg builds an AppendHistoryMsg for multiple response lines (e.g. historic messages).
func NewBulkResponseAppendMsg(texts []string) AppendHistoryMsg {
	entries := make([]historyEntry, len(texts))
	for i, t := range texts {
		entries[i] = historyEntry{text: t, isResponse: true}
	}
	return AppendHistoryMsg{Entries: entries}
}

// NewImageAppendMsg builds an AppendHistoryMsg for an incoming image (CMD tab): a styled
// header line followed by the raw ANSI ASCII-art string.
func NewImageAppendMsg(header, ascii string) AppendHistoryMsg {
	return AppendHistoryMsg{Entries: []historyEntry{
		{text: header, isResponse: true},
		{text: ascii, isRaw: true},
	}}
}

// NewChatImageAppendMsg builds an AppendHistoryMsg for an incoming image in the Chat tab.
func NewChatImageAppendMsg(header, ascii string) AppendHistoryMsg {
	return AppendHistoryMsg{Entries: []historyEntry{
		{text: header, isResponse: true},
		{text: ascii, isRaw: true},
	}, Tab: TabChat}
}

// AppendDMHistoryMsg appends an entry to a specific per-user DM conversation.
type AppendDMHistoryMsg struct {
	User    string
	Entries []historyEntry
}

// NewNotificationAppendMsg builds an AppendHistoryMsg targeted at the Notifications tab.
func NewNotificationAppendMsg(text string) AppendHistoryMsg {
	return AppendHistoryMsg{Entries: []historyEntry{{text: text, isResponse: true}}, Tab: TabNotifications}
}

// NewClientLogAppendMsg converts a clientlog.Msg into an AppendHistoryMsg for the Logs tab.
// The level prefix is colorized inline via lipgloss so the history component needs no special rendering logic.
func NewClientLogAppendMsg(msg clientlog.Msg) AppendHistoryMsg {
	var prefix string
	switch msg.Level {
	case clientlog.LevelWarn:
		prefix = lipgloss.NewStyle().Foreground(lipgloss.Color("#facc15")).Render("[WARN]") + " "
	case clientlog.LevelError:
		prefix = lipgloss.NewStyle().Foreground(lipgloss.Color("#f87171")).Render("[ERROR]") + " "
	default:
		prefix = styles.DimText.Render("[INFO]") + " "
	}
	ts := styles.DimText.Render(msg.At.Format("15:04:05"))
	text := ts + " " + prefix + msg.Text
	return AppendHistoryMsg{Entries: []historyEntry{{text: text, isResponse: true, isRaw: true}}, Tab: TabLogs}
}

// NewDMResponseAppendMsg builds an AppendDMHistoryMsg for a message received from a DM peer.
func NewDMResponseAppendMsg(fromUser, text string) AppendDMHistoryMsg {
	return AppendDMHistoryMsg{User: fromUser, Entries: []historyEntry{{text: text, isResponse: true}}}
}

// StartDMMsg is dispatched when the user opens or creates a DM conversation (via dm command or .dm op).
type StartDMMsg struct{ User string }

// DMTargetChangedMsg is dispatched when the active DM target changes (tab switch, user list navigation, .dm op).
type DMTargetChangedMsg struct{ User string }

type ConversationHistory struct {
	entries  []historyEntry
	scroll   int // lines scrolled up from the bottom (0 = pinned to bottom)
	focused  bool
	chatMode bool
	width    int // content width passed from layout (lipgloss Width value)
	height   int // content height passed from layout (lipgloss Height value)
}

func NewConversationHistory(width, height int) *ConversationHistory {
	return &ConversationHistory{width: width, height: height}
}

func (h *ConversationHistory) SetSize(width, height int) {
	h.width = width
	h.height = height
}

func (h *ConversationHistory) Init() tea.Cmd { return nil }

func (h *ConversationHistory) SetFocused(focused bool) {
	h.focused = focused
}

// CanFocus reports whether focusing this history pane is useful.
// Returns true if already focused (e.g. after a resize) or if there is
// overflow content the user can scroll through.
func (h *ConversationHistory) CanFocus() bool {
	return h.focused || len(h.allLines()) > h.contentBudget()
}

// textWidth is the usable text column width inside the border and padding.
// The style uses Padding(0,1) so each horizontal side consumes 1 column.
func (h *ConversationHistory) textWidth() int {
	w := h.width - 2
	if w < 1 {
		return 1
	}
	paddingRight := 5
	return w - paddingRight
}

// wrapEntry splits one raw entry into display-ready styled lines, hard-wrapping at tw.
// For isRaw entries the text is split on newlines only — no rune-count wrapping —
// so that embedded ANSI escape codes are never split mid-sequence.
func wrapEntry(e historyEntry, tw int) []string {
	if e.isRaw {
		return strings.Split(e.text, "\n")
	}
	raw := strings.ReplaceAll(e.text, "\t", "    ")
	var lines []string
	for _, para := range strings.Split(raw, "\n") {
		runes := []rune(para)
		if len(runes) == 0 {
			lines = append(lines, "")
			continue
		}
		for len(runes) > tw {
			lines = append(lines, string(runes[:tw]))
			runes = runes[tw:]
		}
		lines = append(lines, string(runes))
	}
	return lines
}

// allLines expands all entries into a flat, styled slice of display strings.
// Always derived from raw entries so terminal resize is handled automatically.
func (h *ConversationHistory) allLines() []string {
	tw := h.textWidth()
	var lines []string
	for _, e := range h.entries {
		for _, l := range wrapEntry(e, tw) {
			if e.isRaw {
				lines = append(lines, l)
			} else if e.isResponse {
				lines = append(lines, styles.ResponseStyle.Render(l))
			} else {
				lines = append(lines, styles.SentStyle.Render(l))
			}
		}
	}
	return lines
}

// contentBudget is the number of rows available for scrollable content.
// Fixed rows consumed: 1 (title) + 1 (status line) = 2.
func (h *ConversationHistory) contentBudget() int {
	b := h.height - 2
	if b < 1 {
		return 1
	}
	// give a little buffer too. IDK why we need to do this, but it fixes bugs and it hurts my head
	// trying to figure out why we need it, so I'm just gonna leave it here ¯\_(ツ)_/¯
	b -= 2
	return b
}

// clampScroll ensures h.scroll stays within valid bounds given total line count.
func (h *ConversationHistory) clampScroll(total int) {
	maxScroll := total - h.contentBudget()
	if maxScroll < 0 {
		maxScroll = 0
	}
	if h.scroll > maxScroll {
		h.scroll = maxScroll
	}
	if h.scroll < 0 {
		h.scroll = 0
	}
}

func (h *ConversationHistory) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case AppendHistoryMsg:
		h.entries = append(h.entries, msg.Entries...)
		h.scroll = 0 // pin to bottom on new content
	case ClearHistoryMsg:
		h.entries = nil
		h.scroll = 0
	case ModeChangedMsg:
		h.chatMode = msg.ChatMode
	case JumpScrollMsg:
		lines := h.allLines()
		total := len(lines)
		budget := h.contentBudget()
		maxScroll := total - budget
		if maxScroll < 0 {
			maxScroll = 0
		}
		target := msg.Line
		if target < 1 {
			target = 1
		}
		if target > total {
			target = total
		}
		h.scroll = total - target
		if h.scroll > maxScroll {
			h.scroll = maxScroll
		}
		if h.scroll < 0 {
			h.scroll = 0
		}
	case tea.KeyPressMsg:
		if !h.focused {
			return nil
		}
		lines := h.allLines()
		h.clampScroll(len(lines))
		budget := h.contentBudget()
		maxScroll := len(lines) - budget
		if maxScroll < 0 {
			maxScroll = 0
		}
		switch msg.String() {
		case "up", "ctrl+p":
			if h.scroll < maxScroll {
				h.scroll++
			}
		case "down", "ctrl+n":
			if h.scroll > 0 {
				h.scroll--
			}
		}
	}
	return nil
}

func (h *ConversationHistory) Render() string {
	rows := []string{}

	if len(h.entries) == 0 {
		rows = append(rows, styles.DimText.Render("No messages yet."))
	} else {
		lines := h.allLines()
		total := len(lines)
		h.clampScroll(total)

		budget := h.contentBudget()

		// Determine the bottom of the viewport.
		end := total - h.scroll
		if end > total {
			end = total
		}
		// Tentative start with full budget.
		start := end - budget
		if start < 0 {
			start = 0
		}

		showAbove := start > 0
		showBelow := end < total

		// Reserve rows for indicators.
		rows_ := budget
		if showAbove {
			rows_--
		}
		if showBelow {
			rows_--
		}
		if rows_ < 0 {
			rows_ = 0
		}

		// Recompute start with the tighter budget (end stays fixed).
		start = end - rows_
		if start < 0 {
			start = 0
		}

		if showAbove {
			rows = append(rows, styles.DimText.Render("↑ more"))
		}
		rows = append(rows, lines[start:end]...)
		if showBelow {
			rows = append(rows, styles.DimText.Render("↓ more"))
		}

		// Fixed last row: position indicator.
		rows = append(rows, styles.LineStyle.Render(fmt.Sprintf("L%d/%d  (.jump <n> to navigate)", end, total)))
	}

	borderColor := styles.PanelBorderColor
	bg := styles.ComponentBg
	if h.focused {
		borderColor = styles.PanelFocusedBorderColor
		bg = styles.ComponentFocusedBg
	}
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Background(bg).
		Padding(0, 1).
		Width(h.width + 4).
		Height(h.height)
	return style.Render(strings.Join(rows, "\n"))
}
