// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package voice

// AudioEffect processes a frame of float32 samples in-place.
// Implementations must not retain the frame slice between calls.
type AudioEffect interface {
	Apply(frame []float32)
}

// Enabler is an optional interface that effects may implement to expose their
// enabled/disabled state. EffectPipeline checks this before calling Apply so
// that disabled effects incur zero processing overhead, not just an early return.
type Enabler interface {
	IsEnabled() bool
}

// EffectPipeline is an ordered sequence of AudioEffects applied left-to-right.
// It satisfies AudioEffect itself, so pipelines can be nested.
type EffectPipeline []AudioEffect

// Apply runs each effect in order, skipping any that implement Enabler and
// report IsEnabled() == false.
func (p EffectPipeline) Apply(frame []float32) {
	for _, fx := range p {
		if e, ok := fx.(Enabler); ok && !e.IsEnabled() {
			continue
		}
		fx.Apply(frame)
	}
}

// InboundGain is an AudioEffect that multiplies every sample by a fixed gain.
// Used on the inbound voice path to keep the closed-loop gain below 1.
type InboundGain struct {
	Gain float32 // linear gain, e.g. 0.75 for −2.5 dB
}

// Apply implements AudioEffect.
func (g *InboundGain) Apply(frame []float32) {
	gain := g.Gain
	for i := range frame {
		frame[i] *= gain
	}
}

// InboundPipeline applies an EffectPipeline to a slice of int16 PCM samples
// in-place. It normalises to float32, runs the chain, then converts back. A
// pre-allocated work buffer avoids per-call heap allocation.
type InboundPipeline struct {
	Chain EffectPipeline
	buf   []float32
}

// Process runs the effect chain on samples, modifying it in-place.
func (p *InboundPipeline) Process(samples []int16) {
	if len(samples) == 0 {
		return
	}
	if len(p.buf) < len(samples) {
		p.buf = make([]float32, len(samples))
	}
	buf := p.buf[:len(samples)]
	for i, s := range samples {
		buf[i] = float32(s) / 32768.0
	}
	p.Chain.Apply(buf)
	for i, v := range buf {
		if v > 1.0 {
			v = 1.0
		} else if v < -1.0 {
			v = -1.0
		}
		samples[i] = int16(v * 32767)
	}
}
