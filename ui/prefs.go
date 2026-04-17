// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package main

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type loginPrefs struct {
	Broker   string `yaml:"broker"`
	Username string `yaml:"username"`
}

func loginPrefsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".nito", "defaults.yml"), nil
}

func loadLoginPrefs() loginPrefs {
	p, err := loginPrefsPath()
	if err != nil {
		return loginPrefs{}
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return loginPrefs{}
	}
	var prefs loginPrefs
	_ = yaml.Unmarshal(data, &prefs)
	return prefs
}

func saveLoginPrefs(prefs loginPrefs) {
	p, err := loginPrefsPath()
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	data, err := yaml.Marshal(prefs)
	if err != nil {
		return
	}
	_ = os.WriteFile(p, data, 0o600)
}

func clearLoginPrefs() {
	p, err := loginPrefsPath()
	if err != nil {
		return
	}
	_ = os.Remove(p)
}
