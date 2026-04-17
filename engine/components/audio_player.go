// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package components

import (
	"bufio"
	"bytes"
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

	mp3 "github.com/hajimehoshi/go-mp3"
	"github.com/srschreiber/nito-client/engine/clientlog"
	"github.com/srschreiber/nito-client/engine/voice"
)

// PlayAudioFromURL returns a tea.Cmd that streams and plays the MP3 (or M3U
// PlayAudioFromURL returns a blocking func that streams and plays the MP3 (or
// M3U playlist) at audioURL on the given track slot. ctx can be cancelled to
// abort early.
func PlayAudioFromURL(ctx context.Context, roomID, audioURL string, track int) func() {
	return func() {
		if roomID != voice.SelfRoomID && voice.ActiveRoomID() != roomID {
			return
		}
		entries, err := resolveAudioURLs(ctx, audioURL)
		if err != nil {
			audioPlaybackErr(track, "resolve", err)
			return
		}
		for _, entry := range entries {
			if ctx.Err() != nil {
				return
			}
			playOne(ctx, roomID, entry, track)
			if ctx.Err() != nil {
				return
			}
		}
	}
}

// PlayAudioFromFile returns a tea.Cmd that plays a local MP3 file on the given
// track slot. It never broadcasts and does not require an active voice call.
// Supports absolute paths and paths beginning with ~/ (expanded to home dir).
func PlayAudioFromFile(ctx context.Context, path string, track int) func() {
	return func() {
		if strings.HasPrefix(path, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				path = filepath.Join(home, path[2:])
			}
		}

		f, err := os.Open(path)
		if err != nil {
			audioPlaybackErr(track, "open file", err)
			return
		}
		defer f.Close()

		// Peek at the first 64 KB for ID3v2 tag parsing, then seek back.
		const id3PeekSize = 64 * 1024
		peek := make([]byte, id3PeekSize)
		n, _ := f.Read(peek)
		peek = peek[:n]
		title, artist := parseID3Title(peek)
		if title != "" || artist != "" {
			voice.SetTrackTitle(track, buildTrackDisplayTitle(artist, title))
		} else {
			// Fall back to the filename (without extension) so the status panel
			// always shows something useful for local files.
			base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
			voice.SetTrackTitle(track, base)
		}
		defer voice.ClearTrackTitle(track)
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			audioPlaybackErr(track, "seek", err)
			return
		}

		dec, err := mp3.NewDecoder(f)
		if err != nil {
			audioPlaybackErr(track, "mp3 decode", err)
			return
		}

		otoCtx, err := voice.GetMusicOtoCtx(dec.SampleRate())
		if err != nil {
			audioPlaybackErr(track, "oto init", err)
			return
		}

		eq := newEQReader(dec, dec.SampleRate(), track)
		defer eq.Close()
		defer voice.ClearTrackBandLevels(track)
		defer voice.ClearTrackEQBandLevels(track)

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
				return
			default:
				if !player.IsPlaying() {
					return
				}
				player.SetVolume(voice.EffectivePlaybackVolume())
				time.Sleep(20 * time.Millisecond)
			}
		}
	}
}

// trackEntry pairs an MP3 URL with an optional display title sourced from
// M3U #EXTINF metadata. The title is used as a hint if ID3v2 tags are absent.
type trackEntry struct {
	URL   string
	Title string // from #EXTINF, empty for plain MP3 URLs
}

// resolveAudioURLs fetches audioURL and returns a slice of track entries.
// For a plain MP3 URL it returns a single-element slice. For an M3U/M3U8
// playlist it parses and returns all track entries in order.
func resolveAudioURLs(ctx context.Context, audioURL string) ([]trackEntry, error) {
	if isM3U(audioURL) {
		entries, err := fetchAndParseM3U(ctx, audioURL)
		if err == nil && len(entries) > 0 {
			return entries, nil
		}
		if err != nil && !strings.Contains(err.Error(), "no tracks found") {
			// Real network or parse error — surface it.
			return nil, err
		}
		// The server either streamed audio directly from the .m3u URL or the
		// playlist contained no recognisable http(s) track lines. Fall back to
		// treating the URL itself as the audio stream.
		clientlog.Info("audio_player: m3u yielded no tracks, attempting direct stream of %s", audioURL)
		return []trackEntry{{URL: audioURL}}, nil
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
	return []trackEntry{{URL: audioURL}}, nil
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

// fetchAndParseM3U downloads the playlist at url and returns all track entries.
// #EXTINF display titles are captured and stored in the corresponding entry.
// Relative URLs are not resolved (archive.org and most public sources use
// absolute URLs).
func fetchAndParseM3U(ctx context.Context, url string) ([]trackEntry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var entries []trackEntry
	var pendingTitle string                                       // title from the preceding #EXTINF line, if any
	scanner := bufio.NewScanner(io.LimitReader(resp.Body, 8<<10)) // 8 KB max — real playlists are tiny
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		// Capture #EXTINF display title: "#EXTINF:duration,Artist - Title"
		if strings.HasPrefix(line, "#EXTINF:") {
			if i := strings.Index(line, ","); i != -1 {
				pendingTitle = strings.TrimSpace(line[i+1:])
			}
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		// Skip binary/non-URL lines (e.g. ICY metadata or Shoutcast frames
		// embedded in some M3U responses). Only http(s) absolute URLs are accepted.
		if !strings.HasPrefix(line, "http://") && !strings.HasPrefix(line, "https://") {
			pendingTitle = ""
			continue
		}
		entries = append(entries, trackEntry{URL: line, Title: pendingTitle})
		pendingTitle = ""
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("no tracks found in playlist")
	}
	clientlog.Info("audio_player: m3u resolved %d track(s) from %s", len(entries), url)
	return entries, nil
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

// playOne streams and plays a single MP3 entry, reconnecting automatically when
// a live stream catches up to the broadcast edge. For regular files it runs once.
// If roomID is voice.SelfRoomID the active-room guard is skipped so the user
// can play audio locally without being in a voice call.
func playOne(ctx context.Context, roomID string, entry trackEntry, track int) {
	for {
		if ctx.Err() != nil {
			return
		}
		reconnect, err := playOneAttempt(ctx, roomID, entry, track)
		if err != nil {
			clientlog.Error("audio_player: track %d: %v", track, err)
			return
		}
		if !reconnect {
			return
		}
		clientlog.Info("audio_player: live edge reached on track %d — reconnecting", track)
		select {
		case <-ctx.Done():
			return
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// liveEdgeDrainTicks is how many consecutive 20 ms polling ticks the prefetch
// must be nearly empty (≤1 chunk) before we reconnect for the live edge.
// 50 ticks × 20 ms = 1 s debounce — gives the stream buffer time to recover.
const liveEdgeDrainTicks = 50

// playOneAttempt makes a single HTTP streaming attempt for entry.URL.
// Returns (reconnect, msg): reconnect=true means a live stream hit the live
// edge and the caller should reopen the connection; msg is non-nil on error.
func playOneAttempt(ctx context.Context, roomID string, entry trackEntry, track int) (bool, error) {
	if roomID != voice.SelfRoomID && voice.ActiveRoomID() != roomID {
		return false, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, entry.URL, nil)
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

	// Set the track display title.
	// For live streams use Icy-Name from the response headers (available
	// immediately). For regular files peek the first 64 KB for ID3v2 tags and
	// fall back to the #EXTINF hint from the playlist.
	if isLive {
		if icyName := resp.Header.Get("Icy-Name"); icyName != "" {
			voice.SetTrackTitle(track, icyName)
		} else if entry.Title != "" {
			voice.SetTrackTitle(track, entry.Title)
		}
	}
	defer voice.ClearTrackTitle(track)

	// Peek the first 64 KB to parse ID3v2 tags (non-live only).
	// The peeked bytes are prepended back via io.MultiReader so the MP3 decoder
	// sees a complete stream.
	const id3PeekSize = 64 * 1024
	peek := make([]byte, id3PeekSize)
	n, _ := io.ReadFull(resp.Body, peek)
	peek = peek[:n]
	if !isLive {
		if title, artist := parseID3Title(peek); title != "" || artist != "" {
			voice.SetTrackTitle(track, buildTrackDisplayTitle(artist, title))
		} else if entry.Title != "" {
			voice.SetTrackTitle(track, entry.Title)
		}
	}
	bodyReader := io.MultiReader(bytes.NewReader(peek), resp.Body)

	prefetch := newPrefetchReader(ctx, bodyReader)
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
	defer voice.ClearTrackBandLevels(track)   // zero the status-bar meter when playback ends
	defer voice.ClearTrackEQBandLevels(track) // zero the EQ graph when playback ends
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
		const attack, release = float32(0.6), float32(0.2)
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
	left, right   channelEffects
	leftPipeline  voice.EffectPipeline
	rightPipeline voice.EffectPipeline
	preScale      float32
	outputGain    float32
	panPhase      float64       // LFO phase for auto-pan; preserved across settings rebuilds
	eqBands       bandAnalyzer  // 16-band analyser → audio settings EQ graph
	meterBuf      meterDelayBuf // ring buffer delays both analysers by meterDelay
	meterSmooth   [4]float32    // per-frame EMA for the status-bar slots (extra smoothing)
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
	r.eqBands.init(float32(sampleRate), voice.NumEQBands)
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
			// Push mono mix through the delay ring; feed the delayed sample to
			// both analysers so both track what is actually being heard.
			delayed := r.meterBuf.push((lv + rv) * 0.5)
			r.eqBands.process(delayed)
		}
		// Publish 16-band EQ levels for the audio settings graph.
		for b := range r.eqBands.smooth {
			voice.SetTrackEQBandLevel(r.track, b, r.eqBands.smooth[b])
		}
		// Map to 4 status-bar slots in a middle-out pattern.
		// Inner bars (1,2) use different frequency buckets so they animate
		// independently. Outer bars (0,3) are the overflow of their inner
		// neighbour, appearing above a low threshold.
		// Scale up by 3× — bass bands are low amplitude relative to full range.
		const meterScale = float32(3.0)
		const overflowThresh = float32(0.12)
		clamp := func(v float32) float32 {
			if v > 1 {
				return 1
			}
			return v
		}
		overflow := func(v float32) float32 {
			if v <= overflowThresh {
				return 0
			}
			return (v - overflowThresh) / (1 - overflowThresh)
		}
		// Inner left = sub-bass (bands 0–2: ~20–100 Hz)
		inner1 := clamp((r.eqBands.smooth[0] + r.eqBands.smooth[1] + r.eqBands.smooth[2]) / 3 * meterScale)
		// Inner right = mid-bass (bands 3–5: ~100–400 Hz)
		inner2 := clamp((r.eqBands.smooth[3] + r.eqBands.smooth[4] + r.eqBands.smooth[5]) / 3 * meterScale)
		// Apply per-frame EMA so the status-bar bars glide smoothly.
		const meterAlpha = float32(0.12)
		targets := [4]float32{overflow(inner1), inner1, inner2, overflow(inner2)}
		for i, t := range targets {
			r.meterSmooth[i] = meterAlpha*t + (1-meterAlpha)*r.meterSmooth[i]
			voice.SetTrackBandLevel(r.track, i, r.meterSmooth[i])
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

func audioPlaybackErr(track int, op string, err error) error {
	if err != nil {
		clientlog.Error("audio_player: %s: %v", op, err)
		return fmt.Errorf("%s: %w", op, err)
	}
	clientlog.Error("audio_player: %s: unknown error", op)
	return fmt.Errorf("%s: unknown error", op)
}

// parseID3Title extracts the title (TIT2) and artist (TPE1) from an ID3v2 tag
// at the start of data. Returns ("", "") if no ID3v2 tag is present or those
// frames are absent. Supports ID3v2.2 (3-byte frame IDs), v2.3, and v2.4.
// No external dependency — uses only stdlib encoding/bytes ops.
func parseID3Title(data []byte) (title, artist string) {
	if len(data) < 10 || data[0] != 'I' || data[1] != 'D' || data[2] != '3' {
		return
	}
	majorVersion := data[3]
	flags := data[5]
	// Synchsafe integer: each byte contributes only 7 bits (mask 0x7f).
	tagSize := int(data[6]&0x7f)<<21 | int(data[7]&0x7f)<<14 | int(data[8]&0x7f)<<7 | int(data[9]&0x7f)
	end := 10 + tagSize
	if end > len(data) {
		end = len(data)
	}
	frames := data[10:end]

	// Skip extended header if the flag bit is set (bit 6 of flags byte).
	// Many taggers (e.g. iTunes) write an extended header; without skipping it
	// the first bytes look like an unknown frame and parsing yields nothing.
	if flags&0x40 != 0 {
		if majorVersion >= 4 {
			// ID3v2.4: extended header size is synchsafe and includes the 4-byte
			// size field itself.
			if len(frames) < 4 {
				return
			}
			extSize := int(frames[0]&0x7f)<<21 | int(frames[1]&0x7f)<<14 |
				int(frames[2]&0x7f)<<7 | int(frames[3]&0x7f)
			if extSize > len(frames) {
				return
			}
			frames = frames[extSize:]
		} else {
			// ID3v2.3: extended header size is a plain big-endian uint32 that
			// does NOT count the 4-byte size field itself.
			if len(frames) < 4 {
				return
			}
			extSize := int(frames[0])<<24 | int(frames[1])<<16 | int(frames[2])<<8 | int(frames[3])
			skip := 4 + extSize
			if skip > len(frames) {
				return
			}
			frames = frames[skip:]
		}
	}

	for len(frames) > 0 {
		var (
			frameID   string
			frameData []byte
			step      int
		)

		if majorVersion == 2 {
			// ID3v2.2: 3-char ID + 3-byte size (big-endian, not synchsafe)
			if len(frames) < 6 {
				break
			}
			frameID = string(frames[0:3])
			if frameID[0] == 0 {
				break // padding
			}
			sz := int(frames[3])<<16 | int(frames[4])<<8 | int(frames[5])
			step = 6 + sz
			if sz == 0 || step > len(frames) {
				break
			}
			frameData = frames[6:step]
		} else {
			// ID3v2.3 and 2.4: 4-char ID + 4-byte size + 2-byte flags
			if len(frames) < 10 {
				break
			}
			frameID = string(frames[0:4])
			if frameID[0] == 0 {
				break // padding
			}
			var sz int
			if majorVersion >= 4 {
				// Synchsafe frame sizes in v2.4
				sz = int(frames[4]&0x7f)<<21 | int(frames[5]&0x7f)<<14 |
					int(frames[6]&0x7f)<<7 | int(frames[7]&0x7f)
			} else {
				sz = int(frames[4])<<24 | int(frames[5])<<16 | int(frames[6])<<8 | int(frames[7])
			}
			step = 10 + sz
			if sz == 0 || step > len(frames) {
				break
			}
			frameData = frames[10:step]
		}
		frames = frames[step:]

		switch frameID {
		case "TIT2", "TT2":
			title = decodeID3Text(frameData)
		case "TPE1", "TP1":
			artist = decodeID3Text(frameData)
		}
		if title != "" && artist != "" {
			return
		}
	}
	return
}

// decodeID3Text decodes an ID3v2 text frame payload. The first byte is the
// text encoding: 0=ISO-8859-1, 1=UTF-16 with BOM, 2=UTF-16BE, 3=UTF-8.
func decodeID3Text(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	enc := data[0]
	text := data[1:]
	switch enc {
	case 0, 3: // ISO-8859-1 or UTF-8 — trim null terminators and return
		for len(text) > 0 && text[len(text)-1] == 0 {
			text = text[:len(text)-1]
		}
		return strings.TrimSpace(string(text))
	case 1, 2: // UTF-16 with BOM (1) or UTF-16BE without BOM (2)
		if len(text) < 2 {
			return ""
		}
		bigEndian := enc == 2
		start := 0
		if text[0] == 0xFF && text[1] == 0xFE {
			bigEndian = false
			start = 2
		} else if text[0] == 0xFE && text[1] == 0xFF {
			bigEndian = true
			start = 2
		}
		var b strings.Builder
		for i := start; i+1 < len(text); i += 2 {
			var cp uint16
			if bigEndian {
				cp = uint16(text[i])<<8 | uint16(text[i+1])
			} else {
				cp = uint16(text[i]) | uint16(text[i+1])<<8
			}
			if cp == 0 {
				break
			}
			b.WriteRune(rune(cp))
		}
		return strings.TrimSpace(b.String())
	}
	return ""
}

// buildTrackDisplayTitle formats artist and title into a single display string.
// If both are present it returns "Artist - Title"; if only one is available it
// returns that field alone.
func buildTrackDisplayTitle(artist, title string) string {
	artist = strings.TrimSpace(artist)
	title = strings.TrimSpace(title)
	switch {
	case artist != "" && title != "":
		return artist + " - " + title
	case title != "":
		return title
	default:
		return artist
	}
}
