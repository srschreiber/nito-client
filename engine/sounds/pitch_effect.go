// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package sounds

// PlaybackPitchSettings configures the playback-path pitch shift effect.
type PlaybackPitchSettings struct {
	Enabled   bool
	Semitones float32 // -12.0 to +12.0
}

// SetDefaults applies sensible defaults to PlaybackPitchSettings.
func (s *PlaybackPitchSettings) SetDefaults() {
	s.Enabled = false
	s.Semitones = 0
}

// PlaybackPitchEffect wraps an ssStretch instance for per-channel pitch
// shifting on the playback path. It satisfies the AudioEffect interface.
type PlaybackPitchEffect struct {
	Settings PlaybackPitchSettings
	stretch  *ssStretch // nil if init failed; passthrough in that case
	outBuf   []float32
}

// NewPlaybackPitchEffect creates a PlaybackPitchEffect for the given sample
// rate. If the underlying ssStretch cannot be initialised (e.g. CGo not
// available), a passthrough instance is returned with a nil stretch.
// Exported so it can be called from the components package.
func NewPlaybackPitchEffect(sampleRate int) *PlaybackPitchEffect {
	s, err := newSSStretch(sampleRate, 1, 2048, 512)
	if err != nil {
		return &PlaybackPitchEffect{} // nil stretch = passthrough
	}
	return &PlaybackPitchEffect{stretch: s}
}

// IsEnabled implements Enabler.
func (p *PlaybackPitchEffect) IsEnabled() bool { return p.Settings.Enabled && p.stretch != nil }

// UpdateSettings propagates Settings into the underlying ssStretch.
// If the effect is disabled, semitones is forced to 0 (no shift).
func (p *PlaybackPitchEffect) UpdateSettings() {
	if p.stretch == nil {
		return
	}
	semitones := p.Settings.Semitones
	if !p.Settings.Enabled {
		semitones = 0
	}
	p.stretch.setSemitones(semitones)
}

// Apply implements AudioEffect. It pitch-shifts frame in-place. If the effect
// is disabled, the stretch ratio is 0 semitones, or the stretch instance is
// nil, the frame is left unchanged.
func (p *PlaybackPitchEffect) Apply(frame []float32) {
	if !p.Settings.Enabled || p.stretch == nil || p.Settings.Semitones == 0 {
		return
	}
	// Grow output buffer if needed.
	if cap(p.outBuf) < len(frame) {
		p.outBuf = make([]float32, len(frame))
	}
	out := p.outBuf[:len(frame)]
	p.stretch.process(frame, out)
	copy(frame, out)
}

// Close releases the underlying CGo ssStretch resources.
// Must be called when the effect is no longer needed.
func (p *PlaybackPitchEffect) Close() {
	if p.stretch != nil {
		p.stretch.close()
		p.stretch = nil
	}
}
