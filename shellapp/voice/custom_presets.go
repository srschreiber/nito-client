// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package voice

import (
	"errors"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// PlayerPreset is a named snapshot of all playback EQ + FX settings.
type PlayerPreset struct {
	Name   string                `yaml:"name"`
	EQ     EQSettings            `yaml:"eq"`
	Delay  DelaySettings         `yaml:"delay"`
	Reverb ReverbSettings        `yaml:"reverb"`
	Chorus ChorusSettings        `yaml:"chorus"`
	Pitch  PlaybackPitchSettings `yaml:"pitch"`
	Pan    PannerSettings        `yaml:"pan"`
}

func customPresetsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".nito", "audio", "custom_presets.yaml"), nil
}

// LoadCustomPresets reads all saved custom presets from disk. Returns nil slice
// (not an error) when no presets have been saved yet.
func LoadCustomPresets() ([]PlayerPreset, error) {
	p, err := customPresetsPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var presets []PlayerPreset
	if err := yaml.Unmarshal(data, &presets); err != nil {
		return nil, err
	}
	return presets, nil
}

// SaveCurrentAsPreset snapshots the current in-memory playback settings under
// name and writes them to disk. Overwrites any existing preset with the same name.
func SaveCurrentAsPreset(name string) error {
	if name == "" {
		return errors.New("preset name cannot be empty")
	}
	p, err := customPresetsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	presets, _ := LoadCustomPresets()
	preset := PlayerPreset{
		Name:   name,
		EQ:     GetPlaybackEQSettings(),
		Delay:  GetDelaySettings(),
		Reverb: GetReverbSettings(),
		Chorus: GetChorusSettings(),
		Pitch:  GetPlaybackPitchSettings(),
		Pan:    GetPannerSettings(),
	}
	found := false
	for i, pr := range presets {
		if pr.Name == name {
			presets[i] = preset
			found = true
			break
		}
	}
	if !found {
		presets = append(presets, preset)
	}
	data, err := yaml.Marshal(presets)
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600)
}

// DeleteCustomPreset removes the preset with the given name from disk.
func DeleteCustomPreset(name string) error {
	p, err := customPresetsPath()
	if err != nil {
		return err
	}
	presets, err := LoadCustomPresets()
	if err != nil {
		return err
	}
	newPresets := presets[:0]
	found := false
	for _, pr := range presets {
		if pr.Name == name {
			found = true
		} else {
			newPresets = append(newPresets, pr)
		}
	}
	if !found {
		return errors.New("preset not found: " + name)
	}
	data, err := yaml.Marshal(newPresets)
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600)
}

// Apply loads this preset's settings into memory and saves them.
func (p PlayerPreset) Apply() {
	SetPlaybackEQSettings(p.EQ)
	SetDelaySettings(p.Delay)
	SetReverbSettings(p.Reverb)
	SetChorusSettings(p.Chorus)
	SetPlaybackPitchSettings(p.Pitch)
	SetPannerSettings(p.Pan)
	SaveAudioSettings()
}
