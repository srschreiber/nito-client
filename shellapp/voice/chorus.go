// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package voice

import "math"

// ChorusSettings configures the LFO-modulated delay chorus effect.
type ChorusSettings struct {
	Enabled     bool
	BaseDelayMs float32 // 5–30 ms; center of modulated delay
	RateHz      float32 // 0.1–5.0 Hz; LFO speed
	DepthMs     float32 // 0–15 ms; half-range of delay modulation
	Mix         float32 // 0.0–1.0; wet/dry crossfade
}

// SetDefaults applies sensible defaults to ChorusSettings.
func (s *ChorusSettings) SetDefaults() {
	s.Enabled = false
	s.BaseDelayMs = 20
	s.RateHz = 0.6
	s.DepthMs = 2.5
	s.Mix = 0.20
}

// maxChorusDelaySamples is the ring-buffer capacity: ~104 ms at 48 kHz.
const maxChorusDelaySamples = 5000

// Chorus implements an LFO-modulated delay chorus effect for a single audio
// channel. It satisfies the AudioEffect interface.
type Chorus struct {
	Settings       ChorusSettings
	buf            [maxChorusDelaySamples]float32 // ring buffer of dry input
	writePos       int
	lfoPhase       float64
	baseDelaySamps float32
	depthSamps     float32
	lfoIncrement   float64 // 2π × rate / sampleRate
}

// IsEnabled implements Enabler.
func (c *Chorus) IsEnabled() bool { return c.Settings.Enabled }

// UpdateSettings recomputes baseDelaySamps, depthSamps, and lfoIncrement from
// Settings. Call whenever Settings or sampleRate changes.
func (c *Chorus) UpdateSettings(sampleRate float32) {
	c.baseDelaySamps = sampleRate * c.Settings.BaseDelayMs / 1000.0
	c.depthSamps = sampleRate * c.Settings.DepthMs / 1000.0
	c.lfoIncrement = 2.0 * math.Pi * float64(c.Settings.RateHz) / float64(sampleRate)
}

// Apply implements AudioEffect. It processes frame in-place using the chorus
// algorithm. If Settings.Enabled is false the frame is passed through unchanged.
func (c *Chorus) Apply(frame []float32) {
	if !c.Settings.Enabled {
		return
	}
	mix := c.Settings.Mix
	size := maxChorusDelaySamples

	for i, x := range frame {
		// LFO value and phase advance.
		lfoVal := float32(math.Sin(c.lfoPhase))
		c.lfoPhase += c.lfoIncrement
		if c.lfoPhase >= 2.0*math.Pi {
			c.lfoPhase -= 2.0 * math.Pi
		}

		// Modulated delay in samples (clamp to at least 1).
		delaySampsF := c.baseDelaySamps + c.depthSamps*lfoVal
		if delaySampsF < 1.0 {
			delaySampsF = 1.0
		}

		// Linear interpolation between adjacent ring-buffer samples.
		delayInt := int(delaySampsF)
		delayFrac := delaySampsF - float32(delayInt)
		pos0 := (c.writePos - delayInt + size) % size
		pos1 := (c.writePos - delayInt - 1 + size) % size
		delayed := c.buf[pos0]*(1-delayFrac) + c.buf[pos1]*delayFrac

		// Store dry sample, advance write position.
		c.buf[c.writePos] = x
		c.writePos = (c.writePos + 1) % size

		// Wet/dry mix.
		frame[i] = (1-mix)*x + mix*delayed
	}
}
