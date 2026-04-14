// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

// CGo bridge to the WebRTC AEC3 wrapper (apm_wrap.cpp / apm_wrap.h).
// Provides echo cancellation for voice capture and playback paths.

package voice

/*
#cgo CXXFLAGS: -std=c++17
#cgo CXXFLAGS: -I${SRCDIR}/../../webrtc-audio-processing/webrtc
#cgo CXXFLAGS: -I${SRCDIR}/../../webrtc-audio-processing/subprojects/abseil-cpp-20240722.0
#cgo CXXFLAGS: -DWEBRTC_LIBRARY_IMPL
#cgo darwin  CXXFLAGS: -DWEBRTC_POSIX -DWEBRTC_MAC
#cgo linux   CXXFLAGS: -DWEBRTC_POSIX -DWEBRTC_LINUX
#cgo windows CXXFLAGS: -DWEBRTC_WIN -DNOMINMAX -D_USE_MATH_DEFINES

// — webrtc-audio-processing static libs —
#cgo LDFLAGS: -L${SRCDIR}/../../webrtc-audio-processing/build/webrtc/modules/audio_processing
#cgo LDFLAGS: -lwebrtc-audio-processing-2
#cgo LDFLAGS: -L${SRCDIR}/../../webrtc-audio-processing/build/webrtc/common_audio
#cgo LDFLAGS: -lcommon_audio
#cgo LDFLAGS: -L${SRCDIR}/../../webrtc-audio-processing/build/webrtc/rtc_base
#cgo LDFLAGS: -llibbase
#cgo LDFLAGS: -L${SRCDIR}/../../webrtc-audio-processing/build/webrtc/system_wrappers
#cgo LDFLAGS: -lsystem_wrappers
#cgo LDFLAGS: -L${SRCDIR}/../../webrtc-audio-processing/build/webrtc/api
#cgo LDFLAGS: -llibapi
#cgo LDFLAGS: -L${SRCDIR}/../../webrtc-audio-processing/build/webrtc/third_party/pffft
#cgo LDFLAGS: -llibpffft
#cgo LDFLAGS: -L${SRCDIR}/../../webrtc-audio-processing/build/webrtc/modules/third_party/fft
#cgo LDFLAGS: -llibfft
#cgo LDFLAGS: -L${SRCDIR}/../../webrtc-audio-processing/build/webrtc/third_party/rnnoise
#cgo LDFLAGS: -llibrnnoise

// — Abseil static libs —
#cgo LDFLAGS: -L${SRCDIR}/../../webrtc-audio-processing/build/subprojects/abseil-cpp-20240722.0
#cgo LDFLAGS: -labsl_synchronization -labsl_strings -labsl_base -labsl_debugging
#cgo LDFLAGS: -labsl_types -labsl_log -labsl_status -labsl_hash
#cgo LDFLAGS: -labsl_container -labsl_crc -labsl_profiling -labsl_random
#cgo LDFLAGS: -labsl_flags -labsl_numeric -labsl_time

// — platform system libs —
#cgo darwin  LDFLAGS: -framework CoreFoundation -framework Foundation
#cgo linux   LDFLAGS: -lpthread
#cgo windows LDFLAGS: -lwinmm -lole32

#include <stdint.h>
#include "apm_wrap.h"
*/
import "C"
import (
	"fmt"
	"unsafe"
)

type apmState struct {
	h *C.APMHandle
}

// newAPMState creates a WebRTC AEC3 instance for the given sample rate and
// channel count. Must be destroyed with Destroy() when no longer needed.
func newAPMState(sampleRateHz, numChannels int) (*apmState, error) {
	h := C.apm_create(C.int(sampleRateHz), C.int(numChannels))
	if h == nil {
		return nil, fmt.Errorf("apm_create failed")
	}
	return &apmState{h: h}, nil
}

func (a *apmState) Destroy() {
	if a.h != nil {
		C.apm_destroy(a.h)
		a.h = nil
	}
}

// SetStreamDelay informs the APM of the expected delay in milliseconds between
// the far-end (playback) audio being submitted via ProcessReverse and the
// corresponding echo appearing in the near-end (capture) audio.
func (a *apmState) SetStreamDelay(delayMs int) {
	C.apm_set_stream_delay(a.h, C.int(delayMs))
}

// ProcessReverse feeds a 10 ms (480-sample at 48 kHz) far-end frame into the
// AEC. Call this for every audio frame that is played out through the speaker,
// before the corresponding capture frame is processed.
// The slice is modified in-place.
func (a *apmState) ProcessReverse(pcm []int16) error {
	if len(pcm) == 0 {
		return nil
	}
	rc := C.apm_process_reverse(a.h, (*C.int16_t)(unsafe.Pointer(&pcm[0])), C.int(len(pcm)))
	if rc != 0 {
		return fmt.Errorf("apm_process_reverse: error %d", int(rc))
	}
	return nil
}

// ProcessCapture applies AEC to a 10 ms (480-sample at 48 kHz) near-end
// (microphone) frame. The echo-cancelled result is written back into pcm.
func (a *apmState) ProcessCapture(pcm []int16) error {
	if len(pcm) == 0 {
		return nil
	}
	rc := C.apm_process_capture(a.h, (*C.int16_t)(unsafe.Pointer(&pcm[0])), C.int(len(pcm)))
	if rc != 0 {
		return fmt.Errorf("apm_process_capture: error %d", int(rc))
	}
	return nil
}
