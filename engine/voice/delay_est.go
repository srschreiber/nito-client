// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

// streamDelayEst estimates the speaker→microphone acoustic delay by
// cross-correlating far-end (speaker) audio with near-end (mic) audio.
// The result is used to keep the WebRTC AEC3 stream-delay hint calibrated
// without relying on a hardcoded constant.
//
// When the user has a headset there is no acoustic echo path, the
// cross-correlation stays below delayEstMinCorr, and the estimate is not
// updated — making the estimator a safe no-op in that case.
//
// Known limitation: the EMA smooths over small alignment jitter caused by OS
// scheduling and jitter-buffer variance, but a sudden device switch can take up
// to delayEstInterval frames (~1 s) to converge to the new delay.
package voice

import (
	"math"
	"sync"
	"sync/atomic"

	"github.com/srschreiber/nito-client/ui/clientlog"
)

const (
	// delayEstMaxMs is the farthest echo delay we search for.
	// Real acoustic delays are 0–20 ms (headset) or 30–120 ms (speakers).
	// 150 ms covers all realistic cases without wasting CPU on a 500 ms search.
	delayEstMaxMs = 150
	// delayEstCapMs is the mic capture window used in each correlation.
	// 60 ms (3 Opus frames) is long enough to span a full phoneme and gives
	// much more stable estimates than a single 20 ms frame, especially during
	// speech with natural pauses between frames.
	delayEstCapMs = 60
	// delayEstCapLen is delayEstCapMs in samples.
	delayEstCapLen = delayEstCapMs * sampleRate / 1000 // 2880
	// delayEstRevBufSamples is the far-end ring buffer size.
	// Extra delayEstCapLen samples ensure a full correlation window at every lag.
	delayEstRevBufSamples = delayEstMaxMs*sampleRate/1000 + delayEstCapLen // 10080
	// delayEstInterval is how many capture frames between estimation runs (1 frame = 20 ms).
	// 50 frames ≈ 1 s — fast enough to catch device changes mid-call.
	delayEstInterval = 50
	// delayEstMinCorr is the minimum normalized cross-correlation to accept a new estimate.
	// 0.30 is conservative enough to reject noisy false peaks while still detecting
	// real echoes; 0.20 was too permissive in reverberant environments.
	delayEstMinCorr = 0.30
	// delayEstAlpha is the EMA smoothing factor applied to each new measurement.
	delayEstAlpha = 0.25
)

// streamDelayEst is safe for concurrent use from the capture and receive goroutines.
type streamDelayEst struct {
	mu      sync.Mutex
	revBuf  []float32 // circular buffer of normalised far-end samples (oldest at revHead)
	revHead int       // next write index
	capSnap []float32 // rolling 60 ms capture window (delayEstCapLen samples)
	estMs   int       // current EMA delay estimate in ms

	frameN int64 // monotonically increasing capture-frame counter
	busy   atomic.Bool
	apm    *apmState // AEC instance to update; cleared by destroy()
}

func newStreamDelayEst(initMs int, apm *apmState) *streamDelayEst {
	return &streamDelayEst{
		revBuf:  make([]float32, delayEstRevBufSamples),
		capSnap: make([]float32, delayEstCapLen),
		estMs:   initMs,
		apm:     apm,
	}
}

// destroy clears the APM reference so in-flight estimation goroutines
// cannot call SetStreamDelay after the APM has been destroyed.
// Must be called before sess.apm.Destroy().
func (e *streamDelayEst) destroy() {
	e.mu.Lock()
	e.apm = nil
	e.mu.Unlock()
}

// addReverse records far-end (speaker output) samples.
// Called from the receive goroutine for every decoded frame written to the speaker.
func (e *streamDelayEst) addReverse(samples []int16) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, s := range samples {
		e.revBuf[e.revHead] = float32(s) / 32768.0
		e.revHead = (e.revHead + 1) % len(e.revBuf)
	}
}

// addCapture maintains a rolling 60 ms capture window and, every delayEstInterval
// frames, spawns a background goroutine to update the delay estimate.
// Returns the current estimate in ms. Called from the capture goroutine.
func (e *streamDelayEst) addCapture(frame []int16) int {
	e.mu.Lock()
	// Slide the window left by one frame and append the new frame at the end.
	// capSnap always holds the most recent delayEstCapLen samples.
	n := len(frame)
	if n > delayEstCapLen {
		n = delayEstCapLen
	}
	copy(e.capSnap, e.capSnap[n:])
	tail := e.capSnap[delayEstCapLen-n:]
	for i := 0; i < n; i++ {
		tail[i] = float32(frame[i]) / 32768.0
	}
	e.mu.Unlock()

	e.frameN++
	if e.frameN%delayEstInterval != 0 {
		return e.estMs
	}
	if !e.busy.CompareAndSwap(false, true) {
		return e.estMs // previous estimation still running
	}
	go e.runEstimate()
	return e.estMs
}

// runEstimate performs normalised cross-correlation in a background goroutine.
// It finds the lag (in samples) at which the mic signal best correlates with
// the far-end signal; that lag is the speaker→mic acoustic delay.
func (e *streamDelayEst) runEstimate() {
	defer e.busy.Store(false)

	// Snapshot shared state.
	e.mu.Lock()
	// Unroll circular buffer into chronological order: revSnap[0] = oldest sample,
	// revSnap[len-1] = most recent sample.
	revSnap := make([]float32, len(e.revBuf))
	n := copy(revSnap, e.revBuf[e.revHead:])
	copy(revSnap[n:], e.revBuf[:e.revHead])
	capSnap := make([]float32, delayEstCapLen)
	copy(capSnap, e.capSnap)
	e.mu.Unlock()

	// Skip if mic is silent (nothing to correlate against).
	var capRMS float64
	for _, s := range capSnap {
		capRMS += float64(s) * float64(s)
	}
	capRMS = math.Sqrt(capRMS / float64(len(capSnap)))
	if capRMS < 0.005 {
		return
	}

	// Brute-force normalised cross-correlation search.
	//
	// At delay D samples: the echo in the capture corresponds to far-end audio
	// from D samples ago, which sits at revSnap[len-D-capLen : len-D].
	//
	// We maximise  Σ cap[i]·rev[len-D-capLen+i]  /  (capRMS · refRMS · capLen)
	// over D ∈ [0, maxLag].
	//
	// The denominator is the Cauchy-Schwarz upper bound on the dot product:
	//
	//   |Σ cap[i]·ref[i]| ≤ √(Σ cap[i]²) · √(Σ ref[i]²)
	//                      = capRMS·√N · refRMS·√N  =  capRMS · refRMS · N
	//
	// Dividing by it clamps the result to [−1, 1] and makes it volume-invariant:
	// a quiet echo and a loud echo at the same lag score identically.
	// This is equivalent to computing cos θ between the two N-dimensional
	// sample vectors (their projection / product of magnitudes).
	total := len(revSnap)
	capLen := len(capSnap)
	maxLag := total - capLen

	bestLag := 0
	bestCorr := 0.0

	for d := 0; d <= maxLag; d++ {
		start := total - d - capLen
		if start < 0 {
			break
		}
		ref := revSnap[start : start+capLen]

		var refRMS, dot float64
		for i := range capSnap {
			r := float64(ref[i])
			refRMS += r * r
			dot += float64(capSnap[i]) * r
		}
		refRMS = math.Sqrt(refRMS / float64(capLen))
		if refRMS < 0.001 {
			continue // far-end is silent at this lag; skip
		}
		normCorr := dot / (capRMS * refRMS * float64(capLen))
		if normCorr > bestCorr {
			bestCorr = normCorr
			bestLag = d
		}
	}

	if bestCorr < delayEstMinCorr {
		// Weak correlation — likely a headset or far-end is silent. Don't update.
		clientlog.Info("voice: delay est: corr=%.3f below threshold, skipping", bestCorr)
		return
	}

	measuredMs := bestLag * 1000 / sampleRate

	e.mu.Lock()
	prev := e.estMs
	updated := int(delayEstAlpha*float64(measuredMs) + (1-delayEstAlpha)*float64(prev))
	e.estMs = updated
	apm := e.apm
	e.mu.Unlock()

	clientlog.Info("voice: delay est: lag=%dms corr=%.3f ema=%dms (was %dms)", measuredMs, bestCorr, updated, prev)

	if apm != nil {
		apm.SetStreamDelay(updated)
	}
}
