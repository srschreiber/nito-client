// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

// C++ implementation of the APM C wrapper (apm_wrap.h).
// Keeps all WebRTC C++ types private; only C types cross the boundary.

#include "apm_wrap.h"

#include <memory>

#include "api/audio/audio_processing.h"
#include "api/scoped_refptr.h"

using namespace webrtc;

struct APMHandle {
    rtc::scoped_refptr<AudioProcessing> apm;
    StreamConfig cfg;
};

extern "C" {

APMHandle* apm_create(int sample_rate_hz, int num_channels) {
    AudioProcessing::Config config;

    // Enable AEC3 (echo canceller 3); disable everything else — we use
    // RNNoise for noise suppression and don't need AGC in this pipeline.
    config.echo_canceller.enabled = true;
    config.echo_canceller.mobile_mode = false; // false = AEC3, true = AECm

    config.noise_suppression.enabled = false;
    config.gain_controller1.enabled = false;
    config.gain_controller2.enabled = false;
    config.high_pass_filter.enabled = false;
    config.transient_suppression.enabled = false;

    auto apm = AudioProcessingBuilder().SetConfig(config).Create();
    if (!apm) {
        return nullptr;
    }

    auto* h = new APMHandle();
    h->apm = apm;
    h->cfg = StreamConfig(sample_rate_hz, num_channels);
    return h;
}

void apm_destroy(APMHandle* h) {
    delete h;
}

void apm_set_stream_delay(APMHandle* h, int delay_ms) {
    if (!h) return;
    h->apm->set_stream_delay_ms(delay_ms);
}

int apm_process_reverse(APMHandle* h, int16_t* pcm, int num_samples) {
    if (!h || !pcm) return -1;
    return h->apm->ProcessReverseStream(pcm, h->cfg, h->cfg, pcm);
}

int apm_process_capture(APMHandle* h, int16_t* pcm, int num_samples) {
    if (!h || !pcm) return -1;
    h->apm->set_stream_analog_level(127);
    return h->apm->ProcessStream(pcm, h->cfg, h->cfg, pcm);
}

} // extern "C"
