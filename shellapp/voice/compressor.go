// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

// inboundCompressor is a per-track dynamic range compressor applied to
// received voice audio before it reaches the speaker.
//
// It prevents sudden loud bursts (e.g. someone yelling or a mic spike) from
// exceeding a comfortable level while leaving normal-volume speech unaffected.
// One instance is created per incoming audio track so that multiple peers in a
// call each have independent compression state.
package voice

import (
	"math"
	"sync/atomic"
)

// CompressorLevel controls how aggressively inbound audio is compressed.
type CompressorLevel int32

const (
	CompressorOff    CompressorLevel = 0
	CompressorLow    CompressorLevel = 1
	CompressorMedium CompressorLevel = 2
	CompressorHigh   CompressorLevel = 3
	CompressorCount  CompressorLevel = 4 // sentinel; number of valid levels
)

// CompressorLevelNames maps each level to its display name.
var CompressorLevelNames = [4]string{"off", "low", "medium", "high"}

// compressorLevel is the active compressor setting, readable by all track goroutines.
var compressorLevel atomic.Int32 // stores CompressorLevel; default CompressorMedium set in init()

func init() {
	compressorLevel.Store(int32(CompressorMedium))
}

// CompressorLevel returns the current compressor level.
func GetCompressorLevel() CompressorLevel { return CompressorLevel(compressorLevel.Load()) }

// SetCompressorLevel updates the compressor level. Takes effect on the next audio frame.
func SetCompressorLevel(l CompressorLevel) {
	if l < CompressorOff {
		l = CompressorOff
	} else if l > CompressorHigh {
		l = CompressorHigh
	}
	compressorLevel.Store(int32(l))
}

// compressorPreset holds the parameters for one compression level.
type compressorPreset struct {
	thresholdLin float32 // RMS threshold in linear scale (0–1)
	ratio        float32 // compression ratio (e.g. 4.0 = 4:1)
}

// compressorPresets defines the parameters for each CompressorLevel.
// Threshold in dBFS: low = −9, medium = −15, high = −21.
// Normal conversational speech sits around −20 to −15 dBFS, so low and medium
// leave it untouched and only catch genuine loud bursts.
var compressorPresets = [4]compressorPreset{
	{},           // off — unused
	{0.355, 2.0}, // low:    −9 dBFS, 2:1 — barely perceptible, clips only sharp peaks
	{0.178, 3.0}, // medium: −15 dBFS, 3:1 — comfortable ceiling, normal speech unaffected
	{0.089, 6.0}, // high:   −21 dBFS, 6:1 — tighter range, aggressive burst control
}

const (
	// compAttackCoef is the EMA coefficient per 20 ms Opus frame for rising levels.
	// 0.5 → ~2-frame (40 ms) response time; fast enough to catch sudden bursts.
	compAttackCoef = 0.5
	// compReleaseCoef is the EMA coefficient per 20 ms frame for falling levels.
	// 0.95 → ~20-frame (400 ms) recovery; slow enough to avoid pumping artifacts.
	compReleaseCoef = 0.95
)

type inboundCompressor struct {
	envelope float32 // smoothed RMS envelope, normalised to [0, 1]
}

// process applies compression in-place to a slice of int16 PCM samples.
//
// Algorithm:
//  1. Read current CompressorLevel; return immediately if off.
//  2. Compute the frame RMS (normalised 0–1).
//  3. Update the smoothed envelope with asymmetric attack/release EMA.
//  4. When envelope > threshold: gain = (threshold/envelope)^(1 − 1/ratio)
//     — standard feed-forward gain in linear domain derived from the dB formula:
//     outputLevel = threshold + (inputLevel − threshold) / ratio.
//  5. Multiply every sample by gain and clamp to int16 range.
func (c *inboundCompressor) process(samples []int16) {
	level := CompressorLevel(compressorLevel.Load())
	if level == CompressorOff || len(samples) == 0 {
		return
	}
	preset := compressorPresets[level]

	// Step 2: frame RMS.
	var sumSq float64
	for _, s := range samples {
		f := float64(s) / 32768.0
		sumSq += f * f
	}
	rms := float32(math.Sqrt(sumSq / float64(len(samples))))

	// Step 3: asymmetric envelope follower.
	if rms > c.envelope {
		c.envelope = compAttackCoef*c.envelope + (1-compAttackCoef)*rms
	} else {
		c.envelope = compReleaseCoef*c.envelope + (1-compReleaseCoef)*rms
	}

	// Step 4: gain reduction.
	// Below threshold: gain = 1.0 (no change).
	// Above threshold: gain = (threshold/envelope)^(1 − 1/ratio).
	gain := float32(1.0)
	if c.envelope > preset.thresholdLin {
		gain = float32(math.Pow(
			float64(preset.thresholdLin/c.envelope),
			1.0-1.0/float64(preset.ratio),
		))
	}

	// Step 5: apply gain with int16 clamp.
	for i, s := range samples {
		v := float32(s) * gain
		if v > 32767 {
			v = 32767
		} else if v < -32768 {
			v = -32768
		}
		samples[i] = int16(v)
	}
}
