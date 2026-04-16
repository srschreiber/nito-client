package voice

/*
#cgo pkg-config: rnnoise
#include <stdlib.h>
#include "rnnoise.h"

static DenoiseState* create_denoise_state() {
	return rnnoise_create(NULL);
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// Requires 48kHz mono, frame size of 480. 480 samples / 48_000 sec = 1/100 sec = 10ms
const rnnoiseFrameSize = 480

type rnnoiseState struct {
	state *C.DenoiseState
}

func newRNNoiseState() (*rnnoiseState, error) {
	st := C.create_denoise_state()
	if st == nil {
		return nil, fmt.Errorf("failed to create RNNoise state")
	}
	return &rnnoiseState{state: st}, nil
}

func (s *rnnoiseState) Destroy() {
	if s == nil || s.state == nil {
		return
	}
	C.rnnoise_destroy(s.state)
	s.state = nil
}

// ProcessFrame denoises one 10 ms mono frame at 48 kHz.
// RNNoise expects exactly positive multiple of 480 samples
func (s *rnnoiseState) ProcessFrame(frame []float32) error {
	if len(frame)%rnnoiseFrameSize != 0 {
		return fmt.Errorf("frame size must be a multiple of %d", rnnoiseFrameSize)
	}

	if s == nil || s.state == nil {
		return fmt.Errorf("rnnoise state is nil")
	}

	for fStart := 0; fStart < len(frame); fStart += rnnoiseFrameSize {
		fSlice := frame[fStart : fStart+rnnoiseFrameSize]
		C.rnnoise_process_frame(
			s.state,
			(*C.float)(unsafe.Pointer(&fSlice[0])),
			(*C.float)(unsafe.Pointer(&fSlice[0])),
		)
	}
	return nil
}
