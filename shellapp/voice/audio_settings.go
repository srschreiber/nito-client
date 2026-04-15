// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package voice

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type AudioSettings struct {
	MasterVolume      int   `yaml:"master_volume"`
	ChatSFXOverride   int   `yaml:"chat_sfx_override"`
	PlaybackOverride  int   `yaml:"playback_override"`
	VoiceChatOverride int   `yaml:"voice_chat_override"`
	DenoiseOutEnabled bool  `yaml:"denoise_outbound"`
	DenoiseInEnabled  bool  `yaml:"denoise_inbound"`
	AECEnabled        *bool `yaml:"aec_enabled,omitempty"`
	CompressorLevel   *int  `yaml:"compressor_level,omitempty"`
	JitterBuffer      bool  `yaml:"jitter_buffer"`
	PitchEnabled      bool  `yaml:"pitch_enabled"`
	PitchPos          int   `yaml:"pitch_pos"`
	VibratoEnabled    bool  `yaml:"vibrato_enabled"`
	VibratoFreq       int   `yaml:"vibrato_freq"`
	VibratoRange      int   `yaml:"vibrato_range"`
}

func audioSettingsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".nito", "audio_settings.yaml"), nil
}

// LoadAudioSettings reads ~/.nito/audio_settings.yaml and applies all stored
// values to the in-memory atomics. Missing fields retain their init() defaults.
func LoadAudioSettings() {
	p, err := audioSettingsPath()
	if err != nil {
		return
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return // file doesn't exist yet — keep defaults
	}
	var s AudioSettings
	if err := yaml.Unmarshal(data, &s); err != nil {
		return
	}

	masterVolume.Store(int32(clampVol5(s.MasterVolume)))
	if s.ChatSFXOverride < 0 {
		chatSFXOverride.Store(-1)
	} else {
		chatSFXOverride.Store(int32(clampVol5(s.ChatSFXOverride)))
	}
	if s.PlaybackOverride < 0 {
		playbackOverride.Store(-1)
	} else {
		playbackOverride.Store(int32(clampVol5(s.PlaybackOverride)))
	}
	if s.VoiceChatOverride < 0 {
		voiceChatOverride.Store(-1)
	} else {
		voiceChatOverride.Store(int32(clampVol5(s.VoiceChatOverride)))
	}
	denoiseOutEnabled.Store(s.DenoiseOutEnabled)
	denoiseInEnabled.Store(s.DenoiseInEnabled)
	if s.AECEnabled != nil {
		aecEnabled.Store(*s.AECEnabled)
	}
	if s.CompressorLevel != nil {
		if cl := CompressorLevel(*s.CompressorLevel); cl >= CompressorOff && cl < CompressorCount {
			compressorLevel.Store(int32(cl))
		}
	}
	jitterBufferEnabled.Store(s.JitterBuffer)
	pitchEnabled.Store(s.PitchEnabled)
	if s.PitchPos >= 0 && s.PitchPos <= 24 {
		pitchPos.Store(int32(s.PitchPos))
	}
	vibratoEnabled.Store(s.VibratoEnabled)
	if s.VibratoFreq >= 1 && s.VibratoFreq <= 8 {
		vibratoFreq.Store(int32(s.VibratoFreq))
	}
	if s.VibratoRange >= 1 && s.VibratoRange <= 6 {
		vibratoRange.Store(int32(s.VibratoRange))
	}
}

// SaveAudioSettings writes the current in-memory audio settings to
// ~/.nito/audio_settings.yaml. Errors are silently ignored.
func SaveAudioSettings() {
	p, err := audioSettingsPath()
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	s := AudioSettings{
		MasterVolume:      int(masterVolume.Load()),
		ChatSFXOverride:   int(chatSFXOverride.Load()),
		PlaybackOverride:  int(playbackOverride.Load()),
		VoiceChatOverride: int(voiceChatOverride.Load()),
		DenoiseOutEnabled: denoiseOutEnabled.Load(),
		DenoiseInEnabled:  denoiseInEnabled.Load(),
		AECEnabled:        func() *bool { v := aecEnabled.Load(); return &v }(),
		CompressorLevel:   func() *int { v := int(compressorLevel.Load()); return &v }(),
		JitterBuffer:      jitterBufferEnabled.Load(),
		PitchEnabled:      pitchEnabled.Load(),
		PitchPos:          int(pitchPos.Load()),
		VibratoEnabled:    vibratoEnabled.Load(),
		VibratoFreq:       int(vibratoFreq.Load()),
		VibratoRange:      int(vibratoRange.Load()),
	}
	data, err := yaml.Marshal(s)
	if err != nil {
		return
	}
	_ = os.WriteFile(p, data, 0o600)
}
