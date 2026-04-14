// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

// Package voice manages the client-side WebRTC voice call lifecycle.
//
// Audio path (send):
//
//	mic PCM → Opus encode → AES-256-GCM encrypt → RTP → broker (SFU) → other peers
//
// Audio path (receive):
//
//	broker → RTP → AES-256-GCM decrypt → Opus decode → PCM → speakers
//
// The broker never sees plaintext audio; it only forwards encrypted RTP payloads./
// The AES-256-GCM key is derived via HKDF(roomKey, "voice").
package voice

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hajimehoshi/oto/v2"
	"github.com/pion/ice/v4"
	"github.com/pion/interceptor"
	media "github.com/pion/mediadevices"
	_ "github.com/pion/mediadevices/pkg/driver/microphone"
	"github.com/pion/mediadevices/pkg/prop"
	"github.com/pion/mediadevices/pkg/wave"
	rtppkg "github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
	wstypes "github.com/srschreiber/nito-client/shared/websocket_types"
	"github.com/srschreiber/nito-client/shellapp/clientlog"
	"github.com/srschreiber/nito-client/shellapp/connection"
	"golang.org/x/crypto/hkdf"
)

const (
	sampleRate       = 48000
	numChannels      = 1 // encode/decode mono; SDP advertises 2 per Opus spec
	sdpChannels      = 2 // Opus RFC 7587 says SDP always lists 2
	payloadType      = 111
	sdpFmtp          = "minptime=10;useinbandfec=1"
	opusFrameMs      = 20                              // 20 ms is the standard Opus frame size
	opusFrameSamples = sampleRate * opusFrameMs / 1000 // 960 samples
	apmFrameSamples  = sampleRate / 100                // 10 ms = 480 samples; WebRTC APM frame size
	opusBufMax       = 4096
	playbackGain     = 35 // percent; dampen all voice playback to keep feedback loops in check
)

// AudioDevice represents a system audio input device.
type AudioDevice struct {
	ID    string
	Label string
}

var (
	muSelected          sync.Mutex
	selectedInputDevice string // empty = system default
)

// SetInputDevice stores the device ID used for the next voice/test-audio session.
func SetInputDevice(id string) {
	muSelected.Lock()
	selectedInputDevice = id
	muSelected.Unlock()
}

// SelectedInputDevice returns the currently selected input device ID (empty = system default).
func SelectedInputDevice() string {
	muSelected.Lock()
	defer muSelected.Unlock()
	return selectedInputDevice
}

// ListAudioInputs returns available audio input devices registered with mediadevices.
// Always includes a "System Default" entry at index 0 with an empty ID.
func ListAudioInputs() []AudioDevice {
	out := []AudioDevice{{ID: "", Label: "System Default"}}
	for _, d := range media.EnumerateDevices() {
		if d.Kind == media.AudioInput {
			out = append(out, AudioDevice{ID: d.DeviceID, Label: prettifyDeviceLabel(d.Label)})
		}
	}
	return out
}

// prettifyDeviceLabel decodes the hex-encoded CoreAudio UID that pion/mediadevices
// returns as the Label field and converts it to a readable name.
//
// CoreAudio USB device UIDs have the form:
//
//	AppleUSBAudioEngine:Manufacturer:DeviceName:Serial:Index
//
// Built-in devices use names like "BuiltInMicrophoneDevice".
func prettifyDeviceLabel(raw string) string {
	b, err := hex.DecodeString(raw)
	if err != nil {
		return raw // already a plain string; return as-is
	}
	s := string(b)

	// USB audio engine — extract the device name field (index 2).
	if strings.HasPrefix(s, "AppleUSBAudioEngine:") {
		parts := strings.SplitN(s, ":", 5)
		if len(parts) >= 3 && parts[2] != "" {
			return parts[2]
		}
	}

	// Well-known built-in device UIDs.
	switch s {
	case "BuiltInMicrophoneDevice":
		return "Built-in Microphone"
	case "BuiltInHeadphoneInputDevice":
		return "Built-in Headphone Input"
	case "BuiltInLineInDevice":
		return "Built-in Line In"
	}

	return s
}

func debugf(format string, args ...any) {
	clientlog.Info(format, args...)
}

var (
	mu            sync.Mutex
	activeSession *voiceSession
	connecting    atomic.Bool

	jitterBufferEnabled atomic.Bool  // if true, use pion's default interceptors (jitter buffer + NACK)
	denoiseOutEnabled   atomic.Bool  // if false, skip outbound RNNoise (microphone)
	denoiseInEnabled    atomic.Bool  // if true, apply inbound RNNoise (received audio)
	aecEnabled          atomic.Bool  // if true, apply WebRTC AEC3 echo cancellation
	pitchEnabled        atomic.Bool  // if true, apply pitch shift to captured audio
	pitchPos            atomic.Int32 // 0–24; 12 = no shift, <12 = lower, >12 = higher
	vibratoEnabled      atomic.Bool  // if true, modulate pitch with a sine oscillator
	vibratoFreq         atomic.Int32 // vibrato rate in Hz, 1–10
	vibratoRange        atomic.Int32 // vibrato depth in half-steps of 0.5 st, 1–6 (= 0.5–3.0 st)

	masterVolume      atomic.Int32 // 0–100, master output volume for all audio sources
	chatSFXOverride   atomic.Int32 // -1 = use master; 0–100 = per-source override for chat SFX
	playbackOverride  atomic.Int32 // -1 = use master; 0–100 = per-source override for .play clips
	voiceChatOverride atomic.Int32 // -1 = use master; 0–100 = per-source override for voice call audio

	otoOnce sync.Once
	otoCtx  *oto.Context

	sendPacketCount atomic.Uint64
	sendByteCount   atomic.Uint64
	encodeTimeNs    atomic.Int64
	encodeFrames    atomic.Uint64
	decodeTimeNs    atomic.Int64
	decodeFrames    atomic.Uint64
	lastEncodeNs    atomic.Int64 // most recent single-frame encode duration (not drained)
	lastDecodeNs    atomic.Int64 // most recent single-frame decode duration (not drained)

	// Loopback latency tracking (used only during JoinSelf test sessions).
	// pipelineSendTimes records the time before encode; measures full encode→network→decode→write latency.
	// networkSendTimes records the time just before WriteRTP; measures pure network round-trip.
	pipelineSendTimes sync.Map     // key: uint16 seq → value: time.Time (before encode)
	networkSendTimes  sync.Map     // key: uint16 seq → value: time.Time (just before WriteRTP)
	loopbackPending   atomic.Int64 // number of unmatched entries (shared cap across both maps)
	pipelineLatNs     atomic.Int64 // latest pipeline latency in nanoseconds
	networkRTTNs      atomic.Int64 // latest network RTT in nanoseconds (ICE stats in real calls, loopback timing in test)
)

const maxLoopbackPending = 200 // stop recording send times if this many are unmatched

func init() {
	denoiseOutEnabled.Store(true) // outbound RNNoise on by default
	denoiseInEnabled.Store(false) // inbound RNNoise off by default
	aecEnabled.Store(true)        // AEC3 on by default
	pitchPos.Store(12)            // center = no shift
	vibratoFreq.Store(4)          // 4 Hz default
	vibratoRange.Store(1)         // 1 × 0.5 st = 0.5 st default
	masterVolume.Store(100)
	chatSFXOverride.Store(-1)   // off = use master
	playbackOverride.Store(-1)  // off = use master
	voiceChatOverride.Store(-1) // off = use master
}

// clampVol5 clamps v to [0, 100] rounding to the nearest multiple of 5.
func clampVol5(v int) int {
	v = ((v + 2) / 5) * 5
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

// MasterVolume returns the master output volume (0–100).
func MasterVolume() int { return int(masterVolume.Load()) }

// SetMasterVolume sets the master output volume, clamped to 0–100 in steps of 5.
// Also updates the active voice call player volume immediately.
func SetMasterVolume(v int) {
	masterVolume.Store(int32(clampVol5(v)))
	mu.Lock()
	sess := activeSession
	mu.Unlock()
	if sess != nil {
		sess.player.SetVolume(EffectiveVoiceChatVolume())
	}
}

// ChatSFXOverride returns the chat SFX volume override: -1 means use master.
func ChatSFXOverride() int { return int(chatSFXOverride.Load()) }

// SetChatSFXOverride sets the chat SFX override. Pass -1 to revert to master.
// Any other value is clamped to 0–100 in steps of 5.
func SetChatSFXOverride(v int) {
	if v < 0 {
		chatSFXOverride.Store(-1)
	} else {
		chatSFXOverride.Store(int32(clampVol5(v)))
	}
}

// PlaybackOverride returns the .play clip volume override: -1 means use master.
func PlaybackOverride() int { return int(playbackOverride.Load()) }

// SetPlaybackOverride sets the .play clip override. Pass -1 to revert to master.
func SetPlaybackOverride(v int) {
	if v < 0 {
		playbackOverride.Store(-1)
	} else {
		playbackOverride.Store(int32(clampVol5(v)))
	}
}

// VoiceChatOverride returns the voice call audio override: -1 means use master.
func VoiceChatOverride() int { return int(voiceChatOverride.Load()) }

// SetVoiceChatOverride sets the voice call audio override. Pass -1 to revert to master.
// Also updates the active voice call player volume immediately.
func SetVoiceChatOverride(v int) {
	if v < 0 {
		voiceChatOverride.Store(-1)
	} else {
		voiceChatOverride.Store(int32(clampVol5(v)))
	}
	mu.Lock()
	sess := activeSession
	mu.Unlock()
	if sess != nil {
		sess.player.SetVolume(EffectiveVoiceChatVolume())
	}
}

// EffectiveChatSFXVolume returns the volume [0,1] to use for chat sound effects.
func EffectiveChatSFXVolume() float64 {
	if o := ChatSFXOverride(); o >= 0 {
		return float64(o) / 100.0
	}
	return float64(MasterVolume()) / 100.0
}

// EffectivePlaybackVolume returns the volume [0,1] to use for .play clips.
func EffectivePlaybackVolume() float64 {
	if o := PlaybackOverride(); o >= 0 {
		return float64(o) / 100.0
	}
	return float64(MasterVolume()) / 100.0
}

// EffectiveVoiceChatVolume returns the volume [0,1] to use for live voice call audio.
func EffectiveVoiceChatVolume() float64 {
	if o := VoiceChatOverride(); o >= 0 {
		return float64(o) / 100.0
	}
	return float64(MasterVolume()) / 100.0
}

// DrainSendStats returns the number of packets and bytes sent since the last call, resetting both counters.
// Intended to be called once per second to compute rates.
func DrainSendStats() (packets uint64, bytes uint64) {
	return sendPacketCount.Swap(0), sendByteCount.Swap(0)
}

// DrainEncodeStats returns the average encode time per frame in milliseconds since the last call.
// Returns 0 if no frames were encoded in the interval.
func DrainEncodeStats() float64 {
	ns := encodeTimeNs.Swap(0)
	frames := encodeFrames.Swap(0)
	if frames == 0 {
		return 0
	}
	return float64(ns) / float64(frames) / float64(time.Millisecond)
}

// DrainDecodeStats returns the average decode time per frame in milliseconds since the last call.
// Returns 0 if no frames were decoded in the interval.
func DrainDecodeStats() float64 {
	ns := decodeTimeNs.Swap(0)
	frames := decodeFrames.Swap(0)
	if frames == 0 {
		return 0
	}
	return float64(ns) / float64(frames) / float64(time.Millisecond)
}

// GetPipelineLatMs returns the latest end-to-end pipeline latency in milliseconds:
// time from before encode to after pw.Write on the receive side. Returns 0 if no data.
func GetPipelineLatMs() float64 {
	ns := pipelineLatNs.Load()
	if ns == 0 {
		return 0
	}
	return float64(ns) / float64(time.Millisecond)
}

// GetNetworkRTTMs returns the latest pure network round-trip time in milliseconds:
// time from just before WriteRTP to just after ReadRTP. Returns 0 if no data.
func GetNetworkRTTMs() float64 {
	ns := networkRTTNs.Load()
	if ns == 0 {
		return 0
	}
	return float64(ns) / float64(time.Millisecond)
}

// JitterBufferEnabled reports whether the jitter buffer is enabled.
func JitterBufferEnabled() bool { return jitterBufferEnabled.Load() }

// SetJitterBufferEnabled enables or disables the WebRTC jitter buffer.
// Takes effect on the next call to Join; does not affect an active session.
func SetJitterBufferEnabled(enabled bool) { jitterBufferEnabled.Store(enabled) }

// DenoiseOutboundEnabled reports whether outbound RNNoise (microphone) is enabled.
func DenoiseOutboundEnabled() bool { return denoiseOutEnabled.Load() }

// SetDenoiseOutboundEnabled enables or disables outbound RNNoise. Takes effect immediately.
func SetDenoiseOutboundEnabled(enabled bool) { denoiseOutEnabled.Store(enabled) }

// DenoiseInboundEnabled reports whether inbound RNNoise (received audio) is enabled.
func DenoiseInboundEnabled() bool { return denoiseInEnabled.Load() }

// SetDenoiseInboundEnabled enables or disables inbound RNNoise. Takes effect immediately.
func SetDenoiseInboundEnabled(enabled bool) { denoiseInEnabled.Store(enabled) }

// AECEnabled reports whether WebRTC AEC3 echo cancellation is enabled.
func AECEnabled() bool { return aecEnabled.Load() }

// SetAECEnabled enables or disables echo cancellation. Takes effect immediately.
func SetAECEnabled(enabled bool) { aecEnabled.Store(enabled) }

// PitchEnabled reports whether pitch shift is enabled.
func PitchEnabled() bool { return pitchEnabled.Load() }

// SetPitchEnabled enables or disables pitch shifting. Takes effect immediately.
func SetPitchEnabled(enabled bool) {
	pitchEnabled.Store(enabled)
}

// PitchPos returns the pitch slider position (0–24; 12 = no shift).
func PitchPos() int { return int(pitchPos.Load()) }

// SetPitchPos sets the pitch slider position, clamped to 0–24.
func SetPitchPos(pos int) {
	if pos < 0 {
		pos = 0
	} else if pos > 24 {
		pos = 24
	}
	pitchPos.Store(int32(pos))
}

// VibratoEnabled reports whether vibrato is enabled.
func VibratoEnabled() bool { return vibratoEnabled.Load() }

// SetVibratoEnabled enables or disables vibrato.
func SetVibratoEnabled(enabled bool) { vibratoEnabled.Store(enabled) }

// VibratoFreq returns the vibrato rate in Hz (1–10).
func VibratoFreq() int { return int(vibratoFreq.Load()) }

// SetVibratoFreq sets the vibrato rate in Hz, clamped to 1–8.
func SetVibratoFreq(hz int) {
	if hz < 1 {
		hz = 1
	} else if hz > 8 {
		hz = 8
	}
	vibratoFreq.Store(int32(hz))
}

// VibratoRange returns the vibrato depth (1–8; each unit = 0.5 semitones).
func VibratoRange() int { return int(vibratoRange.Load()) }

// SetVibratoRange sets the vibrato depth, clamped to 1–6 (= 0.5–3.0 st).
func SetVibratoRange(r int) {
	if r < 1 {
		r = 1
	} else if r > 6 {
		r = 6
	}
	vibratoRange.Store(int32(r))
}

// IsConnecting reports whether a voice join is currently in progress.
func IsConnecting() bool { return connecting.Load() }

// iceRTTMs returns the nominated ICE candidate pair round-trip time in
// milliseconds, or 0 if unavailable.
func iceRTTMs(pc *webrtc.PeerConnection) int64 {
	for _, s := range pc.GetStats() {
		pair, ok := s.(webrtc.ICECandidatePairStats)
		if ok && pair.Nominated {
			return int64(pair.CurrentRoundTripTime * 1000)
		}
	}
	return 0
}

// estimatedStreamDelayMs returns the expected speaker→mic round-trip in
// milliseconds for AEC stream delay configuration.
// This is a local hardware property: output buffer drain time + room
// acoustic path + mic input buffer latency. We base it on the buffer sizes
// we actually configure rather than network stats.
func estimatedStreamDelayMs() int {
	// Output buffer: 2 Opus frames (set via SetBufferSize in joinWithAEAD) = 40ms
	// Mic input buffer: ~1 Opus frame = 20ms
	// Room acoustic margin: ~5ms
	return 2*opusFrameMs + opusFrameMs + 5 // 65ms
}

// IsActive reports whether a voice session is currently live.
func IsActive() bool {
	mu.Lock()
	defer mu.Unlock()
	return activeSession != nil
}

// ActiveRoomID returns the room ID of the current voice session, or "" if none.
func ActiveRoomID() string {
	mu.Lock()
	defer mu.Unlock()
	if activeSession == nil {
		return ""
	}
	return activeSession.roomID
}

// audioBuf is a fixed-size ring buffer for decoded PCM audio.
// Write never blocks — oldest bytes are overwritten when full, keeping latency bounded.
// Read blocks until data is available or the buffer is closed, implementing io.Reader for oto.
// it solves the problem where sample rates don't quite line up between the media source and what
// opus expects (48k). if those get out of sync due to clock drift, it shouldn't cause blocking. instead, a ring buffer will
// silently drop old frames by overwriting them with new frames
type audioBuf struct {
	mu       sync.Mutex
	cond     *sync.Cond
	data     []byte
	readPos  int // index of next byte oto will read
	writePos int // index of next byte to write
	buffered int // number of bytes currently in the buffer
	done     bool
}

func newAudioBuf(size int) *audioBuf {
	ab := &audioBuf{data: make([]byte, size)}
	ab.cond = sync.NewCond(&ab.mu)
	return ab
}

func (ab *audioBuf) Write(p []byte) {
	ab.mu.Lock()
	defer ab.mu.Unlock()
	if ab.done {
		return
	}
	if len(ab.data)-ab.buffered < len(p) {
		return // drop frame — buffer full; avoids moving read pointer and causing a pop
	}
	for _, b := range p {
		ab.data[ab.writePos] = b
		ab.writePos = (ab.writePos + 1) % len(ab.data)
		ab.buffered++
	}
	ab.cond.Signal()
}

func (ab *audioBuf) Read(p []byte) (int, error) {
	ab.mu.Lock()
	defer ab.mu.Unlock()
	if ab.buffered == 0 {
		if ab.done {
			return 0, io.EOF
		}
		// Return silence immediately instead of blocking. The oto mux loop
		// processes all registered players sequentially in one goroutine — if
		// this Read blocks waiting for voice PCM, the mux loop stalls and stops
		// filling the MP3-playback player's buffer, which pauses audio output.
		for i := range p {
			p[i] = 0
		}
		return len(p), nil
	}
	i := 0
	for i < len(p) && ab.buffered > 0 {
		p[i] = ab.data[ab.readPos]
		ab.readPos = (ab.readPos + 1) % len(ab.data)
		ab.buffered--
		i++
	}
	return i, nil
}

func (ab *audioBuf) close() {
	ab.mu.Lock()
	ab.done = true
	ab.cond.Broadcast()
	ab.mu.Unlock()
}

type voiceSession struct {
	roomID       string
	pc           *webrtc.PeerConnection
	sendTrack    *webrtc.TrackLocalStaticRTP
	aead         cipher.AEAD
	player       oto.Player
	ab           *audioBuf
	apm          *apmState       // WebRTC AEC3; nil if init failed
	delayEst     *streamDelayEst // acoustic delay estimator; nil if AEC unavailable
	cancel       context.CancelFunc
	ctx          context.Context
	answerCh     chan string // receives the initial SDP answer; closed after use
	iceRestartCh chan string // receives the SDP answer after an ICE restart
	restarting   atomic.Bool
	onTrackCh    chan struct{} // closed when the first remote track arrives
}

// GetOtoCtx returns the shared oto audio context, initializing it on first call.
func GetOtoCtx() (*oto.Context, error) { return getOtoCtx() }

func getOtoCtx() (*oto.Context, error) {
	var initErr error
	otoOnce.Do(func() {
		var ready chan struct{}
		// Use a 20ms output buffer instead of the ~43ms default (4×2048 bytes at 48kHz stereo).
		// Lower values reduce latency but risk glitches under CPU load; 20ms gives 2 frame-lengths
		// of headroom with 10ms Opus frames.
		otoCtx, ready, initErr = oto.NewContextWithOptions(&oto.NewContextOptions{
			SampleRate:   sampleRate,
			ChannelCount: 2,
			Format:       oto.FormatSignedInt16LE,
			BufferSize:   20 * time.Millisecond,
		})
		if initErr == nil {
			<-ready
		}
	})
	return otoCtx, initErr
}

func newPC() (*webrtc.PeerConnection, error) {
	m := &webrtc.MediaEngine{}
	if err := m.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeOpus,
			ClockRate:   sampleRate,
			Channels:    sdpChannels,
			SDPFmtpLine: sdpFmtp,
		},
		PayloadType: payloadType,
	}, webrtc.RTPCodecTypeAudio); err != nil {
		return nil, fmt.Errorf("register codec: %w", err)
	}
	se := webrtc.SettingEngine{}
	se.SetICEMulticastDNSMode(ice.MulticastDNSModeDisabled)
	opts := []func(*webrtc.API){webrtc.WithMediaEngine(m), webrtc.WithSettingEngine(se)}
	if !jitterBufferEnabled.Load() {
		opts = append(opts, webrtc.WithInterceptorRegistry(&interceptor.Registry{}))
	}
	api := webrtc.NewAPI(opts...)
	return api.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		},
	})
}

func deriveVoiceKey(roomKeyBytes []byte) ([]byte, error) {
	r := hkdf.New(sha256.New, roomKeyBytes, nil, []byte("voice"))
	key := make([]byte, 32)
	if _, err := io.ReadFull(r, key); err != nil {
		return nil, fmt.Errorf("hkdf: %w", err)
	}
	return key, nil
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func encryptFrame(aead cipher.AEAD, plaintext []byte) ([]byte, error) {
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return append(nonce, aead.Seal(nil, nonce, plaintext, nil)...), nil
}

func decryptFrame(aead cipher.AEAD, payload []byte) ([]byte, error) {
	ns := aead.NonceSize()
	if len(payload) < ns {
		return nil, fmt.Errorf("payload too short")
	}
	return aead.Open(nil, payload[:ns], payload[ns:], nil)
}

// SelfRoomID is the special roomID the broker uses to loop audio back to the sender.
const SelfRoomID = "self"

// JoinSelf starts a loopback audio test: audio is sent to the broker with roomID "self"
// and the broker relays it straight back. Uses a random ephemeral key since both the
// encrypt and decrypt paths are in the same process.
//
// The broker occasionally doesn't set up the return track on the first connection.
// JoinSelf retries automatically (up to 3 attempts) if no remote track arrives within 3s.
func JoinSelf() error {
	const maxAttempts = 3
	const trackTimeout = 15 * time.Second

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return fmt.Errorf("voice test: generate key: %w", err)
		}
		aead, err := newAEAD(key)
		if err != nil {
			return fmt.Errorf("voice test: aead: %w", err)
		}
		if err := joinWithAEAD(SelfRoomID, aead); err != nil {
			return err
		}

		mu.Lock()
		trackCh := activeSession.onTrackCh
		sessCtx := activeSession.ctx
		mu.Unlock()

		select {
		case <-trackCh:
			if attempt > 1 {
				debugf("voice: self-test loopback established on attempt %d", attempt)
			}
			return nil
		case <-sessCtx.Done():
			// External Leave() was called — abort retries.
			return fmt.Errorf("voice test: cancelled")
		case <-time.After(trackTimeout):
			_ = Leave(SelfRoomID)
			if attempt < maxAttempts {
				debugf("voice: self-test no return track after %.0fs, retrying (%d/%d)", trackTimeout.Seconds(), attempt, maxAttempts)
				time.Sleep(200 * time.Millisecond)
			}
		}
	}
	return fmt.Errorf("voice test: loopback not established after %d attempts", maxAttempts)
}

// Join starts a voice call in roomID. Requires an active session with a selected room.
// Returns once the WebRTC connection is signalled; media flows asynchronously.
// 1. Client creates offer → sets it as local description
// 2. Client sends offer to broker
// 3. Broker sets client's offer as its remote description
// 4. Broker creates answer → sets it as its local description
// 5. Broker sends answer to client
// 6. Client sets broker's answer as its remote description
func Join(roomID string) error {
	rawKeyBytes, err := connection.GetRoomKeyBytes()
	if err != nil {
		return fmt.Errorf("voice join: room key: %w", err)
	}
	voiceKey, err := deriveVoiceKey(rawKeyBytes)
	if err != nil {
		return fmt.Errorf("voice join: derive voice key: %w", err)
	}
	aead, err := newAEAD(voiceKey)
	if err != nil {
		return fmt.Errorf("voice join: aead: %w", err)
	}
	return joinWithAEAD(roomID, aead)
}

func joinWithAEAD(roomID string, aead cipher.AEAD) error {
	connecting.Store(true)
	defer connecting.Store(false)
	debugf("voice: join start roomID=%s", roomID)
	mu.Lock()
	if activeSession != nil {
		mu.Unlock()
		return fmt.Errorf("already in a voice session")
	}
	mu.Unlock()
	debugf("voice: keys ok")

	pc, err := newPC()
	if err != nil {
		return fmt.Errorf("voice join: new pc: %w", err)
	}
	debugf("voice: peer connection created")

	sendTrack, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{
		MimeType:    webrtc.MimeTypeOpus,
		ClockRate:   sampleRate,
		Channels:    sdpChannels,
		SDPFmtpLine: sdpFmtp,
	}, "opus", "voice-stream")
	if err != nil {
		pc.Close()
		return fmt.Errorf("voice join: new send track: %w", err)
	}
	if _, err := pc.AddTrack(sendTrack); err != nil {
		pc.Close()
		return fmt.Errorf("voice join: add track: %w", err)
	}

	debugf("voice: initialising audio output")
	oc, err := getOtoCtx()
	if err != nil {
		pc.Close()
		return fmt.Errorf("voice join: oto: %w", err)
	}
	debugf("voice: audio output ready")
	ab := newAudioBuf(sampleRate * 4 * 10) // 10 seconds of stereo int16 — reduces drift-pop frequency
	debugf("voice: creating player")
	player := oc.NewPlayer(ab)
	bufferSizeSetter, ok := player.(oto.BufferSizeSetter)
	if ok {
		debugf("voice: setting player buffer size to 20ms")
		bufferSizeSetter.SetBufferSize(opusFrameSamples * numChannels * 2 * 2) // 2 frames = 40 ms
	} else {
		debugf("voice: player does not support buffer size setter")
	}
	debugf("voice: starting player")
	player.SetVolume(EffectiveVoiceChatVolume())
	go player.Play() // Play() can block on Windows waiting for the audio device; don't hold up Join.
	debugf("voice: player started")

	// Create APM before OnTrack so both the receive and capture closures share it.
	apm, apmErr := newAPMState(sampleRate, numChannels)
	var de *streamDelayEst
	if apmErr != nil {
		debugf("voice: AEC init failed (continuing without): %v", apmErr)
		apm = nil
	} else {
		initDelayMs := estimatedStreamDelayMs()
		apm.SetStreamDelay(initDelayMs)
		de = newStreamDelayEst(initDelayMs, apm)
	}

	onTrackCh := make(chan struct{})
	var onTrackOnce sync.Once

	// Receive incoming tracks: decrypt → decode Opus → PCM → speakers.
	pc.OnTrack(func(remote *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		debugf("voice: OnTrack fired, ssrc=%d", remote.SSRC())
		onTrackOnce.Do(func() { close(onTrackCh) })
		dec, err := newOpusDecoder(sampleRate, numChannels)
		if err != nil {
			return
		}
		defer dec.close()
		comp := &inboundCompressor{}
		denoiseIn, denoiseInErr := newRNNoiseState()
		if denoiseInErr != nil {
			debugf("voice: new inbound rnnoise state, will not denoise: %v", denoiseInErr)
		} else {
			defer denoiseIn.Destroy()
		}
		pcmBuf := make([]int16, opusFrameSamples*numChannels)
		isLoopback := roomID == SelfRoomID
		var lastSeq uint16
		seenFirst := false
		for {
			pkt, _, err := remote.ReadRTP()
			if err != nil {
				return
			}
			// Network RTT: measured from just before WriteRTP to just after ReadRTP.
			if isLoopback {
				if v, ok := networkSendTimes.LoadAndDelete(pkt.Header.SequenceNumber); ok {
					networkRTTNs.Store(time.Since(v.(time.Time)).Nanoseconds())
				}
			}
			// PLC: synthesize audio for any skipped sequence numbers.
			seq := pkt.Header.SequenceNumber
			if seenFirst {
				if int16(seq-lastSeq) <= 0 {
					continue // late or duplicate; already PLC'd past it
				}
				gap := int(seq-lastSeq) - 1 // wraps correctly for uint16
				for i := 0; i < gap && i < 4; i++ {
					if n, err := dec.decodePLC(pcmBuf); err == nil {
						ab.Write(int16ToBytes(pcmBuf[:n*numChannels]))
					}
				}
			}
			lastSeq = seq
			seenFirst = true
			plain, err := decryptFrame(aead, pkt.Payload)
			if err != nil {
				continue
			}
			decodeStart := time.Now()
			n, err := dec.decode(plain, pcmBuf)
			decDur := time.Since(decodeStart).Nanoseconds()
			decodeTimeNs.Add(decDur)
			decodeFrames.Add(1)
			lastDecodeNs.Store(decDur)
			if err != nil {
				continue
			}
			if denoiseIn != nil && denoiseInEnabled.Load() {
				f32 := pcm16ToFloat32(pcmBuf[:n*numChannels])
				_ = denoiseIn.ProcessFrame(f32)
				copy(pcmBuf[:n*numChannels], float32ToPCM16(f32))
			}
			total := n * numChannels
			// Dampen playback to keep the closed-loop gain below 1 and prevent
			// feedback loops regardless of whether AEC is active.
			samples := pcmBuf[:total]
			for i, s := range samples {
				samples[i] = int16(int32(s) * playbackGain / 100)
			}
			// Compress: clamp sudden loud bursts to a comfortable range without
			// affecting normal speech levels. Applied after the gain reduction so
			// the compressor threshold is relative to the dampened signal.
			comp.process(samples)
			// AEC reverse path: record each decoded frame as far-end reference
			// *before* writing it to the speaker. The AEC builds a model of the
			// echo path from these frames so it knows what to subtract later when
			// the mic picks up the same audio bouncing back from the room.
			if apm != nil && aecEnabled.Load() {
				for i := 0; i+apmFrameSamples <= total; i += apmFrameSamples {
					_ = apm.ProcessReverse(pcmBuf[i : i+apmFrameSamples])
				}
				if de != nil {
					de.addReverse(pcmBuf[:total])
				}
			}
			ab.Write(int16ToBytes(pcmBuf[:total]))
			// Pipeline latency: measured from before encode to after ab.Write.
			if isLoopback {
				if v, ok := pipelineSendTimes.LoadAndDelete(pkt.Header.SequenceNumber); ok {
					loopbackPending.Add(-1)
					pipelineLatNs.Store(time.Since(v.(time.Time)).Nanoseconds())
				}
			}
		}
	})

	// Create offer and gather ICE.
	// SetLocalDescription triggers mDNS registration which blocks forever on Windows,
	// so we run the entire gather sequence in a goroutine and select on a result channel.
	debugf("voice: creating offer")
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		pc.Close()
		player.Close()
		return fmt.Errorf("voice join: create offer: %w", err)
	}
	gatherDone := webrtc.GatheringCompletePromise(pc)
	setLocalErrCh := make(chan error, 1)
	go func() {
		debugf("voice: setting local description")
		if err := pc.SetLocalDescription(offer); err != nil {
			debugf("voice: set local desc error: %v", err)
			setLocalErrCh <- err
			return
		}
		debugf("voice: waiting for ICE gathering")
		<-gatherDone
		debugf("voice: ICE gathering complete")
		setLocalErrCh <- nil
	}()
	select {
	case err := <-setLocalErrCh:
		if err != nil {
			go func() {
				ab.close()
				_ = player.Close()
				_ = pc.Close()
			}()
			return fmt.Errorf("voice join: set local desc: %w", err)
		}
	case <-time.After(10 * time.Second):
		debugf("voice: ICE gathering timed out")
		go func() {
			ab.close()
			_ = player.Close()
			_ = pc.Close()
		}()
		return fmt.Errorf("voice join: ICE gathering timeout")
	}

	s := connection.CurrentSession()
	if s == nil {
		pc.Close()
		player.Close()
		return fmt.Errorf("voice join: not connected")
	}

	// Set up the session and register the message handler BEFORE sending the
	// offer so a fast local broker's answer isn't dropped.
	answerCh := make(chan string, 1)
	ctx, cancel := context.WithCancel(context.Background())
	sess := &voiceSession{
		roomID: roomID, pc: pc, sendTrack: sendTrack,
		aead: aead, player: player, ab: ab, apm: apm, delayEst: de, ctx: ctx, cancel: cancel,
		answerCh: answerCh, iceRestartCh: make(chan string, 1),
		onTrackCh: onTrackCh,
	}
	mu.Lock()
	activeSession = sess
	mu.Unlock()

	// Periodically update network stats and refresh AEC stream delay.
	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				// Update network RTT from ICE stats (works in both real calls and loopback).
				if rtt := iceRTTMs(pc); rtt > 0 {
					networkRTTNs.Store(rtt * 1_000_000)
				}
				// Synthesize pipeline latency from measured encode + network + decode times.
				encNs := lastEncodeNs.Load()
				decNs := lastDecodeNs.Load()
				netNs := networkRTTNs.Load()
				if encNs > 0 && decNs > 0 && netNs > 0 {
					pipelineLatNs.Store(encNs + netNs + decNs)
				}
				// Stream delay is now updated by the streamDelayEst in captureAndSend.
			}
		}
	}()

	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		if state == webrtc.PeerConnectionStateFailed {
			debugf("voice: connection failed, initiating ICE restart")
			go iceRestart(sess)
		}
	})
	connection.SetVoiceMessageHandler(handleIncoming)

	debugf("voice: sending offer to broker")
	payload, _ := json.Marshal(wstypes.VoiceJoinPayload{
		RoomID: roomID, SDPOffer: pc.LocalDescription().SDP,
	})
	wsMsg := wstypes.ToBrokerWsMessage{
		RPCName: wstypes.RPCVoiceJoin, RequestID: fmt.Sprintf("%d", time.Now().UnixNano()),
		UserID: s.UserID, Nonce: fmt.Sprintf("%d", time.Now().UnixNano()),
		Timestamp: time.Now().Unix(), Payload: payload,
	}
	data, _ := json.Marshal(wsMsg)
	if err := connection.Send(data); err != nil {
		Leave(roomID)
		return fmt.Errorf("voice join: send: %w", err)
	}
	debugf("voice: offer sent, waiting for broker answer")

	select {
	case sdpAnswer := <-answerCh:
		if err := pc.SetRemoteDescription(webrtc.SessionDescription{
			Type: webrtc.SDPTypeAnswer, SDP: sdpAnswer,
		}); err != nil {
			debugf("voice: set remote desc error: %v", err)
			Leave(roomID)
			return fmt.Errorf("voice join: set remote desc: %w", err)
		}
		debugf("voice: remote description set, join complete")
	case <-time.After(10 * time.Second):
		Leave(roomID)
		return fmt.Errorf("voice join: timeout waiting for broker answer")
	}

	go captureAndSend(ctx, aead, sendTrack, roomID == SelfRoomID, apm, de)
	return nil
}

// LeaveIfActive tears down the active voice session regardless of room ID.
// Safe to call when no session is active.
func LeaveIfActive() {
	mu.Lock()
	sess := activeSession
	mu.Unlock()
	if sess == nil {
		return
	}
	Leave(sess.roomID)
}

// Leave ends the active voice session for roomID.
func Leave(roomID string) error {
	mu.Lock()
	sess := activeSession
	if sess != nil && sess.roomID == roomID {
		activeSession = nil
	} else {
		sess = nil
	}
	mu.Unlock()

	if sess == nil {
		return nil
	}
	sess.cancel()
	sess.ab.close()
	_ = sess.player.Close() // release oto resources so the next session starts clean
	_ = sess.pc.Close()
	if sess.delayEst != nil {
		sess.delayEst.destroy() // clear APM ref before APM is destroyed
	}
	if sess.apm != nil {
		sess.apm.Destroy()
		sess.apm = nil
	}

	if roomID == SelfRoomID {
		pipelineSendTimes.Clear()
		networkSendTimes.Clear()
		loopbackPending.Store(0)
	}
	pipelineLatNs.Store(0)
	networkRTTNs.Store(0)
	lastEncodeNs.Store(0)
	lastDecodeNs.Store(0)

	s := connection.CurrentSession()
	if s == nil {
		return nil
	}
	payload, _ := json.Marshal(wstypes.VoiceLeavePayload{RoomID: roomID})
	wsMsg := wstypes.ToBrokerWsMessage{
		RPCName: wstypes.RPCVoiceLeave, RequestID: fmt.Sprintf("%d", time.Now().UnixNano()),
		UserID: s.UserID, Nonce: fmt.Sprintf("%d", time.Now().UnixNano()),
		Timestamp: time.Now().Unix(), Payload: payload,
	}
	data, _ := json.Marshal(wsMsg)
	return connection.Send(data)
}

// handleIncoming is the voice message handler registered with connection.SetVoiceMessageHandler.
func handleIncoming(rpcName string, payload []byte) {
	mu.Lock()
	sess := activeSession
	mu.Unlock()

	switch rpcName {
	case wstypes.RPCVoiceAnswer:
		if sess == nil {
			return
		}
		var ans wstypes.VoiceAnswerPayload
		if err := json.Unmarshal(payload, &ans); err != nil {
			return
		}
		select {
		case sess.answerCh <- ans.SDPAnswer:
		default:
		}

	case wstypes.RPCVoiceICERestartAnswer:
		if sess == nil {
			return
		}
		var ans wstypes.VoiceICERestartAnswerPayload
		if err := json.Unmarshal(payload, &ans); err != nil {
			return
		}
		select {
		case sess.iceRestartCh <- ans.SDPAnswer:
		default:
		}

	case wstypes.RPCVoiceOffer:
		if sess == nil {
			return
		}
		var offer wstypes.VoiceOfferPayload
		if err := json.Unmarshal(payload, &offer); err != nil || offer.RoomID != sess.roomID {
			return
		}
		go func() {
			// The broker may send a renegotiation offer before the client has finished
			// processing the initial answer (i.e. while in have-local-offer state).
			// Wait for stable state before applying the renegotiation.
			for i := 0; i < 50; i++ {
				if sess.pc.SignalingState() == webrtc.SignalingStateStable {
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
			if sess.pc.SignalingState() != webrtc.SignalingStateStable {
				debugf("voice: reneg: timed out waiting for stable state, dropping offer")
				return
			}
			if err := sess.pc.SetRemoteDescription(webrtc.SessionDescription{
				Type: webrtc.SDPTypeOffer, SDP: offer.SDPOffer,
			}); err != nil {
				debugf("voice: reneg set remote desc: %v", err)
				return
			}
			answer, err := sess.pc.CreateAnswer(nil)
			if err != nil {
				debugf("voice: reneg create answer: %v", err)
				return
			}
			gatherDone := webrtc.GatheringCompletePromise(sess.pc)
			if err := sess.pc.SetLocalDescription(answer); err != nil {
				debugf("voice: reneg set local desc: %v", err)
				return
			}
			<-gatherDone

			s := connection.CurrentSession()
			if s == nil {
				return
			}
			respPayload, _ := json.Marshal(wstypes.VoiceRenegAnswerPayload{
				RoomID: offer.RoomID, SDPAnswer: sess.pc.LocalDescription().SDP,
			})
			wsMsg := wstypes.ToBrokerWsMessage{
				RPCName: wstypes.RPCVoiceRenegAnswer, RequestID: fmt.Sprintf("%d", time.Now().UnixNano()),
				UserID: s.UserID, Nonce: fmt.Sprintf("%d", time.Now().UnixNano()),
				Timestamp: time.Now().Unix(), Payload: respPayload,
			}
			data, _ := json.Marshal(wsMsg)
			_ = connection.Send(data)
		}()
	}
}

// iceRestart initiates an ICE restart when the peer connection enters the Failed state.
func iceRestart(sess *voiceSession) {
	if !sess.restarting.CompareAndSwap(false, true) {
		return
	}
	defer sess.restarting.Store(false)

	mu.Lock()
	active := activeSession == sess
	mu.Unlock()
	if !active {
		return
	}

	offer, err := sess.pc.CreateOffer(&webrtc.OfferOptions{ICERestart: true})
	if err != nil {
		debugf("voice: ice restart create offer: %v", err)
		return
	}
	gatherDone := webrtc.GatheringCompletePromise(sess.pc)
	if err := sess.pc.SetLocalDescription(offer); err != nil {
		debugf("voice: ice restart set local desc: %v", err)
		return
	}
	<-gatherDone

	s := connection.CurrentSession()
	if s == nil {
		return
	}
	payload, _ := json.Marshal(wstypes.VoiceICERestartPayload{
		RoomID: sess.roomID, SDPOffer: sess.pc.LocalDescription().SDP,
	})
	wsMsg := wstypes.ToBrokerWsMessage{
		RPCName: wstypes.RPCVoiceICERestart, RequestID: fmt.Sprintf("%d", time.Now().UnixNano()),
		UserID: s.UserID, Nonce: fmt.Sprintf("%d", time.Now().UnixNano()),
		Timestamp: time.Now().Unix(), Payload: payload,
	}
	data, _ := json.Marshal(wsMsg)
	if err := connection.Send(data); err != nil {
		return
	}

	select {
	case sdpAnswer := <-sess.iceRestartCh:
		if err := sess.pc.SetRemoteDescription(webrtc.SessionDescription{
			Type: webrtc.SDPTypeAnswer, SDP: sdpAnswer,
		}); err != nil {
			debugf("voice: ice restart set remote desc: %v", err)
		}
	case <-time.After(10 * time.Second):
		debugf("voice: ice restart timeout")
	}
}

// captureAndSend captures microphone audio, encodes each 20ms frame to Opus,
// and writes it to the WebRTC send track.
func captureAndSend(ctx context.Context, aead cipher.AEAD, track *webrtc.TrackLocalStaticRTP, isLoopback bool, apm *apmState, de *streamDelayEst) {
	muSelected.Lock()
	inputID := selectedInputDevice
	muSelected.Unlock()

	// Log available input devices for diagnostics.
	var inputDeviceIDs []string
	for _, d := range media.EnumerateDevices() {
		if d.Kind == media.AudioInput {
			inputDeviceIDs = append(inputDeviceIDs, d.DeviceID)
		}
	}
	clientlog.Info("captureAndSend: selected=%q available inputs=%d %v", inputID, len(inputDeviceIDs), inputDeviceIDs)

	stream, err := media.GetUserMedia(media.MediaStreamConstraints{
		Audio: func(c *media.MediaTrackConstraints) {
			if inputID != "" {
				c.DeviceID = prop.StringExact(inputID)
			}
		},
	})
	if err != nil && inputID != "" {
		// Selected device unavailable — fall back to system default.
		clientlog.Warn("audio input device unavailable, falling back to default: %v", err)
		stream, err = media.GetUserMedia(media.MediaStreamConstraints{
			Audio: func(c *media.MediaTrackConstraints) {},
		})
	}
	if err != nil {
		clientlog.Error("voice: get user media failed: %v", err)
		return
	}
	tracks := stream.GetAudioTracks()
	if len(tracks) == 0 {
		debugf("voice: no audio tracks from microphone")
		return
	}
	audioTrack := tracks[0]
	defer func() { _ = audioTrack.Close() }()

	enc, err := newOpusEncoder(sampleRate, numChannels)
	if err != nil {
		debugf("voice: new opus encoder: %v", err)
		return
	}

	denoise, err := newRNNoiseState()
	if err != nil {
		debugf("voice: new rnnoise state, will not denoise: %v", err)
	}

	// signalsmith-stretch pitch shifter: 2048-sample block (~43 ms), 512-sample interval (~11 ms).
	// Creates ~43 ms of additional send-side latency when pitch shift is active.
	const ssBlock = 2048
	const ssInterval = 512
	pitch, pitchErr := newSSStretch(sampleRate, numChannels, ssBlock, ssInterval)
	if pitchErr != nil {
		debugf("voice: signalsmith-stretch init failed: %v", pitchErr)
	}
	pitchOut := make([]float32, opusFrameSamples*numChannels)
	var vibratoPhase float64 // radians; advances each frame

	defer enc.close()
	if pitch != nil {
		defer pitch.close()
	}
	// note: brokers are rate-limited, so messing with this value can result in
	// dropped packets
	enc.setBitrate(24000)
	enc.setPacketLossPerc(1)
	enc.setDTX(true)

	reader := audioTrack.(*media.AudioTrack).NewReader(false)
	var seq uint32
	var ts uint32
	var pcmAccum []int16
	opusBuf := make([]byte, opusBufMax)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		chunk, release, err := reader.Read()
		if err != nil {
			debugf("voice: reader read: %v", err)
			return
		}
		pcm, err := chunkToInt16(chunk)
		release()
		if err != nil {
			debugf("voice: chunk convert: %v", err)
			continue
		}
		pcmAccum = append(pcmAccum, pcm...)

		for len(pcmAccum) >= opusFrameSamples*numChannels {
			seqNumber := uint16(atomic.AddUint32(&seq, 1))

			if isLoopback && loopbackPending.Load() < maxLoopbackPending {
				pipelineSendTimes.Store(seqNumber, time.Now()) // before encode
				loopbackPending.Add(1)
			}

			frame := pcmAccum[:opusFrameSamples*numChannels]
			pcmAccum = pcmAccum[opusFrameSamples*numChannels:]

			encodeStart := time.Now()

			// AEC capture path: cancel echo from the mic signal using the
			// reference frames fed via ProcessReverse. Any speaker audio that
			// leaked back into the mic is subtracted here, leaving (ideally)
			// only the speaker's own voice. Must run before RNNoise so the
			// noise suppressor works on an already-clean signal.
			if apm != nil && aecEnabled.Load() {
				for i := 0; i+apmFrameSamples <= len(frame); i += apmFrameSamples {
					_ = apm.ProcessCapture(frame[i : i+apmFrameSamples])
				}
				if de != nil {
					de.addCapture(frame)
				}
			}

			if denoise != nil && denoiseOutEnabled.Load() {
				f32 := pcm16ToFloat32(frame)
				_ = denoise.ProcessFrame(f32)
				frame = float32ToPCM16(f32)
			}

			usePitch := pitch != nil && (pitchEnabled.Load() || vibratoEnabled.Load())
			if usePitch {
				semitones := float64(0)
				if pitchEnabled.Load() {
					semitones += float64(pitchPos.Load() - 12)
				}
				if vibratoEnabled.Load() {
					depth := float64(vibratoRange.Load()) * 0.5 // in semitones
					semitones += depth * math.Sin(vibratoPhase)
					frameDur := float64(opusFrameSamples) / float64(sampleRate)
					vibratoPhase += 2 * math.Pi * float64(vibratoFreq.Load()) * frameDur
					if vibratoPhase >= 2*math.Pi {
						vibratoPhase -= 2 * math.Pi
					}
				}
				pitch.setSemitones(float32(semitones))
				f32 := pcm16ToFloat32(frame)
				pitch.process(f32, pitchOut)
				frame = float32ToPCM16(pitchOut)
			}

			n, err := enc.encode(frame, opusBuf)
			encDur := time.Since(encodeStart).Nanoseconds()
			encodeTimeNs.Add(encDur)
			encodeFrames.Add(1)
			lastEncodeNs.Store(encDur)
			if err != nil {
				debugf("voice: opus encode: %v", err)
				continue
			}

			ciphertext, err := encryptFrame(aead, opusBuf[:n])
			if err != nil {
				debugf("voice: encrypt: %v", err)
				continue
			}

			pkt := &rtppkg.Packet{
				Header: rtppkg.Header{
					Version:        2,
					PayloadType:    payloadType,
					SequenceNumber: seqNumber,
					Timestamp:      ts,
					SSRC:           0xDEADBEEF,
					Marker:         true,
				},
				Payload: ciphertext,
			}
			if isLoopback {
				networkSendTimes.Store(seqNumber, time.Now()) // just before WriteRTP
			}
			if err := track.WriteRTP(pkt); err != nil {
				return
			}
			sendPacketCount.Add(1)
			sendByteCount.Add(uint64(len(ciphertext)))
			ts += uint32(opusFrameSamples)
		}
	}
}

// chunkToInt16 converts a mediadevices audio chunk to mono int16 PCM.
//
// On Windows, microphone drivers typically capture stereo (2-channel interleaved).
// Passing stereo data straight to the mono Opus encoder doubles the apparent frame
// size: 480 stereo pairs look like 960 mono samples, so the encoder treats 10ms of
// audio as 20ms — producing half-speed, octave-low playback on the receiver.
// We fix this by downmixing: averaging each (L+R) pair into one mono sample so the
// frame count matches what the encoder expects.
func chunkToInt16(chunk any) ([]int16, error) {
	switch pcm := chunk.(type) {
	case *wave.Int16Interleaved:
		ch := pcm.Size.Channels
		if ch <= 1 {
			out := make([]int16, len(pcm.Data))
			copy(out, pcm.Data)
			return out, nil
		}
		frames := len(pcm.Data) / ch
		out := make([]int16, frames)
		for i := range out {
			var sum int32
			for c := 0; c < ch; c++ {
				sum += int32(pcm.Data[i*ch+c])
			}
			out[i] = int16(sum / int32(ch))
		}
		return out, nil
	case *wave.Float32Interleaved:
		ch := pcm.Size.Channels
		if ch <= 1 {
			out := make([]int16, len(pcm.Data))
			for i, v := range pcm.Data {
				out[i] = int16(v * 32767)
			}
			return out, nil
		}
		frames := len(pcm.Data) / ch
		out := make([]int16, frames)
		for i := range out {
			var sum float32
			for c := 0; c < ch; c++ {
				sum += pcm.Data[i*ch+c]
			}
			out[i] = int16((sum / float32(ch)) * 32767)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported audio chunk type %T", chunk)
	}
}

// int16ToBytes converts mono int16 PCM to stereo little-endian bytes for the oto player.
// oto is initialized with 2 channels; we upmix by duplicating each mono sample to L and R.
func int16ToBytes(pcm []int16) []byte {
	b := make([]byte, len(pcm)*4) // 2 channels × 2 bytes per sample
	for i, v := range pcm {
		b[i*4] = byte(v)
		b[i*4+1] = byte(v >> 8)
		b[i*4+2] = byte(v)
		b[i*4+3] = byte(v >> 8)
	}
	return b
}

func pcm16ToFloat32(in []int16) []float32 {
	out := make([]float32, len(in))
	for i, s := range in {
		out[i] = float32(s) // not float32(s)/32768
	}
	return out
}

func float32ToPCM16(in []float32) []int16 {
	out := make([]int16, len(in))
	for i, s := range in {
		if s > 32767 {
			s = 32767
		} else if s < -32768 {
			s = -32768
		}
		out[i] = int16(s)
	}
	return out
}
