// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package botcli

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/srschreiber/nito-client/engine/connection"
	"github.com/srschreiber/nito-client/engine/keys"
)

// runWizard runs the three-field TUI prompt, generates the bot's RSA keypair,
// registers with the broker as a bot, and persists the resulting state plus
// the password env file. Returns the populated BotState ready to be used by
// connectAndLogin.
//
// Runs in three phases:
//  1. Bubble Tea — collect broker / username / password interactively.
//  2. Key gen — keys.LoadOrGenerate writes PEMs to disk (mode 0600).
//  3. Register — HTTP POST /api/v0/register with IsBot=true.
//
// Errors at any phase exit the bot with a clear message; partial state is
// not persisted until all three phases succeed, so a crash during wizard
// leaves the user able to re-run cleanly.
func runWizard(ctx context.Context) (BotState, error) {
	// Seed the broker field from the env var if present — makes container
	// re-runs after a wipe less tedious.
	m := NewWizardModel(os.Getenv("NITO_BOT_BROKER"))
	p := tea.NewProgram(m)
	finalModel, err := p.Run()
	if err != nil {
		return BotState{}, fmt.Errorf("wizard: %w", err)
	}
	fm, ok := finalModel.(*Model)
	if !ok {
		return BotState{}, errors.New("wizard: unexpected model type")
	}
	if fm.phase == phaseAborted {
		return BotState{}, errors.New("wizard: aborted")
	}

	broker := strings.TrimSpace(fm.broker)
	username := strings.TrimSpace(fm.username)
	password := fm.password // no trim — spaces in passwords are legitimate
	if broker == "" || username == "" || password == "" {
		return BotState{}, errors.New("wizard: incomplete submission")
	}

	log.Printf("wizard: registering %q with broker %s", username, broker)
	keys.SetActiveBroker(broker)
	pubPEM, err := keys.LoadOrGenerate(username)
	if err != nil {
		return BotState{}, fmt.Errorf("wizard: key setup: %w", err)
	}

	resp, err := connection.Register(ctx, broker, username, password, pubPEM, true)
	if err != nil {
		return BotState{}, fmt.Errorf("wizard: register: %w", err)
	}
	if resp.AlreadyRegistered {
		// The username existed before this wizard ran. Two cases:
		//   (a) this bot was previously registered and state was wiped —
		//       the broker's stored pubkey still matches ours, and login
		//       will work.
		//   (b) someone else owns the name; our pubkey won't match, and
		//       login will fail immediately after.
		// We don't try to distinguish here — the next step (login) will
		// surface the ambiguity with a specific error.
		log.Printf("wizard: %q was already registered with this broker — continuing", username)
	}

	if err := SavePasswordEnv(password); err != nil {
		return BotState{}, fmt.Errorf("wizard: save password env: %w", err)
	}
	// Surface NITO_BOT_PASSWORD into the current process so the subsequent
	// login in this run picks it up from the same code path as restarts.
	_ = os.Setenv("NITO_BOT_PASSWORD", password)

	state := BotState{
		Broker:   broker,
		Username: username,
		Step:     StepRegistered,
	}
	if err := SaveState(state); err != nil {
		return BotState{}, fmt.Errorf("wizard: save state: %w", err)
	}
	log.Printf("wizard: complete — username=%q broker=%s", username, broker)
	return state, nil
}
