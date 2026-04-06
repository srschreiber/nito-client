// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package history

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func filePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".nito")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "history"), nil
}

// Load reads command history from ~/.nito/history. Returns nil if the file
// doesn't exist yet.
func Load() []string {
	path, err := filePath()
	if err != nil {
		return nil
	}
	f, err := os.Open(path)
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

// Save writes entries to ~/.nito/history, overwriting the file.
func Save(entries []string) error {
	path, err := filePath()
	if err != nil {
		return err
	}
	f, err := os.Create(path)
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
