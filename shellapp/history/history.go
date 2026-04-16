// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package history

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func nitoDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".nito")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func load(name string) []string {
	dir, err := nitoDir()
	if err != nil {
		return nil
	}
	f, err := os.Open(filepath.Join(dir, name))
	if err != nil {
		return nil
	}
	defer f.Close()

	var entries []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) != "" {
			entries = append(entries, line)
		}
	}
	return entries
}

func save(name string, entries []string) error {
	dir, err := nitoDir()
	if err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		return err
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	for _, e := range entries {
		if _, err := fmt.Fprintln(w, e); err != nil {
			return err
		}
	}
	return w.Flush()
}

// Load reads command history from ~/.nito/history. Returns nil if the file
// doesn't exist yet.
func Load() []string { return load("history") }

// LoadChat reads chat/DM history from ~/.nito/chat_history. Returns nil if
// the file doesn't exist yet.
func LoadChat() []string { return load("chat_history") }

// Save writes cmd entries to ~/.nito/history, overwriting the file.
func Save(entries []string) error { return save("history", entries) }

// SaveChat writes chat/DM entries to ~/.nito/chat_history, overwriting the file.
func SaveChat(entries []string) error { return save("chat_history", entries) }
