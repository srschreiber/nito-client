// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

// C API wrapper around the WebRTC AudioProcessing Module (AEC3).
// Designed to be called from CGo — no C++ types cross the boundary.

#pragma once
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

typedef struct APMHandle APMHandle;

// apm_create allocates and configures an APM instance with AEC3 enabled.
// sample_rate_hz must be 48000; num_channels must be 1 (mono).
// Returns NULL on failure.
APMHandle* apm_create(int sample_rate_hz, int num_channels);

void apm_destroy(APMHandle* h);

// apm_set_stream_delay sets the known delay (ms) between ProcessReverseStream
// receiving far-end audio and ProcessStream receiving the corresponding near-end
// audio. Pass 0 if unknown.
void apm_set_stream_delay(APMHandle* h, int delay_ms);

// apm_process_reverse feeds far-end (playback/render) audio into the AEC.
// Must be called for every audio frame played out, before the corresponding
// capture frame arrives. Processes in-place: pcm is both input and output.
// num_samples must equal sample_rate_hz / 100 (10 ms frame, e.g. 480 at 48 kHz).
// Returns 0 on success, non-zero on error.
int apm_process_reverse(APMHandle* h, int16_t* pcm, int num_samples);

// apm_process_capture applies echo cancellation to near-end (microphone) audio.
// Processes in-place. num_samples must equal sample_rate_hz / 100.
// Returns 0 on success, non-zero on error.
int apm_process_capture(APMHandle* h, int16_t* pcm, int num_samples);

#ifdef __cplusplus
}
#endif
