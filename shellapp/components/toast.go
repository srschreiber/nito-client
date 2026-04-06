// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package components

import (
	"time"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
)

const toastDuration = 5 * time.Second

// ShowToastMsg triggers a toast notification with the given message.
type ShowToastMsg struct{ Text string }

// toastExpireMsg is sent internally when the current toast should be hidden.
type toastExpireMsg struct{ gen int }

// ToastComponent renders a floating notification that auto-dismisses after 5 seconds.
// It is rendered as an overlay in the lower-right corner of the screen.
type ToastComponent struct {
	text    string
	gen     int // incremented each time a new toast is shown; stale expire ticks are ignored
	visible bool
}

func NewToastComponent() *ToastComponent {
	return &ToastComponent{}
}

func (t *ToastComponent) Init() tea.Cmd { return nil }

func (t *ToastComponent) SetFocused(_ bool) {}

func (t *ToastComponent) Update(msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case ShowToastMsg:
		t.text = m.Text
		t.visible = true
		t.gen++
		gen := t.gen
		return tea.Tick(toastDuration, func(time.Time) tea.Msg {
			return toastExpireMsg{gen: gen}
		})
	case toastExpireMsg:
		if m.gen == t.gen {
			t.visible = false
		}
	}
	return nil
}

// Visible reports whether the toast is currently showing.
func (t *ToastComponent) Visible() bool { return t.visible }

// Render returns the styled toast string, or "" if not visible.
func (t *ToastComponent) Render() string {
	if !t.visible {
		return ""
	}
	return lipgloss.NewStyle().
		Background(lipgloss.Color("57")).
		Foreground(lipgloss.Color("255")).
		Bold(true).
		Padding(0, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("213")).
		Render("💬 " + t.text)
}
