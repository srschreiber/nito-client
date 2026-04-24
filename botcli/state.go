// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

// Package botcli is a headless bot front-end for nito. It shares the engine/
// and shared/ packages with the desktop client — identity keys, web-of-trust
// resolution, room encryption are all unchanged. What differs is the bootstrap
// flow (wizard instead of GUI), the resumable state machine, and the narrow
// request surface (!-prefixed room messages, no voice / no DMs).
//
// The package is organised around the persisted state machine in state.go:
// every launch loads the state file, decides which step the bot is in, and
// dispatches to wizard.go / verify.go / invite.go / serve.go. See BOTS.md for
// the user-facing protocol + security model.
package botcli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Step is the persisted bootstrap phase. Transitions are one-way: a bot that
// reached `ready` cannot drop back to `verified` without the user deleting
// state on disk — changing owner mid-flight is not a supported operation.
type Step string

const (
	StepFresh      Step = ""           // sentinel: file didn't exist yet
	StepRegistered Step = "registered" // broker knows us as is_bot=true; no owner yet
	StepVerified   Step = "verified"   // owner pinned; awaiting room invite
	StepReady      Step = "ready"      // joined room; serving
)

// BotState is the on-disk representation. Absent fields are zero-valued so an
// older file written by a previous version still loads.
type BotState struct {
	Broker        string `yaml:"broker"`
	Username      string `yaml:"username"`
	OwnerUsername string `yaml:"owner_username,omitempty"`
	// OwnerDeviceID is the sha256(DER(owner_pubkey)) hex, captured at verify
	// time. Cross-checked on every start so a compromised-broker identity swap
	// at the owner level forces a hard stop instead of silently re-trusting.
	OwnerDeviceID string `yaml:"owner_device_id,omitempty"`
	// RoomIDs is the set of rooms the bot has been invited into and joined.
	// Multi-room since v0.3: the bot accepts every owner-issued invite, so a
	// single bot can serve multiple rooms simultaneously.
	RoomIDs []string `yaml:"room_ids,omitempty"`
	// LegacyRoomID is the v0.2 single-room field, kept for one-shot migration
	// on load. Never written back: SaveState clears it.
	LegacyRoomID string `yaml:"room_id,omitempty"`
	Step         Step   `yaml:"step"`
}

// DataDir resolves the bot's per-install data directory. NITO_BOT_DATA wins
// (container path), otherwise ~/.nito-bot for bare-metal runs.
func DataDir() (string, error) {
	if v := os.Getenv("NITO_BOT_DATA"); v != "" {
		if err := os.MkdirAll(v, 0700); err != nil {
			return "", fmt.Errorf("create data dir: %w", err)
		}
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home: %w", err)
	}
	dir := filepath.Join(home, ".nito-bot")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("create data dir: %w", err)
	}
	return dir, nil
}

func statePath() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "bot-state.yml"), nil
}

// LoadState returns the persisted state. When the file doesn't exist it
// returns a zero value with Step == StepFresh (not an error) — first-run is
// the common case, not an exceptional one.
func LoadState() (BotState, error) {
	p, err := statePath()
	if err != nil {
		return BotState{}, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return BotState{}, nil
		}
		return BotState{}, fmt.Errorf("read state: %w", err)
	}
	var s BotState
	if err := yaml.Unmarshal(data, &s); err != nil {
		return BotState{}, fmt.Errorf("parse state: %w", err)
	}
	// One-shot migration from the v0.2 single-room layout.
	if s.LegacyRoomID != "" && len(s.RoomIDs) == 0 {
		s.RoomIDs = []string{s.LegacyRoomID}
	}
	s.LegacyRoomID = ""
	return s, nil
}

// SaveState writes atomically via write-then-rename so a crash mid-save can't
// leave a truncated file that fails parsing on next launch.
func SaveState(s BotState) error {
	p, err := statePath()
	if err != nil {
		return err
	}
	data, err := yaml.Marshal(s)
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("write state: %w", err)
	}
	return os.Rename(tmp, p)
}

// EnvFilePath is where the first-run wizard persists NITO_BOT_PASSWORD so
// subsequent container launches don't need to re-prompt. Mode 0600; same
// volume as state — no secret sprawl.
func EnvFilePath() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, ".env"), nil
}

// LoadEnvFile reads `KEY=VALUE` lines from ${NITO_BOT_DATA}/.env and exports
// them into the current process environment, unless the variable is already
// set (process env wins — explicit `-e` flags to docker run should not be
// silently overridden). Missing file is not an error: on first-run the
// file hasn't been written yet.
//
// Avoids the need for a shell-based entrypoint.sh, which lets the runtime
// image stay as distroless/static.
func LoadEnvFile() error {
	p, err := EnvFilePath()
	if err != nil {
		return err
	}
	f, err := os.Open(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		val := line[eq+1:]
		// Strip one layer of matching quotes so values with spaces
		// survive a round-trip through the file without changing shape.
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		if _, set := os.LookupEnv(key); set {
			continue
		}
		_ = os.Setenv(key, val)
	}
	return sc.Err()
}

// SavePasswordEnv writes the single-line .env that the process loads via
// LoadEnvFile on subsequent starts. Refuses to write an empty password
// (rejected upstream, defense in depth).
func SavePasswordEnv(password string) error {
	if password == "" {
		return fmt.Errorf("refuse to save empty password")
	}
	p, err := EnvFilePath()
	if err != nil {
		return err
	}
	// Single KEY=VALUE line; no shell escaping — the entrypoint uses
	// `set -a && . .env` which handles quoting via POSIX semantics. We
	// refuse passwords containing newlines to avoid ambiguity.
	for _, r := range password {
		if r == '\n' || r == '\r' {
			return fmt.Errorf("password contains newline")
		}
	}
	return os.WriteFile(p, []byte("NITO_BOT_PASSWORD="+password+"\n"), 0600)
}
