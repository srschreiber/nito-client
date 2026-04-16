// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package components

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	mp3 "github.com/hajimehoshi/go-mp3"
	"github.com/srschreiber/nito-client/shellapp/clientlog"
	"github.com/srschreiber/nito-client/shellapp/voice"
)

// PlayAudioFromURL returns a tea.Cmd that streams and plays the MP3 (or M3U
// playlist) at audioURL on the given track slot, but only if the user is
// currently in a voice call in roomID. ctx can be cancelled to abort early.
// On natural completion or error it returns AudioTrackDoneMsg or
// AudioPlaybackErrorMsg so the caller can free the track slot; on
// cancellation it returns nil.
func PlayAudioFromURL(ctx context.Context, roomID, audioURL string, track int) tea.Cmd {
	return func() tea.Msg {
		if roomID != voice.SelfRoomID && voice.ActiveRoomID() != roomID {
			return AudioTrackDoneMsg{Track: track}
		}

		urls, err := resolveAudioURLs(ctx, audioURL)
		if err != nil {
			return audioPlaybackErr(track, "resolve", err)
		}

		for _, u := range urls {
			if ctx.Err() != nil {
				return nil
			}
			if msg := playOne(ctx, roomID, u, track); msg != nil {
				return msg
			}
			if ctx.Err() != nil {
				return nil
			}
		}
		return AudioTrackDoneMsg{Track: track}
	}
}

// PlayAudioFromFile returns a tea.Cmd that plays a local MP3 file on the given
// track slot. It never broadcasts and does not require an active voice call.
// Supports absolute paths and paths beginning with ~/ (expanded to home dir).
func PlayAudioFromFile(ctx context.Context, path string, track int) tea.Cmd {
	return func() tea.Msg {
		if strings.HasPrefix(path, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				path = filepath.Join(home, path[2:])
			}
		}

		f, err := os.Open(path)
		if err != nil {
			return audioPlaybackErr(track, "open file", err)
		}
		defer f.Close()

		dec, err := mp3.NewDecoder(f)
		if err != nil {
			return audioPlaybackErr(track, "mp3 decode", err)
		}

		otoCtx, err := voice.GetMusicOtoCtx(dec.SampleRate())
		if err != nil {
			return audioPlaybackErr(track, "oto init", err)
		}

		eq := newEQReader(dec, dec.SampleRate(), track)
		defer eq.Close()
		defer voice.ClearTrackBandLevels(track)

		player := otoCtx.NewPlayer(eq)
		if bss, ok := player.(interface{ SetBufferSize(int) }); ok {
			bss.SetBufferSize(dec.SampleRate() / 5 * 4) // ~200 ms
		}
		player.SetVolume(voice.EffectivePlaybackVolume())
		defer player.Close()
		player.Play()

		for {
			select {
			case <-ctx.Done():
				return nil
			default:
				if !player.IsPlaying() {
					return AudioTrackDoneMsg{Track: track}
				}
				player.SetVolume(voice.EffectivePlaybackVolume())
				time.Sleep(20 * time.Millisecond)
			}
		}
	}
}

// resolveAudioURLs fetches audioURL and returns a slice of MP3 URLs to play.
// For a plain MP3 URL it returns a single-element slice. For an M3U/M3U8
// playlist it parses and returns all track URLs in order.
func resolveAudioURLs(ctx context.Context, audioURL string) ([]string, error) {
	if isM3U(audioURL) {
		urls, err := fetchAndParseM3U(ctx, audioURL)
		if err == nil && len(urls) > 0 {
			return urls, nil
		}
		if err != nil && !strings.Contains(err.Error(), "no tracks found") {
			// Real network or parse error — surface it.
			return nil, err
		}
		// The server either streamed audio directly from the .m3u URL or the
		// playlist contained no recognisable http(s) track lines. Fall back to
		// treating the URL itself as the audio stream.
		clientlog.Info("audio_player: m3u yielded no tracks, attempting direct stream of %s", audioURL)
		return []string{audioURL}, nil
	}
	// Check Content-Type in case the URL has no recognisable extension.
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, audioURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err == nil {
		resp.Body.Close()
		ct := resp.Header.Get("Content-Type")
		if strings.Contains(ct, "mpegurl") || strings.Contains(ct, "x-scpls") {
			return fetchAndParseM3U(ctx, audioURL)
		}
	}
	return []string{audioURL}, nil
}

// isM3U reports whether the URL path ends with a playlist extension.
func isM3U(u string) bool {
	lower := strings.ToLower(u)
	// Strip any query string before checking the extension.
	if i := strings.Index(lower, "?"); i != -1 {
		lower = lower[:i]
	}
	return strings.HasSuffix(lower, ".m3u") || strings.HasSuffix(lower, ".m3u8")
}

// fetchAndParseM3U downloads the playlist at url and returns all non-comment,
// non-empty lines as track URLs. Relative URLs are not resolved (archive.org
// and most public sources use absolute URLs).
func fetchAndParseM3U(ctx context.Context, url string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var tracks []string
	scanner := bufio.NewScanner(io.LimitReader(resp.Body, 8<<10)) // 8 KB max — real playlists are tiny
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Skip binary/non-URL lines (e.g. ICY metadata or Shoutcast frames
		// embedded in some M3U responses). Only http(s) absolute URLs are accepted.
		if !strings.HasPrefix(line, "http://") && !strings.HasPrefix(line, "https://") {
			continue
		}
		tracks = append(tracks, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(tracks) == 0 {
		return nil, fmt.Errorf("no tracks found in playlist")
	}
	clientlog.Info("audio_player: m3u resolved %d track(s) from %s", len(tracks), url)
	return tracks, nil
}

// prefetchReader wraps an io.Reader with an asynchronous read-ahead goroutine.
// The goroutine fills a bounded channel of byte chunks so the MP3 decoder never
// blocks on network I/O during playback. The goroutine exits when src is
// exhausted, an error occurs, or ctx is cancelled (closing the channel so
// Read() returns io.EOF and the player stops naturally).
type prefetchReader struct {
	ch   <-chan []byte
	tail []byte
}

const (
	prefetchChunkSize = 16 * 1024 // 16 KB per chunk
	prefetchChunks    = 8         // 128 KB read-ahead — enough to smooth network jitter
)

// newPrefetchReader creates a prefetchReader with an asynchronous read-ahead buffer to optimize streaming I/O operations.
func newPrefetchReader(ctx context.Context, src io.Reader) *prefetchReader {
	ch := make(chan []byte, prefetchChunks)
	go func() {
		defer close(ch)
		for {
			buf := make([]byte, prefetchChunkSize)
			n, err := src.Read(buf)
			if n > 0 {
				select {
				case ch <- buf[:n]:
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	return &prefetchReader{ch: ch}
}

func (r *prefetchReader) Read(p []byte) (int, error) {
	if len(r.tail) == 0 {
		chunk, ok := <-r.ch
		if !ok {
			return 0, io.EOF
		}
		r.tail = chunk
	}
	n := copy(p, r.tail)
	r.tail = r.tail[n:]
	return n, nil
}

// IsDrained reports whether all prefetched data has been consumed.
func (r *prefetchReader) IsDrained() bool {
	return len(r.ch) == 0 && len(r.tail) == 0
}

// IsNearlyDrained reports whether the prefetch buffer is critically low
// (≤1 chunk buffered). For live streams this means we are close to the
// broadcast edge and should reconnect before audio glitches.
func (r *prefetchReader) IsNearlyDrained() bool {
	return len(r.ch) <= 1
}

// playOne streams and plays a single MP3 URL, reconnecting automatically when a
// live stream catches up to the broadcast edge. For regular files it runs once.
// If roomID is voice.SelfRoomID the active-room guard is skipped so the user
// can play audio locally without being in a voice call.
func playOne(ctx context.Context, roomID, audioURL string, track int) tea.Msg {
	for {
		if ctx.Err() != nil {
			return nil
		}
		reconnect, msg := playOneAttempt(ctx, roomID, audioURL, track)
		if msg != nil {
			return msg
		}
		if !reconnect {
			return nil
		}
		clientlog.Info("audio_player: live edge reached on track %d — reconnecting", track)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// liveEdgeDrainTicks is how many consecutive 20 ms polling ticks the prefetch
// must be nearly empty (≤1 chunk) before we reconnect for the live edge.
// 3 ticks × 20 ms = 60 ms debounce — aggressive but intentional per user preference.
const liveEdgeDrainTicks = 3

// playOneAttempt makes a single HTTP streaming attempt for audioURL.
// Returns (reconnect, msg): reconnect=true means a live stream hit the live
// edge and the caller should reopen the connection; msg is non-nil on error.
func playOneAttempt(ctx context.Context, roomID, audioURL string, track int) (bool, tea.Msg) {
	if roomID != voice.SelfRoomID && voice.ActiveRoomID() != roomID {
		return false, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, audioURL, nil)
	if err != nil {
		return false, audioPlaybackErr(track, "build request", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return false, nil
		}
		return false, audioPlaybackErr(track, "fetch", err)
	}
	defer resp.Body.Close()

	// Detect Icecast/Shoutcast live streams via ICY response headers.
	isLive := resp.Header.Get("Icy-Metaint") != "" ||
		resp.Header.Get("Icy-Name") != "" ||
		resp.Header.Get("Icy-Url") != ""
	voice.SetTrackLive(track, isLive)
	defer voice.SetTrackLive(track, false)
	// Show a pulsing spinner while the 5 s live buffer fills.
	if isLive {
		voice.SetTrackBuffering(track, true)
		defer voice.SetTrackBuffering(track, false)
		go func() {
			timer := time.NewTimer(5 * time.Second)
			defer timer.Stop()
			select {
			case <-ctx.Done():
			case <-timer.C:
				voice.SetTrackBuffering(track, false)
			}
		}()
	}

	prefetch := newPrefetchReader(ctx, resp.Body)
	dec, err := mp3.NewDecoder(prefetch)
	if err != nil {
		if ctx.Err() != nil {
			return false, nil
		}
		return false, audioPlaybackErr(track, "mp3 decode", err)
	}

	otoCtx, err := voice.GetMusicOtoCtx(dec.SampleRate())
	if err != nil {
		return false, audioPlaybackErr(track, "oto init", err)
	}

	eq := newEQReader(dec, dec.SampleRate(), track)
	defer eq.Close()
	defer voice.ClearTrackBandLevels(track) // zero the meter when playback ends
	player := otoCtx.NewPlayer(eq)
	if bss, ok := player.(interface{ SetBufferSize(int) }); ok {
		bufSize := dec.SampleRate() / 5 * 4 // ~200 ms
		if isLive {
			bufSize = dec.SampleRate() * 4 * 5 // ~5 s for live streams
		}
		bss.SetBufferSize(bufSize)
	}
	player.SetVolume(voice.EffectivePlaybackVolume())
	defer player.Close()
	player.Play()

	drainCount := 0
	bufferWasFull := false // guard: only detect live edge after buffer has been full at least once
	for {
		select {
		case <-ctx.Done():
			return false, nil
		default:
			if !player.IsPlaying() {
				return false, nil
			}
			player.SetVolume(voice.EffectivePlaybackVolume())
			if isLive {
				if len(prefetch.ch) >= prefetchChunks/2 {
					bufferWasFull = true
				}
				if bufferWasFull && prefetch.IsNearlyDrained() {
					drainCount++
					if drainCount >= liveEdgeDrainTicks {
						return true, nil // live edge — reconnect
					}
				} else {
					drainCount = 0
				}
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
}

// biquad is a 2nd-order IIR filter in direct-form-II-transposed. Used to build
// the per-frequency-band analyser for the spectrum meter.
type biquad struct {
	b0, b2 float32 // numerator coefficients; b1 = 0 for bandpass
	a1n    float32 // a1/a0 (RBJ convention: -2cos(w0)/a0)
	a2n    float32 // a2/a0 = (1-alpha)/a0
	w1, w2 float32 // filter state
}

// newBandpass returns a biquad bandpass filter (0 dB peak gain at fc) using
// the Audio EQ Cookbook (RBJ) coefficients.
func newBandpass(fc, q, sr float32) biquad {
	w0 := 2 * math.Pi * float64(fc) / float64(sr)
	sinw0 := math.Sin(w0)
	cosw0 := math.Cos(w0)
	alpha := sinw0 / (2 * float64(q))
	a0 := 1 + alpha
	return biquad{
		b0:  float32(alpha / a0),
		b2:  float32(-alpha / a0),
		a1n: float32(-2 * cosw0 / a0),
		a2n: float32((1 - alpha) / a0),
	}
}

func (f *biquad) process(x float32) float32 {
	y := f.b0*x + f.w1
	f.w1 = -f.a1n*y + f.w2
	f.w2 = f.b2*x - f.a2n*y
	return y
}

// bandAnalyzer runs a bank of bandpass filters on a mono signal and tracks the
// smoothed amplitude envelope for each band with fast-attack / slow-release.
// The filter bank is sized dynamically so the number of bands can change at
// runtime (e.g. when the terminal is resized).
type bandAnalyzer struct {
	filters []biquad
	smooth  []float32
}

// init (re)initialises the filter bank for n bands at sample rate sr.
// Existing smooth values are zeroed; call when the band count changes.
func (a *bandAnalyzer) init(sr float32, n int) {
	centers := voice.BandCenters(n)
	a.filters = make([]biquad, n)
	a.smooth = make([]float32, n)
	for i, c := range centers {
		a.filters[i] = newBandpass(c, 1.0, sr)
	}
}

func (a *bandAnalyzer) process(mono float32) {
	for i := range a.filters {
		out := a.filters[i].process(mono)
		amp := out
		if amp < 0 {
			amp = -amp
		}
		const attack, release = float32(0.4), float32(0.06)
		if amp > a.smooth[i] {
			a.smooth[i] = attack*amp + (1-attack)*a.smooth[i]
		} else {
			a.smooth[i] = release*amp + (1-release)*a.smooth[i]
		}
	}
}

// playbackProcessEMAus holds the EMA of the DSP processing time per eqReader.Read()
// call, in microseconds. Updated by every read; read by the settings screen.
var playbackProcessEMAus atomic.Int64

// playbackJitterEMAus and playbackJitterPeakUs track the inter-call scheduling
// jitter of eqReader.Read() (|actual_interval - expected_interval|, µs).
var playbackJitterEMAus atomic.Int64
var playbackJitterPeakUs atomic.Int64

// eqReader wraps an io.Reader (typically an mp3.Decoder) and applies the global
// playback effect chain to each decoded stereo int16-LE frame. Left and right
// channels are processed through independent effect chains so state stays
// consistent per channel. The version counter detects settings changes and
// triggers a rebuild without restarting playback.
//
// Pipeline: normalise int16→float32 → preScale → EQ → Delay → Reverb → Chorus →
//
//	[Pitch] → Limiter → tanh(outputGain × x) → int16.
//
// preScale = 10^(-maxBoostDB/20) ensures the EQ stage never produces a value
// outside [-1, 1], preventing intermediate clipping. outputGain (the volume
// slider, 0–800%) is applied after the full chain.

// channelEffects owns the effect instances for a single audio channel. Storing
// them here keeps their addresses stable across pipeline rebuilds, so the ring
// buffers, filter history, and CGo state persist between settings changes.
type channelEffects struct {
	eq     voice.EQ
	delay  voice.Delay
	reverb voice.Reverb
	chorus voice.Chorus
	pitch  *voice.PlaybackPitchEffect
	lim    voice.PeakLimiter
}

// close releases any CGo resources held by this channel.
func (c *channelEffects) close() {
	if c.pitch != nil {
		c.pitch.Close()
		c.pitch = nil
	}
}

// buildPipeline assembles the channel's effects into an ordered EffectPipeline.
// Disabled effects are skipped at runtime by EffectPipeline via the Enabler interface.
func (c *channelEffects) buildPipeline() voice.EffectPipeline {
	p := voice.EffectPipeline{&c.eq, &c.delay, &c.reverb, &c.chorus}
	if c.pitch != nil {
		p = append(p, c.pitch)
	}
	return append(p, &c.lim)
}

// meterDelay is the fixed render delay applied to the EQ bar chart so the
// visuals roughly align with the audio being heard. Tune by feel — raise it
// if bars look early, lower it if they look late.
const meterDelay = 500 * time.Millisecond

// meterDelayBuf is a fixed-size ring buffer of mono mix samples used to delay
// the band analyser feed by meterDelay. On each push the overwritten slot holds
// the sample from exactly len(buf) samples ago — no timestamps or allocations.
type meterDelayBuf struct {
	buf  []float32
	pos  int
	full bool
}

func newMeterDelayBuf(sampleRate int) meterDelayBuf {
	n := int(float64(sampleRate) * meterDelay.Seconds())
	return meterDelayBuf{buf: make([]float32, n)}
}

// push writes v into the ring and returns the sample from meterDelay ago (0
// until the buffer has filled for the first time).
func (b *meterDelayBuf) push(v float32) float32 {
	old := b.buf[b.pos]
	b.buf[b.pos] = v
	b.pos++
	if b.pos >= len(b.buf) {
		b.pos = 0
		b.full = true
	}
	if !b.full {
		return 0
	}
	return old
}

type eqReader struct {
	track         int // audio track index (0–2); used to write level meter data
	src           io.Reader
	sampleRate    int
	version       uint64
	bandVersion   uint64 // mirrors voice.BandCountVersion(); triggers filter bank rebuild
	left, right   channelEffects
	leftPipeline  voice.EffectPipeline
	rightPipeline voice.EffectPipeline
	preScale      float32
	outputGain    float32
	panPhase      float64       // LFO phase for auto-pan; preserved across settings rebuilds
	bands         bandAnalyzer  // per-frequency-band spectrum analyser for the meter
	meterBuf      meterDelayBuf // ring buffer that delays the analyser feed by meterDelay
	// Pre-allocated work buffers reused each Read() call to avoid GC churn.
	workLeft  []float32
	workRight []float32
	// Inter-call jitter tracking.
	prevReadTime  time.Time
	prevFrames    int
	jitterBuf     [256]int64 // ring buffer of recent jitter samples (µs)
	jitterBufIdx  int        // next write position
	jitterBufFull bool       // true once the buffer has wrapped at least once
}

func newEQReader(src io.Reader, sampleRate, track int) *eqReader {
	r := &eqReader{src: src, sampleRate: sampleRate, track: track, meterBuf: newMeterDelayBuf(sampleRate)}
	r.left.pitch = voice.NewPlaybackPitchEffect(sampleRate)
	r.right.pitch = voice.NewPlaybackPitchEffect(sampleRate)
	r.bands.init(float32(sampleRate), voice.NumBands())
	r.bandVersion = voice.BandCountVersion()
	r.rebuildEffects()
	return r
}

// Close releases CGo resources held by the pitch effects.
func (r *eqReader) Close() {
	r.left.close()
	r.right.close()
}

func (r *eqReader) rebuildEffects() {
	sr := float32(r.sampleRate)

	eqS := voice.GetPlaybackEQSettings()
	r.left.eq.Settings = eqS
	r.right.eq.Settings = eqS
	r.left.eq.UpdateFilters(sr)
	r.right.eq.UpdateFilters(sr)

	delayS := voice.GetDelaySettings()
	r.left.delay.Settings = delayS
	r.right.delay.Settings = delayS
	r.left.delay.UpdateSettings(sr)
	r.right.delay.UpdateSettings(sr)

	revS := voice.GetReverbSettings()
	r.left.reverb.Settings = revS
	r.right.reverb.Settings = revS
	r.left.reverb.UpdateSettings(sr)
	r.right.reverb.UpdateSettings(sr)

	choS := voice.GetChorusSettings()
	r.left.chorus.Settings = choS
	r.right.chorus.Settings = choS
	r.left.chorus.UpdateSettings(sr)
	r.right.chorus.UpdateSettings(sr)

	pitchS := voice.GetPlaybackPitchSettings()
	if r.left.pitch != nil {
		r.left.pitch.Settings = pitchS
		r.left.pitch.UpdateSettings()
	}
	if r.right.pitch != nil {
		r.right.pitch.Settings = pitchS
		r.right.pitch.UpdateSettings()
	}

	// preScale: attenuate input so EQ boost never clips the internal float range.
	maxDB := eqS.BassGain
	if eqS.MidGain > maxDB {
		maxDB = eqS.MidGain
	}
	if eqS.TrebleGain > maxDB {
		maxDB = eqS.TrebleGain
	}
	if maxDB > 0 {
		r.preScale = float32(math.Pow(10, -float64(maxDB)/20.0))
	} else {
		r.preScale = 1.0
	}
	r.outputGain = float32(voice.GetPlaybackEQVolume()) / 100.0

	r.leftPipeline = r.left.buildPipeline()
	r.rightPipeline = r.right.buildPipeline()

	r.version = voice.PlaybackEQVersion()
}

func (r *eqReader) Read(p []byte) (int, error) {
	// Measure inter-call jitter: |actual_interval - expected_interval|.
	// Skip the first two calls (no baseline yet) and any gap > 2 s (stale/paused).
	now := time.Now()
	if !r.prevReadTime.IsZero() && r.prevFrames > 0 {
		actual := now.Sub(r.prevReadTime)
		if actual < 2*time.Second {
			expected := time.Duration(float64(r.prevFrames) / float64(r.sampleRate) * float64(time.Second))
			jitter := actual - expected
			if jitter < 0 {
				jitter = -jitter
			}
			jitterUs := jitter.Microseconds()
			prev := playbackJitterEMAus.Load()
			ema := jitterUs
			if prev != 0 {
				ema = int64(0.9*float64(prev) + 0.1*float64(jitterUs))
			}
			playbackJitterEMAus.Store(ema)
			// Windowed peak: write into ring buffer, then scan for max.
			r.jitterBuf[r.jitterBufIdx] = jitterUs
			r.jitterBufIdx++
			if r.jitterBufIdx >= len(r.jitterBuf) {
				r.jitterBufIdx = 0
				r.jitterBufFull = true
			}
			n := r.jitterBufIdx
			if r.jitterBufFull {
				n = len(r.jitterBuf)
			}
			var windowPeak int64
			for i := 0; i < n; i++ {
				if r.jitterBuf[i] > windowPeak {
					windowPeak = r.jitterBuf[i]
				}
			}
			playbackJitterPeakUs.Store(windowPeak)
		}
	}
	r.prevReadTime = now

	if voice.PlaybackEQVersion() != r.version {
		r.rebuildEffects()
	}
	if voice.BandCountVersion() != r.bandVersion {
		r.bands.init(float32(r.sampleRate), voice.NumBands())
		r.bandVersion = voice.BandCountVersion()
	}
	n, err := r.src.Read(p)
	// Process only complete stereo frames (4 bytes = L int16 + R int16).
	frames := n / 4
	if frames > 0 {
		dspStart := time.Now()
		// Grow pre-allocated work buffers only when necessary (typically never
		// after the first call, eliminating per-Read heap allocation and GC churn).
		if len(r.workLeft) < frames {
			r.workLeft = make([]float32, frames)
			r.workRight = make([]float32, frames)
		}
		left := r.workLeft[:frames]
		right := r.workRight[:frames]
		scale := float32(1.0/32768.0) * r.preScale
		for i := 0; i < frames; i++ {
			off := i * 4
			left[i] = float32(int16(binary.LittleEndian.Uint16(p[off:]))) * scale
			right[i] = float32(int16(binary.LittleEndian.Uint16(p[off+2:]))) * scale
		}
		// Apply pipelines. EffectPipeline.Apply skips disabled effects automatically.
		r.leftPipeline.Apply(left)
		r.rightPipeline.Apply(right)
		// Apply pan gains, output gain, soft-clip via tanh, reinterleave.
		// tanh provides smooth saturation instead of hard clipping, which is
		// audible when the volume slider is pushed well above 100%.
		panS := voice.GetPannerSettings()
		// Pre-compute static gains for the non-auto-pan case to avoid per-sample trig.
		staticAngle := float64(panS.Balance+1) * math.Pi / 4
		staticLeftGain := float32(math.Cos(staticAngle))
		staticRightGain := float32(math.Sin(staticAngle))
		phaseInc := 2 * math.Pi * float64(panS.AutoPanRate) / float64(r.sampleRate)
		for i := 0; i < frames; i++ {
			var lg, rg float32
			if panS.AutoPanEnabled {
				bal := panS.Balance + panS.AutoPanDepth*float32(math.Sin(r.panPhase))
				r.panPhase += phaseInc
				if r.panPhase >= 2*math.Pi {
					r.panPhase -= 2 * math.Pi
				}
				if bal < -1 {
					bal = -1
				} else if bal > 1 {
					bal = 1
				}
				angle := float64(bal+1) * math.Pi / 4
				lg = float32(math.Cos(angle))
				rg = float32(math.Sin(angle))
			} else {
				lg = staticLeftGain
				rg = staticRightGain
			}
			off := i * 4
			lv := float32(math.Tanh(float64(left[i] * r.outputGain * lg)))
			rv := float32(math.Tanh(float64(right[i] * r.outputGain * rg)))
			binary.LittleEndian.PutUint16(p[off:], uint16(int16(lv*32767)))
			binary.LittleEndian.PutUint16(p[off+2:], uint16(int16(rv*32767)))
			// Push mono mix through the delay ring; process the sample from
			// meterDelay ago so the analyser tracks what is actually being heard.
			r.bands.process(r.meterBuf.push((lv + rv) * 0.5))
		}
		for b, level := range r.bands.smooth {
			voice.SetTrackBandLevel(r.track, b, level)
		}
		// Update EMA of DSP wall-clock time so the settings screen can display it.
		dspUs := time.Since(dspStart).Microseconds()
		prev := playbackProcessEMAus.Load()
		ema := dspUs
		if prev != 0 {
			ema = int64(0.9*float64(prev) + 0.1*float64(dspUs))
		}
		playbackProcessEMAus.Store(ema)
		// Remember frame count for next call's jitter baseline.
		r.prevFrames = frames
	}
	return n, err
}

func audioPlaybackErr(track int, op string, err error) AudioPlaybackErrorMsg {
	if err != nil {
		clientlog.Error("audio_player: %s: %v", op, err)
		return AudioPlaybackErrorMsg{Track: track, Text: "audio: " + op + ": " + err.Error()}
	}
	clientlog.Error("audio_player: %s: unknown error", op)
	return AudioPlaybackErrorMsg{Track: track, Text: "audio: " + op + ": unknown error"}
}
