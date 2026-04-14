// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

// C wrapper around signalsmith-stretch (MIT) for real-time pitch shifting.
// Header-only C++ library vendored in signalsmith/.

#include <cstring>
#include "signalsmith/signalsmith-stretch.h"
#include <cstdlib>
#include <cstring>
#include <vector>

using Stretch = signalsmith::stretch::SignalsmithStretch<float>;

extern "C" {

Stretch* ss_new(int channels, int sampleRate, int blockSamples, int intervalSamples) {
	Stretch* s = new Stretch();
	s->configure(channels, blockSamples, intervalSamples);
	return s;
}

void ss_delete(Stretch* s) {
	delete s;
}

void ss_set_semitones(Stretch* s, float semitones) {
	s->setTransposeSemitones(semitones);
}

int ss_input_latency(Stretch* s) {
	return s->inputLatency();
}

int ss_output_latency(Stretch* s) {
	return s->outputLatency();
}

// Process mono audio. in and out may be the same pointer.
// outputSamples output samples are written to out.
void ss_process_mono(Stretch* s, const float* in, float* out, int inputSamples, int outputSamples) {
	const float* inputs[1]  = {in};
	float*       outputs[1] = {out};
	s->process(inputs, inputSamples, outputs, outputSamples);
}

void ss_reset(Stretch* s) {
	s->reset();
}

} // extern "C"
