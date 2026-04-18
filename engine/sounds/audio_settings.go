// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package sounds

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
	// Playback EQ (player EQ screen)
	EQBassGain      float32 `yaml:"eq_bass_gain"`
	EQBassHz        float32 `yaml:"eq_bass_hz"`
	EQMidGain       float32 `yaml:"eq_mid_gain"`
	EQMidHz         float32 `yaml:"eq_mid_hz"`
	EQMidQ          float32 `yaml:"eq_mid_q"`
	EQTrebGain      float32 `yaml:"eq_treble_gain"`
	EQTrebHz        float32 `yaml:"eq_treble_hz"`
	EQPresGain      float32 `yaml:"eq_pres_gain"`
	EQPresHz        float32 `yaml:"eq_pres_hz"`
	EQPresQ         float32 `yaml:"eq_pres_q"`
	EQVolume        *int    `yaml:"eq_volume,omitempty"`
	DelayEnabled    bool    `yaml:"delay_enabled"`
	DelayDurationMs float32 `yaml:"delay_duration_ms"`
	DelayFeedback   float32 `yaml:"delay_feedback"`
	// Reverb (parallel comb filter bank)
	ReverbEnabled bool    `yaml:"reverb_enabled"`
	ReverbMix     float32 `yaml:"reverb_mix"`
	ReverbSize    float32 `yaml:"reverb_size"`
	ReverbDecay   float32 `yaml:"reverb_decay"`
	ReverbTone    float32 `yaml:"reverb_tone"`
	// Chorus
	ChorusEnabled     bool    `yaml:"chorus_enabled"`
	ChorusBaseDelayMs float32 `yaml:"chorus_base_delay_ms"`
	ChorusRateHz      float32 `yaml:"chorus_rate_hz"`
	ChorusDepthMs     float32 `yaml:"chorus_depth_ms"`
	ChorusMix         float32 `yaml:"chorus_mix"`
	// Playback compressor
	PlaybackCompEnabled     bool    `yaml:"playback_comp_enabled"`
	PlaybackCompThresholdDB float32 `yaml:"playback_comp_threshold_db"`
	PlaybackCompRatio       float32 `yaml:"playback_comp_ratio"`
	// Playback pitch
	PlaybackPitchEnabled   bool    `yaml:"playback_pitch_enabled"`
	PlaybackPitchSemitones float32 `yaml:"playback_pitch_semitones"`
	// Panner (balance + auto-pan); AutoPanRate > 0 distinguishes a saved value.
	PannerBalance        float32 `yaml:"panner_balance"`
	PannerAutoPanEnabled bool    `yaml:"panner_autopan_enabled"`
	PannerAutoPanRate    float32 `yaml:"panner_autopan_rate"`
	PannerAutoPanDepth   float32 `yaml:"panner_autopan_depth"`
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
	// Playback EQ — apply only when at least one EQ field is non-zero (absent in
	// older settings files) to avoid overwriting the in-memory defaults with zeros.
	if s.EQBassHz != 0 || s.EQMidHz != 0 || s.EQTrebHz != 0 {
		eq := EQSettings{
			BassGain: s.EQBassGain, BassHz: s.EQBassHz,
			MidGain: s.EQMidGain, MidHz: s.EQMidHz, MidQ: s.EQMidQ,
			TrebleGain: s.EQTrebGain, TrebleHz: s.EQTrebHz,
			PresenceGain: s.EQPresGain,
			PresenceHz:   s.EQPresHz,
			PresenceQ:    s.EQPresQ,
		}
		// Upgrade path: older files have PresenceHz=0 — apply defaults.
		if eq.PresenceHz == 0 {
			eq.PresenceHz = 3000.0
			eq.PresenceQ = 1.0
		}
		SetPlaybackEQSettings(eq)
	}
	if s.EQVolume != nil {
		SetPlaybackEQVolume(*s.EQVolume)
	}
	// Delay — DurationMs > 0 distinguishes a saved value from an absent field.
	if s.DelayDurationMs > 0 {
		SetDelaySettings(DelaySettings{
			Enabled:  s.DelayEnabled,
			DelayMs:  s.DelayDurationMs,
			Feedback: s.DelayFeedback,
		})
	}
	// Chorus — BaseDelayMs > 0 distinguishes a saved value from an absent field.
	if s.ChorusBaseDelayMs > 0 {
		SetChorusSettings(ChorusSettings{
			Enabled:     s.ChorusEnabled,
			BaseDelayMs: s.ChorusBaseDelayMs,
			RateHz:      s.ChorusRateHz,
			DepthMs:     s.ChorusDepthMs,
			Mix:         s.ChorusMix,
		})
	}
	// Playback compressor — Ratio > 0 distinguishes a saved value from absent.
	if s.PlaybackCompRatio > 0 {
		SetPlaybackCompressorSettings(PlaybackCompressorSettings{
			Enabled:     s.PlaybackCompEnabled,
			ThresholdDB: s.PlaybackCompThresholdDB,
			Ratio:       s.PlaybackCompRatio,
		})
	}
	// Playback pitch — always valid (zero semitones is a legitimate value).
	SetPlaybackPitchSettings(PlaybackPitchSettings{
		Enabled:   s.PlaybackPitchEnabled,
		Semitones: s.PlaybackPitchSemitones,
	})
	// Reverb — Size > 0 distinguishes a saved value from an absent field
	// (Size defaults to 1.0 so any written settings file will have Size != 0).
	if s.ReverbSize > 0 {
		SetReverbSettings(ReverbSettings{
			Enabled: s.ReverbEnabled,
			Mix:     s.ReverbMix,
			Size:    s.ReverbSize,
			Decay:   s.ReverbDecay,
			Tone:    s.ReverbTone,
		})
	}
	// Panner — AutoPanRate > 0 distinguishes a saved value from an absent field.
	if s.PannerAutoPanRate > 0 {
		SetPannerSettings(PannerSettings{
			Balance:        s.PannerBalance,
			AutoPanEnabled: s.PannerAutoPanEnabled,
			AutoPanRate:    s.PannerAutoPanRate,
			AutoPanDepth:   s.PannerAutoPanDepth,
		})
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
	eq := GetPlaybackEQSettings()
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
		EQBassGain:        eq.BassGain, EQBassHz: eq.BassHz,
		EQMidGain: eq.MidGain, EQMidHz: eq.MidHz, EQMidQ: eq.MidQ,
		EQTrebGain: eq.TrebleGain, EQTrebHz: eq.TrebleHz,
		EQPresGain: eq.PresenceGain, EQPresHz: eq.PresenceHz, EQPresQ: eq.PresenceQ,
		EQVolume:                func() *int { v := GetPlaybackEQVolume(); return &v }(),
		DelayEnabled:            func() bool { d := GetDelaySettings(); return d.Enabled }(),
		DelayDurationMs:         func() float32 { d := GetDelaySettings(); return d.DelayMs }(),
		DelayFeedback:           func() float32 { d := GetDelaySettings(); return d.Feedback }(),
		ReverbEnabled:           func() bool { r := GetReverbSettings(); return r.Enabled }(),
		ReverbMix:               func() float32 { r := GetReverbSettings(); return r.Mix }(),
		ReverbSize:              func() float32 { r := GetReverbSettings(); return r.Size }(),
		ReverbDecay:             func() float32 { r := GetReverbSettings(); return r.Decay }(),
		ReverbTone:              func() float32 { r := GetReverbSettings(); return r.Tone }(),
		ChorusEnabled:           func() bool { c := GetChorusSettings(); return c.Enabled }(),
		ChorusBaseDelayMs:       func() float32 { c := GetChorusSettings(); return c.BaseDelayMs }(),
		ChorusRateHz:            func() float32 { c := GetChorusSettings(); return c.RateHz }(),
		ChorusDepthMs:           func() float32 { c := GetChorusSettings(); return c.DepthMs }(),
		ChorusMix:               func() float32 { c := GetChorusSettings(); return c.Mix }(),
		PlaybackCompEnabled:     func() bool { c := GetPlaybackCompressorSettings(); return c.Enabled }(),
		PlaybackCompThresholdDB: func() float32 { c := GetPlaybackCompressorSettings(); return c.ThresholdDB }(),
		PlaybackCompRatio:       func() float32 { c := GetPlaybackCompressorSettings(); return c.Ratio }(),
		PlaybackPitchEnabled:    func() bool { p := GetPlaybackPitchSettings(); return p.Enabled }(),
		PlaybackPitchSemitones:  func() float32 { p := GetPlaybackPitchSettings(); return p.Semitones }(),
		PannerBalance:           func() float32 { p := GetPannerSettings(); return p.Balance }(),
		PannerAutoPanEnabled:    func() bool { p := GetPannerSettings(); return p.AutoPanEnabled }(),
		PannerAutoPanRate:       func() float32 { p := GetPannerSettings(); return p.AutoPanRate }(),
		PannerAutoPanDepth:      func() float32 { p := GetPannerSettings(); return p.AutoPanDepth }(),
	}
	data, err := yaml.Marshal(s)
	if err != nil {
		return
	}
	_ = os.WriteFile(p, data, 0o600)
}
