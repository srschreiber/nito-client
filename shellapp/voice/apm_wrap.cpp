// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

// C++ implementation of the APM C wrapper (apm_wrap.h).
// Keeps all WebRTC C++ types private; only C types cross the boundary.

#include "apm_wrap.h"

#include <memory>
#include <optional>

#include "api/audio/audio_processing.h"
#include "api/audio/echo_canceller3_config.h"
#include "api/audio/echo_control.h"
#include "api/scoped_refptr.h"
#include "modules/audio_processing/aec3/echo_canceller3.h"

using namespace webrtc;

// Inline factory that constructs EchoCanceller3 with a custom config.
// AudioProcessingBuilder::SetEchoControlFactory is the only way to inject
// an EchoCanceller3Config in this version of webrtc-audio-processing
// (there is no standalone EchoCanceller3Factory class).
class Aec3Factory : public EchoControlFactory {
public:
    explicit Aec3Factory(const EchoCanceller3Config& cfg) : cfg_(cfg) {}
    std::unique_ptr<EchoControl> Create(int sample_rate_hz,
                                        int num_render_channels,
                                        int num_capture_channels) override {
        return std::make_unique<EchoCanceller3>(
            cfg_, std::nullopt, sample_rate_hz,
            num_render_channels, num_capture_channels);
    }
private:
    EchoCanceller3Config cfg_;
};

struct APMHandle {
    rtc::scoped_refptr<AudioProcessing> apm;
    StreamConfig cfg;
};

extern "C" {

APMHandle* apm_create(int sample_rate_hz, int num_channels) {
    // Tuned EchoCanceller3 config:
    //
    // filter.refined/coarse.length_blocks: 20 (default 13).
    //   Each block is 64 samples at 48 kHz ≈ 1.33 ms, so 20 blocks ≈ 27 ms of
    //   adaptive filter coverage for the reverberant echo tail. The bulk echo
    //   delay is handled separately via SetStreamDelay; this covers reflections
    //   arriving after that delay. 13 blocks ≈ 17 ms was sufficient for
    //   anechoic setups but left residual in typical rooms.
    //
    // ep_strength.default_len / nearend_len: 0.95 (default 0.83).
    //   Controls how aggressively the nonlinear (residual echo) suppressor
    //   acts after the linear filter. 0.83 assumes the echo path is well
    //   modelled; 0.95 tells the suppressor to expect more residual and apply
    //   stronger post-filtering. Fixes subtle residual echo without audible
    //   speech distortion at this level.
    EchoCanceller3Config aec3_cfg;
    aec3_cfg.filter.refined.length_blocks        = 20;
    aec3_cfg.filter.coarse.length_blocks         = 20;
    aec3_cfg.filter.refined_initial.length_blocks = 20;
    aec3_cfg.filter.coarse_initial.length_blocks  = 20;
    aec3_cfg.ep_strength.default_len  = 0.95f;
    aec3_cfg.ep_strength.nearend_len  = 0.95f;

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

    auto apm = AudioProcessingBuilder()
        .SetConfig(config)
        .SetEchoControlFactory(std::make_unique<Aec3Factory>(aec3_cfg))
        .Create();
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
