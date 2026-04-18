// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package components

import (
	"github.com/srschreiber/nito-client/engine/sounds"
)

// ShowAudioPlayerPresetsMsg opens the preset selector screen.
type ShowAudioPlayerPresetsMsg struct{}

// HideAudioPlayerPresetsMsg closes the preset selector screen.
type HideAudioPlayerPresetsMsg struct{}

// audioPreset defines one named preset.
type audioPreset struct {
	name    string // display name
	tagline string // one-line vibe description
	tags    string // active effects summary e.g. "EQ · Reverb"
	apply   func() // applies all settings and saves
}

// noFX returns sensible "off" states for delay/reverb/chorus/pan.
// Preset apply functions call this then layer any FX they need on top.
func noFX() (sounds.DelaySettings, sounds.ReverbSettings, sounds.ChorusSettings, sounds.PannerSettings) {
	del := sounds.DelaySettings{Enabled: false, DelayMs: 55, Feedback: 0.3}
	rev := sounds.ReverbSettings{Enabled: false, Mix: 0.2, Size: 1.0, Decay: 0.5, Tone: 0.5}
	cho := sounds.ChorusSettings{Enabled: false, BaseDelayMs: 15, RateHz: 0.5, DepthMs: 3.0, Mix: 0.2}
	// AutoPanRate must be >0 so LoadAudioSettings recognises the field as written.
	pan := sounds.PannerSettings{Balance: 0, AutoPanEnabled: false, AutoPanRate: 1.0, AutoPanDepth: 0.5}
	return del, rev, cho, pan
}

// applyPreset sets EQ + FX settings and saves, preserving volume and pitch.
func applyPreset(eq sounds.EQSettings,
	del sounds.DelaySettings, rev sounds.ReverbSettings,
	cho sounds.ChorusSettings, pan sounds.PannerSettings) {
	sounds.SetPlaybackEQSettings(eq)
	sounds.SetDelaySettings(del)
	sounds.SetReverbSettings(rev)
	sounds.SetChorusSettings(cho)
	sounds.SetPannerSettings(pan)
	sounds.SaveAudioSettings()
}

// audioPresetList contains all available presets in alphabetical order.
var audioPresetList = []audioPreset{
	// ── Airy ─────────────────────────────────────────────────────────────────
	{
		name:    "Airy",
		tagline: "Light & ethereal",
		tags:    "EQ · Chorus",
		apply: func() {
			del, rev, _, pan := noFX()
			cho := sounds.ChorusSettings{
				Enabled: true, BaseDelayMs: 22, RateHz: 0.25, DepthMs: 2.5, Mix: 0.14,
			}
			applyPreset(sounds.EQSettings{
				BassGain: -2.0, BassHz: 120,
				MidGain: -1.0, MidHz: 1000, MidQ: 1.0,
				TrebleGain: 3.0, TrebleHz: 10000,
				PresenceGain: 3.0, PresenceHz: 4500, PresenceQ: 0.9,
			}, del, rev, cho, pan)
		},
	},
	// ── Auto Pan ─────────────────────────────────────────────────────────────
	{
		name:    "Auto Pan",
		tagline: "Rhythmic stereo sweep",
		tags:    "Auto-Pan",
		apply: func() {
			del, rev, cho, _ := noFX()
			pan := sounds.PannerSettings{
				Balance: 0, AutoPanEnabled: true, AutoPanRate: 1.5, AutoPanDepth: 0.7,
			}
			applyPreset(sounds.EQSettings{
				BassGain: 0.0, BassHz: 120,
				MidGain: 0.0, MidHz: 1000, MidQ: 1.0,
				TrebleGain: 0.5, TrebleHz: 5000,
				PresenceGain: 1.0, PresenceHz: 3000, PresenceQ: 1.0,
			}, del, rev, cho, pan)
		},
	},
	// ── Balanced ─────────────────────────────────────────────────────────────
	{
		name:    "Balanced",
		tagline: "Gentle V-curve",
		tags:    "EQ only",
		apply: func() {
			del, rev, cho, pan := noFX()
			applyPreset(sounds.EQSettings{
				BassGain: 2.0, BassHz: 120,
				MidGain: -0.5, MidHz: 1000, MidQ: 1.0,
				TrebleGain: 1.5, TrebleHz: 5000,
				PresenceGain: 2.0, PresenceHz: 3000, PresenceQ: 1.0,
			}, del, rev, cho, pan)
		},
	},
	// ── Bass Boost ───────────────────────────────────────────────────────────
	{
		name:    "Bass Boost",
		tagline: "Heavy low end",
		tags:    "EQ only",
		apply: func() {
			del, rev, cho, pan := noFX()
			applyPreset(sounds.EQSettings{
				BassGain: 3.0, BassHz: 80,
				MidGain: -1.0, MidHz: 1000, MidQ: 1.0,
				TrebleGain: 0.0, TrebleHz: 5000,
				PresenceGain: 0.0, PresenceHz: 3000, PresenceQ: 1.0,
			}, del, rev, cho, pan)
		},
	},
	// ── Bright ───────────────────────────────────────────────────────────────
	{
		name:    "Bright",
		tagline: "Crispy highs",
		tags:    "EQ only",
		apply: func() {
			del, rev, cho, pan := noFX()
			applyPreset(sounds.EQSettings{
				BassGain: -1.0, BassHz: 120,
				MidGain: 0.0, MidHz: 1000, MidQ: 1.0,
				TrebleGain: 3.0, TrebleHz: 9000,
				PresenceGain: 3.0, PresenceHz: 4000, PresenceQ: 1.2,
			}, del, rev, cho, pan)
		},
	},
	// ── Chill ────────────────────────────────────────────────────────────────
	{
		name:    "Chill",
		tagline: "Warm & relaxed",
		tags:    "EQ · Reverb",
		apply: func() {
			del, _, cho, pan := noFX()
			rev := sounds.ReverbSettings{
				Enabled: true, Mix: 0.15, Size: 1.3, Decay: 0.55, Tone: 0.35,
			}
			applyPreset(sounds.EQSettings{
				BassGain: 2.5, BassHz: 100,
				MidGain: -0.5, MidHz: 1000, MidQ: 1.0,
				TrebleGain: -2.5, TrebleHz: 5000,
				PresenceGain: -1.0, PresenceHz: 3000, PresenceQ: 1.0,
			}, del, rev, cho, pan)
		},
	},
	// ── Delay Echo ───────────────────────────────────────────────────────────
	{
		name:    "Delay Echo",
		tagline: "Bouncy echo repeats",
		tags:    "EQ · Delay",
		apply: func() {
			_, rev, cho, pan := noFX()
			del := sounds.DelaySettings{Enabled: true, DelayMs: 250, Feedback: 0.42}
			applyPreset(sounds.EQSettings{
				BassGain: 0.5, BassHz: 120,
				MidGain: -0.5, MidHz: 1000, MidQ: 1.0,
				TrebleGain: 2.0, TrebleHz: 7000,
				PresenceGain: 1.5, PresenceHz: 3500, PresenceQ: 1.0,
			}, del, rev, cho, pan)
		},
	},
	// ── Flat ─────────────────────────────────────────────────────────────────
	{
		name:    "Flat",
		tagline: "No processing",
		tags:    "Clean slate",
		apply: func() {
			del, rev, cho, pan := noFX()
			applyPreset(sounds.EQSettings{
				BassGain: 0.0, BassHz: 120,
				MidGain: 0.0, MidHz: 1000, MidQ: 1.0,
				TrebleGain: 0.0, TrebleHz: 5000,
				PresenceGain: 0.0, PresenceHz: 3000, PresenceQ: 1.0,
			}, del, rev, cho, pan)
		},
	},
	// ── Immersive ────────────────────────────────────────────────────────────
	{
		name:    "Immersive",
		tagline: "Spacious & enveloping",
		tags:    "EQ · Reverb · Chorus",
		apply: func() {
			del, _, _, _ := noFX()
			pan := sounds.PannerSettings{Balance: 0, AutoPanEnabled: true, AutoPanRate: 0.3, AutoPanDepth: 0.3}
			rev := sounds.ReverbSettings{
				Enabled: true, Mix: 0.28, Size: 1.8, Decay: 0.72, Tone: 0.55,
			}
			cho := sounds.ChorusSettings{
				Enabled: true, BaseDelayMs: 18, RateHz: 0.35, DepthMs: 3.5, Mix: 0.18,
			}
			applyPreset(sounds.EQSettings{
				BassGain: 2.0, BassHz: 120,
				MidGain: -0.5, MidHz: 1000, MidQ: 1.0,
				TrebleGain: 2.0, TrebleHz: 5000,
				PresenceGain: 2.5, PresenceHz: 3500, PresenceQ: 1.0,
			}, del, rev, cho, pan)
		},
	},
	// ── Lo-Fi ────────────────────────────────────────────────────────────────
	{
		name:    "Lo-Fi",
		tagline: "Warm, dusty & nostalgic",
		tags:    "EQ · Reverb · Delay",
		apply: func() {
			_, _, cho, pan := noFX()
			del := sounds.DelaySettings{Enabled: true, DelayMs: 80, Feedback: 0.2}
			rev := sounds.ReverbSettings{Enabled: true, Mix: 0.2, Size: 1.2, Decay: 0.65, Tone: 0.3}
			applyPreset(sounds.EQSettings{
				BassGain: 2.0, BassHz: 110,
				MidGain: -1.0, MidHz: 400, MidQ: 1.2,
				TrebleGain: -3.0, TrebleHz: 8000,
				PresenceGain: -3.0, PresenceHz: 3500, PresenceQ: 0.9,
			}, del, rev, cho, pan)
		},
	},
	// ── Punchy ───────────────────────────────────────────────────────────────
	{
		name:    "Punchy",
		tagline: "Tight bass, forward mids",
		tags:    "EQ only",
		apply: func() {
			del, rev, cho, pan := noFX()
			applyPreset(sounds.EQSettings{
				BassGain: 3.0, BassHz: 80,
				MidGain: 1.5, MidHz: 700, MidQ: 1.5,
				TrebleGain: 2.0, TrebleHz: 5000,
				PresenceGain: 2.5, PresenceHz: 3000, PresenceQ: 1.4,
			}, del, rev, cho, pan)
		},
	},
	// ── Techno ───────────────────────────────────────────────────────────────
	{
		name:    "Techno",
		tagline: "Full FX chain, high energy",
		tags:    "EQ · Reverb · Chorus · Delay · Pan",
		apply: func() {
			del := sounds.DelaySettings{Enabled: true, DelayMs: 120, Feedback: 0.35}
			rev := sounds.ReverbSettings{Enabled: true, Mix: 0.25, Size: 2.0, Decay: 0.70, Tone: 0.6}
			cho := sounds.ChorusSettings{Enabled: true, BaseDelayMs: 12, RateHz: 0.5, DepthMs: 2.0, Mix: 0.12}
			pan := sounds.PannerSettings{Balance: 0, AutoPanEnabled: true, AutoPanRate: 0.5, AutoPanDepth: 0.4}
			applyPreset(sounds.EQSettings{
				BassGain: 3.0, BassHz: 80,
				MidGain: -3.0, MidHz: 500, MidQ: 1.2,
				TrebleGain: 3.0, TrebleHz: 8000,
				PresenceGain: 3.0, PresenceHz: 4000, PresenceQ: 1.0,
			}, del, rev, cho, pan)
		},
	},
	// ── Vocal Boost ──────────────────────────────────────────────────────────
	{
		name:    "Vocal Boost",
		tagline: "Forward mids & presence",
		tags:    "EQ only",
		apply: func() {
			del, rev, cho, pan := noFX()
			applyPreset(sounds.EQSettings{
				BassGain: -2.0, BassHz: 120,
				MidGain: 2.5, MidHz: 800, MidQ: 0.8,
				TrebleGain: 1.0, TrebleHz: 5000,
				PresenceGain: 3.0, PresenceHz: 3000, PresenceQ: 1.2,
			}, del, rev, cho, pan)
		},
	},
	// ── Warm ─────────────────────────────────────────────────────────────────
	{
		name:    "Warm",
		tagline: "Full bass, rolled highs",
		tags:    "EQ only",
		apply: func() {
			del, rev, cho, pan := noFX()
			applyPreset(sounds.EQSettings{
				BassGain: 3.0, BassHz: 110,
				MidGain: 1.0, MidHz: 1000, MidQ: 1.0,
				TrebleGain: -3.0, TrebleHz: 6000,
				PresenceGain: -2.0, PresenceHz: 3000, PresenceQ: 1.0,
			}, del, rev, cho, pan)
		},
	},
}

// ── Public exports ────────────────────────────────────────────────────────────

// AudioPresetInfo is a public descriptor for a single built-in preset.
type AudioPresetInfo struct {
	Name    string
	Tagline string
	Tags    string
}

// ListBuiltinPresets returns info for every built-in audio preset in order.
func ListBuiltinPresets() []AudioPresetInfo {
	out := make([]AudioPresetInfo, len(audioPresetList))
	for i, p := range audioPresetList {
		out[i] = AudioPresetInfo{Name: p.name, Tagline: p.tagline, Tags: p.tags}
	}
	return out
}

// ApplyPresetByName applies the built-in preset with the given name, returning
// its tagline and tags. Returns ok=false when no preset with that name exists.
func ApplyPresetByName(name string) (tagline, tags string, ok bool) {
	for _, p := range audioPresetList {
		if p.name == name {
			p.apply()
			return p.tagline, p.tags, true
		}
	}
	return "", "", false
}

// ── Display preset (built-in + custom unified) ────────────────────────────────

type displayPreset struct {
	name     string
	tagline  string
	tags     string
	isCustom bool
	apply    func()
}

func buildDisplayPresets(custom []sounds.PlayerPreset) []displayPreset {
	out := make([]displayPreset, 0, len(audioPresetList)+len(custom))
	for _, p := range audioPresetList {
		out = append(out, displayPreset{
			name: p.name, tagline: p.tagline, tags: p.tags, apply: p.apply,
		})
	}
	for _, cp := range custom {
		cp := cp // capture
		out = append(out, displayPreset{
			name:     cp.Name,
			tagline:  "Custom preset",
			tags:     "Custom",
			isCustom: true,
			apply:    func() { cp.Apply() },
		})
	}
	return out
}
