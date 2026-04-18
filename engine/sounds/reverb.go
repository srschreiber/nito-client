// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package sounds

/*
Parallel comb filter reverb.

Four feedback comb filters with one-pole low-pass damping run in parallel:

	for each comb filter i:
	    y[n]       = x[n] + buf_i[n - D_i]
	    damp_i[n]  = y[n]*(1 - d_i) + damp_i[n-1]*d_i   // one-pole LPF
	    buf_i[n]   = damp_i[n] * fb_i

	wet[n] = mean(y_0[n], y_1[n], y_2[n], y_3[n])
	out[n] = (1 - mix)*x[n] + mix*wet[n]

Base parameters (used when Size=1, Decay=0.6, Tone=0.5):

	delays:   31 ms, 43 ms, 59 ms, 71 ms
	feedback: 0.55, 0.60, 0.65, 0.58
	damping:  0.12, 0.15, 0.18, 0.14

User controls:
	Mix   — wet/dry blend (0.0–1.0)
	Size  — scales delay times (0.5–2.0); larger = roomier
	Decay — scales feedback (0.0–1.0); higher = longer tail
	Tone  — damping blend (0.0=dark, 1.0=bright)

The four prime-ish delay values prevent resonant combing and give a
richer, more diffuse tail than a single echo would.
*/

const reverbNumFilters = 4
const maxReverbDelaySamples = 9600 // 200 ms at 48 kHz; accommodates Size=2 × 71 ms

var (
	reverbBaseDelayMs  = [reverbNumFilters]float32{31, 43, 59, 71}
	reverbBaseFeedback = [reverbNumFilters]float32{0.55, 0.60, 0.65, 0.58}
	reverbBaseDamping  = [reverbNumFilters]float32{0.12, 0.15, 0.18, 0.14}
)

// ReverbSettings configures the parallel comb filter reverb.
type ReverbSettings struct {
	Enabled bool
	Mix     float32 // 0.0–1.0; wet/dry blend
	Size    float32 // 0.5–2.0; scales delay times (room size)
	Decay   float32 // 0.0–1.0; scales feedback (tail length); base at 0.6
	Tone    float32 // 0.0–1.0; 0=dark (high damping), 1=bright (low damping)
}

// SetDefaults applies sensible defaults to ReverbSettings.
func (s *ReverbSettings) SetDefaults() {
	s.Enabled = false
	s.Mix = 0.18
	s.Size = 1.0
	s.Decay = 0.6
	s.Tone = 0.5
}

// combFilter is one feedback comb filter with one-pole LPF damping.
type combFilter struct {
	buf        [maxReverbDelaySamples]float32
	writePos   int
	dampState  float32 // LPF state
	delaySamps int
	feedback   float32
	damping    float32 // 0=no LPF (bright), higher=more LPF (dark)
}

// Reverb implements the parallel comb filter reverb. It satisfies the
// AudioEffect interface.
type Reverb struct {
	Settings ReverbSettings
	filters  [reverbNumFilters]combFilter
}

// IsEnabled implements Enabler.
func (r *Reverb) IsEnabled() bool { return r.Settings.Enabled }

// UpdateSettings recomputes per-filter delay, feedback, and damping from
// Settings and sampleRate. Call whenever Settings or sampleRate changes.
func (r *Reverb) UpdateSettings(sampleRate float32) {
	for i := 0; i < reverbNumFilters; i++ {
		cf := &r.filters[i]
		// Delay: base × Size
		delayMs := reverbBaseDelayMs[i] * r.Settings.Size
		cf.delaySamps = int(sampleRate * delayMs / 1000.0)
		if cf.delaySamps < 1 {
			cf.delaySamps = 1
		}
		if cf.delaySamps >= maxReverbDelaySamples {
			cf.delaySamps = maxReverbDelaySamples - 1
		}
		// Feedback: base × (Decay / 0.6), capped at 0.95
		cf.feedback = reverbBaseFeedback[i] * r.Settings.Decay / 0.6
		if cf.feedback > 0.95 {
			cf.feedback = 0.95
		}
		if cf.feedback < 0 {
			cf.feedback = 0
		}
		// Damping: base × (1 - Tone); Tone=1 → no damping (bright), Tone=0 → full base damping (dark)
		cf.damping = reverbBaseDamping[i] * (1.0 - r.Settings.Tone)
	}
}

// Apply implements AudioEffect. It processes frame in-place. If
// Settings.Enabled is false the frame is passed through unchanged.
func (r *Reverb) Apply(frame []float32) {
	if !r.Settings.Enabled {
		return
	}
	mix := r.Settings.Mix
	size := maxReverbDelaySamples

	for i, x := range frame {
		var wet float32
		for f := 0; f < reverbNumFilters; f++ {
			cf := &r.filters[f]
			readPos := (cf.writePos - cf.delaySamps + size) % size
			y := x + cf.buf[readPos]
			// One-pole LPF in feedback path.
			cf.dampState = y*(1-cf.damping) + cf.dampState*cf.damping
			cf.buf[cf.writePos] = cf.dampState * cf.feedback
			cf.writePos = (cf.writePos + 1) % size
			wet += y
		}
		wet /= float32(reverbNumFilters)
		frame[i] = (1-mix)*x + mix*wet
	}
}
