// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package components

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/srschreiber/nito-client/shellapp/styles"
)

const toastDuration = 5 * time.Second

// ShowToastMsg triggers a toast notification with the given message.
type ShowToastMsg struct{ Text string }

// PlayAudioMsg asks the main model to play audio on a specific track.
// Track -1 means "auto": pick the first idle track (0 if all are busy).
// Broadcast=true sends a play_audio RPC to share with the voice room;
// Broadcast=false (default) plays locally only regardless of voice state.
type PlayAudioMsg struct {
	URL       string
	Track     int
	Broadcast bool
}

// StopAudioMsg cancels in-flight audio playback.
// Track -1 stops all tracks; 0–2 stops a specific track.
type StopAudioMsg struct{ Track int }

// AudioTrackDoneMsg is sent by PlayAudioFromURL when a track finishes naturally,
// so the main model knows the track slot is free.
type AudioTrackDoneMsg struct{ Track int }

// AudioPlaybackErrorMsg is sent by PlayAudioFromURL when playback fails.
// It carries the track number so the slot can be freed, and the error text for
// the toast notification.
type AudioPlaybackErrorMsg struct {
	Track int
	Text  string
}

// AliasEntry is a named audio alias displayed in the status panel.
type AliasEntry struct{ Name string }

// TrackStateMsg updates the status component with the current playback state of
// all three audio tracks.
type TrackStateMsg struct {
	Playing   [3]bool
	InRoom    bool         // kept for compatibility; TRACKS always renders now
	Aliases   []AliasEntry // up to 5, sorted by name
	StartedBy [3]string    // username who started each track; "" if idle
	Broadcast [3]bool      // true if the track was network-broadcast to the room
}

// PreFillCommandMsg asks the command component to pre-fill its input with Text
// and switch focus to it so the user can complete the command.
// CursorPos sets where the cursor lands; -1 means end of text.
type PreFillCommandMsg struct {
	Text      string
	CursorPos int
}

// RefreshTrackStateMsg asks main to re-broadcast the current track+alias state.
// Emitted after alias mutations so the status panel reflects the change.
type RefreshTrackStateMsg struct{}

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
	return styles.ToastStyle.Render("💬 " + t.text)
}
