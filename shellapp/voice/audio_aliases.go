// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

package voice

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

func audioAliasesPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".nito", "audio", "aliases.yaml"), nil
}

func loadAudioAliases() (map[string]string, error) {
	p, err := audioAliasesPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	var aliases map[string]string
	if err := yaml.Unmarshal(data, &aliases); err != nil {
		return nil, err
	}
	if aliases == nil {
		aliases = map[string]string{}
	}
	return aliases, nil
}

// LookupAudioAlias returns the URL for the given alias name, or ("", false) if
// no such alias is saved.
func LookupAudioAlias(name string) (string, bool) {
	aliases, err := loadAudioAliases()
	if err != nil {
		return "", false
	}
	url, ok := aliases[name]
	return url, ok
}

// SaveAudioAlias saves name→url to ~/.nito/audio/aliases.yaml, creating the
// file (and directory) if necessary. Overwrites any existing entry for name.
func SaveAudioAlias(name, url string) error {
	p, err := audioAliasesPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	aliases, _ := loadAudioAliases() // ignore load error; write a fresh map
	if aliases == nil {
		aliases = map[string]string{}
	}
	aliases[name] = url
	data, err := yaml.Marshal(aliases)
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600)
}

// ListAudioAliases returns a copy of all saved aliases.
func ListAudioAliases() (map[string]string, error) {
	return loadAudioAliases()
}
