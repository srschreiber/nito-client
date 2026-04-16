// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package voice

// CGo wrapper around signalsmith-stretch (MIT) for real-time pitch shifting.
// Header-only C++ library vendored in signalsmith/.
// No system library required — the headers are compiled as part of this package.

/*
#cgo CXXFLAGS: -std=c++17 -I${SRCDIR}
#cgo darwin  LDFLAGS: -lc++
#cgo linux   LDFLAGS: -lstdc++
#cgo windows LDFLAGS: -lstdc++

#include <stdlib.h>

typedef struct Stretch Stretch;

Stretch* ss_new(int channels, int sampleRate, int blockSamples, int intervalSamples);
void     ss_delete(Stretch* s);
void     ss_set_semitones(Stretch* s, float semitones);
int      ss_input_latency(Stretch* s);
int      ss_output_latency(Stretch* s);
void     ss_process_mono(Stretch* s, const float* in, float* out, int inputSamples, int outputSamples);
void     ss_reset(Stretch* s);
*/
import "C"
import (
	"fmt"
	"unsafe"
)

// ssStretch wraps a signalsmith-stretch instance for real-time mono pitch shifting.
type ssStretch struct {
	s             *C.Stretch
	inputLatency  int
	outputLatency int
}

// newSSStretch creates a new pitch shifter.
// blockSamples controls quality/latency trade-off; intervalSamples controls processing granularity.
// At 48 kHz, blockSamples=2048 (~43 ms) / intervalSamples=512 (~11 ms) is a reasonable balance.
func newSSStretch(sampleRate, channels, blockSamples, intervalSamples int) (*ssStretch, error) {
	s := C.ss_new(C.int(channels), C.int(sampleRate), C.int(blockSamples), C.int(intervalSamples))
	if s == nil {
		return nil, fmt.Errorf("ss_new failed")
	}
	return &ssStretch{
		s:             s,
		inputLatency:  int(C.ss_input_latency(s)),
		outputLatency: int(C.ss_output_latency(s)),
	}, nil
}

func (r *ssStretch) setSemitones(semitones float32) {
	C.ss_set_semitones(r.s, C.float(semitones))
}

// process pitch-shifts inputSamples samples from in, writing outputSamples to out.
// in and out must not alias.
func (r *ssStretch) process(in, out []float32) {
	if len(in) == 0 || len(out) == 0 {
		return
	}
	C.ss_process_mono(r.s,
		(*C.float)(unsafe.Pointer(&in[0])),
		(*C.float)(unsafe.Pointer(&out[0])),
		C.int(len(in)),
		C.int(len(out)),
	)
}

func (r *ssStretch) reset() {
	C.ss_reset(r.s)
}

func (r *ssStretch) close() {
	if r.s != nil {
		C.ss_delete(r.s)
		r.s = nil
	}
}
