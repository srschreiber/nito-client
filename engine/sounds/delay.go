// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package sounds

/*
Feedback comb filter delay.

Difference equation (per channel):

	y[n] = x[n] + feedback × y[n − D]

where D is the delay in samples.  Each output sample is the sum of the dry
input and a delayed copy of a previous output, attenuated by feedback [0, 1).
For |feedback| < 1 the filter is unconditionally stable — feedback < 1 means
each echo is quieter than the last, so the impulse response decays to zero.

Ring buffer layout:

	buf[0 … maxDelaySamples-1]  — circular store of past output samples
	writePos                    — next position to write

To read y[n − D], compute:

	readPos = (writePos − D + maxDelaySamples) % maxDelaySamples

The buffer capacity is fixed at maxDelaySamples regardless of the current
delay setting.  Changing DelayMs at runtime only updates delaySamps — no
allocation, no flush.  The buffer will contain stale samples from the previous
delay for up to max-delay samples (≈ 500 ms), audible as a brief artefact
when the delay is changed live.
*/

// DelaySettings configures the feedback comb filter delay effect.
type DelaySettings struct {
	Enabled  bool
	DelayMs  float32 // 1–500 ms; sets the echo spacing
	Feedback float32 // 0.00–0.95; attenuation per echo (closer to 1 = longer tail)
}

func (s *DelaySettings) SetDefaults() {
	s.Enabled = false
	s.DelayMs = 55.0
	s.Feedback = 0.35
}

// maxDelaySamples is the ring-buffer capacity: 500 ms at 48 kHz.
// Keeping it fixed lets the delay be changed live without reallocation.
const maxDelaySamples = 24000

// Delay implements a feedback comb filter for a single audio channel.
// It satisfies the AudioEffect interface.
type Delay struct {
	Settings   DelaySettings
	buf        [maxDelaySamples]float32
	writePos   int
	delaySamps int
}

// IsEnabled implements Enabler.
func (r *Delay) IsEnabled() bool { return r.Settings.Enabled }

// UpdateSettings recomputes delaySamps from Settings.DelayMs and sampleRate.
// Call whenever Settings or sampleRate changes.
func (r *Delay) UpdateSettings(sampleRate float32) {
	ms := r.Settings.DelayMs
	if ms < 1 {
		ms = 1
	}
	r.delaySamps = int(sampleRate * ms / 1000.0)
	if r.delaySamps < 1 {
		r.delaySamps = 1
	}
	if r.delaySamps > maxDelaySamples {
		r.delaySamps = maxDelaySamples
	}
}

// Apply implements AudioEffect. It processes frame in-place using the comb
// filter equation y[n] = x[n] + feedback × y[n − D].  If Settings.Enabled
// is false the frame is passed through unchanged.
func (r *Delay) Apply(frame []float32) {
	if !r.Settings.Enabled {
		return
	}
	fb := r.Settings.Feedback
	D := r.delaySamps
	size := maxDelaySamples
	for i, x := range frame {
		readPos := (r.writePos - D + size) % size
		y := x + fb*r.buf[readPos]
		r.buf[r.writePos] = y
		r.writePos = (r.writePos + 1) % size
		frame[i] = y
	}
}
