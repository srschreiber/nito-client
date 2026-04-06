// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: MIT

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
//
// TODO: apply HKDF(roomKey, "chat") for message encryption too.
package voice

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hajimehoshi/oto/v2"
	"github.com/pion/ice/v4"
	media "github.com/pion/mediadevices"
	_ "github.com/pion/mediadevices/pkg/driver/microphone"
	"github.com/pion/mediadevices/pkg/wave"
	rtppkg "github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
	"golang.org/x/crypto/hkdf"

	wstypes "github.com/srschreiber/nito-client/shared/websocket_types"
	"github.com/srschreiber/nito-client/shellapp/connection"
	"github.com/srschreiber/nito-client/shellapp/keys"
)

const (
	sampleRate       = 48000
	numChannels      = 1 // encode/decode mono; SDP advertises 2 per Opus spec
	sdpChannels      = 2 // Opus RFC 7587 says SDP always lists 2
	payloadType      = 111
	sdpFmtp          = "minptime=10;useinbandfec=1"
	opusFrameMs      = 20                              // 20 ms is the standard Opus frame size
	opusFrameSamples = sampleRate * opusFrameMs / 1000 // 960 samples
	opusBufMax       = 4096
)

var (
	mu            sync.Mutex
	activeSession *voiceSession

	otoOnce sync.Once
	otoCtx  *oto.Context
)

type voiceSession struct {
	roomID       string
	pc           *webrtc.PeerConnection
	sendTrack    *webrtc.TrackLocalStaticRTP
	aead         cipher.AEAD
	player       oto.Player
	pw           *io.PipeWriter
	cancel       context.CancelFunc
	answerCh     chan string // receives the initial SDP answer; closed after use
	iceRestartCh chan string // receives the SDP answer after an ICE restart
	restarting   atomic.Bool
}

func getOtoCtx() (*oto.Context, error) {
	var initErr error
	otoOnce.Do(func() {
		var ready chan struct{}
		otoCtx, ready, initErr = oto.NewContext(sampleRate, numChannels, oto.FormatSignedInt16LE)
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
	api := webrtc.NewAPI(webrtc.WithMediaEngine(m), webrtc.WithSettingEngine(se))
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

// Join starts a voice call in roomID. Requires an active session with a selected room.
// Returns once the WebRTC connection is signalled; media flows asynchronously.
// 1. Client creates offer → sets it as local description
// 2. Client sends offer to broker
// 3. Broker sets client's offer as its remote description
// 4. Broker creates answer → sets it as its local description
// 5. Broker sends answer to client
// 6. Client sets broker's answer as its remote description
func Join(roomID string) error {
	mu.Lock()
	if activeSession != nil {
		mu.Unlock()
		return fmt.Errorf("already in a voice session")
	}
	mu.Unlock()

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

	pc, err := newPC()
	if err != nil {
		return fmt.Errorf("voice join: new pc: %w", err)
	}

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

	oc, err := getOtoCtx()
	if err != nil {
		pc.Close()
		return fmt.Errorf("voice join: oto: %w", err)
	}
	pr, pw := io.Pipe()
	player := oc.NewPlayer(pr)
	player.Play()

	// Receive incoming tracks: decrypt → decode Opus → PCM → speakers.
	pc.OnTrack(func(remote *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		dec, err := newOpusDecoder(sampleRate, numChannels)
		if err != nil {
			return
		}
		defer dec.close()
		pcmBuf := make([]int16, opusFrameSamples*numChannels)
		for {
			pkt, _, err := remote.ReadRTP()
			if err != nil {
				return
			}
			plain, err := decryptFrame(aead, pkt.Payload)
			if err != nil {
				continue
			}
			n, err := dec.decode(plain, pcmBuf)
			if err != nil {
				continue
			}
			if _, err := pw.Write(int16ToBytes(pcmBuf[:n*numChannels])); err != nil {
				return
			}
		}
	})

	// Create offer and gather ICE.
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		pc.Close()
		player.Close()
		return fmt.Errorf("voice join: create offer: %w", err)
	}
	gatherDone := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(offer); err != nil {
		pc.Close()
		player.Close()
		return fmt.Errorf("voice join: set local desc: %w", err)
	}
	select {
	case <-gatherDone:
	case <-time.After(10 * time.Second):
		go func() {
			_ = pw.Close() // EOF to player's pipe reader so oto stops blocking on read.
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
	payload, _ := json.Marshal(wstypes.VoiceJoinPayload{
		RoomID: roomID, SDPOffer: pc.LocalDescription().SDP,
	})
	sig, err := keys.Sign(s.UserID+":"+wstypes.RPCVoiceJoin, s.UserID)
	if err != nil {
		pc.Close()
		player.Close()
		return fmt.Errorf("voice join: sign: %w", err)
	}
	wsMsg := wstypes.ToBrokerWsMessage{
		RPCName: wstypes.RPCVoiceJoin, RequestID: fmt.Sprintf("%d", time.Now().UnixNano()),
		UserID: s.UserID, Nonce: fmt.Sprintf("%d", time.Now().UnixNano()),
		Timestamp: time.Now().Unix(), Signature: sig, Payload: payload,
	}
	data, _ := json.Marshal(wsMsg)
	if err := connection.Send(data); err != nil {
		pc.Close()
		player.Close()
		return fmt.Errorf("voice join: send: %w", err)
	}

	answerCh := make(chan string, 1)
	ctx, cancel := context.WithCancel(context.Background())
	sess := &voiceSession{
		roomID: roomID, pc: pc, sendTrack: sendTrack,
		aead: aead, player: player, pw: pw, cancel: cancel,
		answerCh: answerCh, iceRestartCh: make(chan string, 1),
	}
	mu.Lock()
	activeSession = sess
	mu.Unlock()

	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		if state == webrtc.PeerConnectionStateFailed {
			log.Printf("voice: connection failed, initiating ICE restart")
			go iceRestart(sess)
		}
	})

	// Register handler before we wait, so the answer isn't missed.
	connection.SetVoiceMessageHandler(handleIncoming)

	select {
	case sdpAnswer := <-answerCh:
		if err := pc.SetRemoteDescription(webrtc.SessionDescription{
			Type: webrtc.SDPTypeAnswer, SDP: sdpAnswer,
		}); err != nil {
			Leave(roomID)
			return fmt.Errorf("voice join: set remote desc: %w", err)
		}
	case <-time.After(10 * time.Second):
		Leave(roomID)
		return fmt.Errorf("voice join: timeout waiting for broker answer")
	}

	go captureAndSend(ctx, aead, sendTrack)
	return nil
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
	_ = sess.pw.Close()
	_ = sess.pc.Close()

	s := connection.CurrentSession()
	if s == nil {
		return nil
	}
	payload, _ := json.Marshal(wstypes.VoiceLeavePayload{RoomID: roomID})
	sig, _ := keys.Sign(s.UserID+":"+wstypes.RPCVoiceLeave, s.UserID)
	wsMsg := wstypes.ToBrokerWsMessage{
		RPCName: wstypes.RPCVoiceLeave, RequestID: fmt.Sprintf("%d", time.Now().UnixNano()),
		UserID: s.UserID, Nonce: fmt.Sprintf("%d", time.Now().UnixNano()),
		Timestamp: time.Now().Unix(), Signature: sig, Payload: payload,
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
			if err := sess.pc.SetRemoteDescription(webrtc.SessionDescription{
				Type: webrtc.SDPTypeOffer, SDP: offer.SDPOffer,
			}); err != nil {
				log.Printf("voice: reneg set remote desc: %v", err)
				return
			}
			answer, err := sess.pc.CreateAnswer(nil)
			if err != nil {
				log.Printf("voice: reneg create answer: %v", err)
				return
			}
			gatherDone := webrtc.GatheringCompletePromise(sess.pc)
			if err := sess.pc.SetLocalDescription(answer); err != nil {
				log.Printf("voice: reneg set local desc: %v", err)
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
			sig, _ := keys.Sign(s.UserID+":"+wstypes.RPCVoiceRenegAnswer, s.UserID)
			wsMsg := wstypes.ToBrokerWsMessage{
				RPCName: wstypes.RPCVoiceRenegAnswer, RequestID: fmt.Sprintf("%d", time.Now().UnixNano()),
				UserID: s.UserID, Nonce: fmt.Sprintf("%d", time.Now().UnixNano()),
				Timestamp: time.Now().Unix(), Signature: sig, Payload: respPayload,
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
		log.Printf("voice: ice restart create offer: %v", err)
		return
	}
	gatherDone := webrtc.GatheringCompletePromise(sess.pc)
	if err := sess.pc.SetLocalDescription(offer); err != nil {
		log.Printf("voice: ice restart set local desc: %v", err)
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
	sig, err := keys.Sign(s.UserID+":"+wstypes.RPCVoiceICERestart, s.UserID)
	if err != nil {
		return
	}
	wsMsg := wstypes.ToBrokerWsMessage{
		RPCName: wstypes.RPCVoiceICERestart, RequestID: fmt.Sprintf("%d", time.Now().UnixNano()),
		UserID: s.UserID, Nonce: fmt.Sprintf("%d", time.Now().UnixNano()),
		Timestamp: time.Now().Unix(), Signature: sig, Payload: payload,
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
			log.Printf("voice: ice restart set remote desc: %v", err)
		}
	case <-time.After(10 * time.Second):
		log.Printf("voice: ice restart timeout")
	}
}

// captureAndSend captures microphone audio, encodes each 20ms frame to Opus,
// and writes it to the WebRTC send track.
func captureAndSend(ctx context.Context, aead cipher.AEAD, track *webrtc.TrackLocalStaticRTP) {
	stream, err := media.GetUserMedia(media.MediaStreamConstraints{
		Audio: func(c *media.MediaTrackConstraints) {},
	})
	if err != nil {
		log.Printf("voice: get user media: %v", err)
		return
	}
	tracks := stream.GetAudioTracks()
	if len(tracks) == 0 {
		log.Printf("voice: no audio tracks from microphone")
		return
	}
	audioTrack := tracks[0]
	defer audioTrack.Close()

	enc, err := newOpusEncoder(sampleRate, numChannels)
	if err != nil {
		log.Printf("voice: new opus encoder: %v", err)
		return
	}
	defer enc.close()
	enc.setBitrate(32000)
	enc.setPacketLossPerc(5)

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
			log.Printf("voice: reader read: %v", err)
			return
		}
		pcm, err := chunkToInt16(chunk)
		release()
		if err != nil {
			log.Printf("voice: chunk convert: %v", err)
			continue
		}
		pcmAccum = append(pcmAccum, pcm...)

		for len(pcmAccum) >= opusFrameSamples*numChannels {
			frame := pcmAccum[:opusFrameSamples*numChannels]
			pcmAccum = pcmAccum[opusFrameSamples*numChannels:]

			n, err := enc.encode(frame, opusBuf)
			if err != nil {
				log.Printf("voice: opus encode: %v", err)
				continue
			}

			ciphertext, err := encryptFrame(aead, opusBuf[:n])
			if err != nil {
				log.Printf("voice: encrypt: %v", err)
				continue
			}

			pkt := &rtppkg.Packet{
				Header: rtppkg.Header{
					Version:        2,
					PayloadType:    payloadType,
					SequenceNumber: uint16(atomic.AddUint32(&seq, 1)),
					Timestamp:      ts,
					SSRC:           0xDEADBEEF,
					Marker:         true,
				},
				Payload: ciphertext,
			}
			if err := track.WriteRTP(pkt); err != nil {
				return
			}
			ts += uint32(opusFrameSamples)
		}
	}
}

// chunkToInt16 converts a mediadevices audio chunk to mono int16 PCM.
// The returned slice is always a fresh copy — safe to use after release() is called.
func chunkToInt16(chunk any) ([]int16, error) {
	switch pcm := chunk.(type) {
	case *wave.Int16Interleaved:
		out := make([]int16, len(pcm.Data))
		copy(out, pcm.Data)
		return out, nil
	case *wave.Float32Interleaved:
		out := make([]int16, len(pcm.Data))
		for i, v := range pcm.Data {
			out[i] = int16(v * 32767)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported audio chunk type %T", chunk)
	}
}

func int16ToBytes(pcm []int16) []byte {
	b := make([]byte, len(pcm)*2)
	for i, v := range pcm {
		b[i*2] = byte(v)
		b[i*2+1] = byte(v >> 8)
	}
	return b
}
