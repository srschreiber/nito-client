// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package sounds

// PannerSettings configures stereo balance and auto-pan for the music playback path.
// Balance is a static L/R offset; auto-pan modulates balance over time with a sine LFO.
// The two are additive: the effective balance at time t is
//
//	Balance + AutoPanDepth * sin(2π * AutoPanRate * t)  (clamped to [-1, 1])
//
// Gain law: angle = (balance + 1) * π / 4
//
//	leftGain  = cos(angle)   rightGain = sin(angle)
//
// This is a constant-power pan law: L²+R²=1 at every position.
type PannerSettings struct {
	Balance        float32 // -1.0 (full left) … 0.0 (center) … +1.0 (full right)
	AutoPanEnabled bool
	AutoPanRate    float32 // Hz; 0.05–5.0; LFO speed
	AutoPanDepth   float32 // 0.0–1.0; half-range of the pan sweep
}

// SetDefaults applies sensible defaults to PannerSettings.
func (s *PannerSettings) SetDefaults() {
	s.Balance = 0
	s.AutoPanEnabled = false
	s.AutoPanRate = 0.5
	s.AutoPanDepth = 0.5
}
