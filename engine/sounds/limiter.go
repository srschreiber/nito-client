// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package sounds

/*
Peak limiter — always-on, zero-latency, transparent at normal levels.

Algorithm: feed-forward peak limiter with instantaneous attack and
EMA (exponential moving average) release.

	abs[n]          = |x[n]|
	envelope[n]     = abs[n]                                  if abs[n] > envelope[n-1]  (attack)
	                = α×abs[n] + (1−α)×envelope[n-1]         otherwise                  (release)
	gain[n]         = min(1, ceiling / envelope[n])
	y[n]            = x[n] × gain[n]

Instantaneous attack means the gain is applied in the same sample that the
peak is detected, so there is never a transient above the ceiling.

EMA release (α = limiterReleaseAlpha) lets the envelope track the current
signal level rather than decaying all the way to zero. This prevents
over-suppression of sustained loud material that stays below the ceiling.
At 48 kHz, α = 0.0002 gives a τ ≈ 104 ms (63% recovery time), which avoids
audible gain pumping while recovering promptly after transients.

The ceiling is set at 0.98 (≈ −0.18 dBFS) rather than exactly 1.0 to
leave a tiny margin for any floating-point rounding.
*/

const limiterCeiling = float32(0.98)

// limiterReleaseAlpha is the EMA coefficient for envelope release.
// Lower = slower release. At 48 kHz, 0.0002 ≈ 104 ms τ.
const limiterReleaseAlpha = float32(0.0002)

// PeakLimiter is a zero-latency peak limiter for a single audio channel.
// It implements AudioEffect and is intended as the last stage of the
// playback effect chain, before the final output-gain multiply and soft clamp.
type PeakLimiter struct {
	envelope float32
}

// Apply implements AudioEffect. Modifies frame in-place.
func (l *PeakLimiter) Apply(frame []float32) {
	for i, x := range frame {
		abs := x
		if abs < 0 {
			abs = -abs
		}
		// Instantaneous attack, EMA release toward current abs.
		if abs > l.envelope {
			l.envelope = abs
		} else {
			l.envelope = limiterReleaseAlpha*abs + (1-limiterReleaseAlpha)*l.envelope
		}

		gain := float32(1.0)
		if l.envelope > limiterCeiling {
			gain = limiterCeiling / l.envelope
		}
		frame[i] = x * gain
	}
}
