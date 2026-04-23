// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package botcli

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// The interactive TUI is deliberately one-shot: it either drives the
// registration wizard (fresh state) OR the verify-target prompt (after
// register). Once the user's interactive inputs are in, the Model exits
// Bubble Tea and main.go takes over with a headless serve loop — a
// long-running daemon has no use for alt-screen rendering.

type phase int

const (
	phaseWizardBroker phase = iota
	phaseWizardUsername
	phaseWizardPassword
	phaseWizardSubmitting
	phaseVerifyPrompt
	phaseDone
	phaseAborted
)

// Model is the tiny bubbletea Model used for both the wizard and the
// verify-target prompt. Exactly one text field is visible at a time, so we
// don't need a focus manager — just a single `input` buffer and the current
// phase.
type Model struct {
	phase   phase
	mask    bool // true while entering password
	label   string
	input   string
	status  string
	errText string
	// Capture fields filled in as the wizard progresses. Snapshotted on Done.
	broker       string
	username     string
	password     string
	verifyTarget string
}

// NewWizardModel returns a Model for the three-step registration wizard.
// Pre-seeds broker from the caller when they want to default to an existing
// value; empty strings show as placeholders.
func NewWizardModel(defaultBroker string) *Model {
	return &Model{
		phase:  phaseWizardBroker,
		label:  "broker URL",
		input:  defaultBroker,
		status: "Register a new bot identity with a broker.",
	}
}

// NewVerifyPromptModel returns a Model that only asks "verify with whom?"
// and exits. The actual verify handshake runs headlessly after the program
// returns.
func NewVerifyPromptModel(botUsername string) *Model {
	return &Model{
		phase:  phaseVerifyPrompt,
		label:  "verify with username",
		status: fmt.Sprintf("Registered as %q. Who is the bot's owner?", botUsername),
	}
}

// Init satisfies bubbletea's Model.
func (m *Model) Init() tea.Cmd { return nil }

// Update is intentionally terse — the inputs aren't long-lived enough to need
// bubbles or any other input framework.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	switch k.String() {
	case "ctrl+c", "ctrl+d":
		m.phase = phaseAborted
		return m, tea.Quit
	case "backspace":
		if len(m.input) > 0 {
			// Strip one rune, not one byte — avoids mangling multibyte input.
			runes := []rune(m.input)
			m.input = string(runes[:len(runes)-1])
		}
		m.errText = ""
		return m, nil
	case "enter":
		return m.submit()
	}
	// Treat anything else as typed characters: bubbletea puts the literal
	// runes in Key.Text for printable keys, empty for ctrl / arrow / etc.
	if k.Text != "" {
		m.input += k.Text
		m.errText = ""
	}
	return m, nil
}

func (m *Model) submit() (tea.Model, tea.Cmd) {
	val := strings.TrimSpace(m.input)
	switch m.phase {
	case phaseWizardBroker:
		if val == "" {
			m.errText = "broker URL is required"
			return m, nil
		}
		m.broker = val
		m.input = ""
		m.label = "bot username"
		m.phase = phaseWizardUsername
		return m, nil
	case phaseWizardUsername:
		if val == "" {
			m.errText = "username is required"
			return m, nil
		}
		m.username = val
		m.input = ""
		m.label = "password (hidden)"
		m.mask = true
		m.phase = phaseWizardPassword
		return m, nil
	case phaseWizardPassword:
		if m.input == "" {
			m.errText = "password is required"
			return m, nil
		}
		m.password = m.input
		m.mask = false
		m.phase = phaseDone
		return m, tea.Quit
	case phaseVerifyPrompt:
		if val == "" {
			m.errText = "username is required"
			return m, nil
		}
		if val == m.username {
			// Registering the bot as its own owner would leave it with
			// no introduced peers — refuse early rather than have the
			// verify RPC fail later.
			m.errText = "owner must be a different user"
			return m, nil
		}
		m.verifyTarget = val
		m.phase = phaseDone
		return m, tea.Quit
	}
	return m, nil
}

// View renders the single visible prompt plus status / error text.
func (m *Model) View() tea.View {
	var b strings.Builder
	b.WriteString(styleTitle.Render("nito-bot setup"))
	b.WriteString("\n")
	if m.status != "" {
		b.WriteString(styleDim.Render(m.status))
		b.WriteString("\n\n")
	}
	b.WriteString(styleLabel.Render(m.label))
	b.WriteString(" ")
	b.WriteString(stylePrompt.Render(">"))
	b.WriteString(" ")
	shown := m.input
	if m.mask {
		shown = strings.Repeat("•", len([]rune(m.input)))
	}
	b.WriteString(shown)
	b.WriteString("\n")
	if m.errText != "" {
		b.WriteString(styleErr.Render("error: " + m.errText))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(styleDim.Render("enter to submit · ctrl+c to abort"))
	return tea.NewView(b.String())
}
