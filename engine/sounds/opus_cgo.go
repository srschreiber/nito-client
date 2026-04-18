// Copyright (c) 2026 Sam Schreiber
// SPDX-License-Identifier: LicenseRef-nito

package sounds

// Minimal CGo wrapper around libopus for encoding and decoding Opus frames.
// Only libopus is required (pkg-config: opus); libopusfile is not needed.

/*
#cgo pkg-config: opus
#include <opus.h>
#include <stdlib.h>

static int encoder_set_bitrate(OpusEncoder *enc, int bitrate) {
    return opus_encoder_ctl(enc, OPUS_SET_BITRATE(bitrate));
}

static int encoder_set_packet_loss_perc(OpusEncoder *enc, int perc) {
    return opus_encoder_ctl(enc, OPUS_SET_PACKET_LOSS_PERC(perc));
}

static int encoder_set_dtx(OpusEncoder *enc, int dtx) {
    return opus_encoder_ctl(enc, OPUS_SET_DTX(dtx));
}
*/
import "C"
import (
	"fmt"
	"unsafe"
)

type opusEncoder struct {
	enc *C.OpusEncoder
}

func newOpusEncoder(sampleRate, channels int) (*opusEncoder, error) {
	var errCode C.int
	enc := C.opus_encoder_create(C.opus_int32(sampleRate), C.int(channels), C.OPUS_APPLICATION_VOIP, &errCode)
	if errCode != C.OPUS_OK {
		return nil, fmt.Errorf("opus_encoder_create: error %d", int(errCode))
	}
	return &opusEncoder{enc: enc}, nil
}

func (e *opusEncoder) setBitrate(bitrate int) {
	C.encoder_set_bitrate(e.enc, C.int(bitrate))
}

func (e *opusEncoder) setPacketLossPerc(loss int) {
	C.encoder_set_packet_loss_perc(e.enc, C.int(loss))
}

func (e *opusEncoder) setDTX(enabled bool) {
	v := 0
	if enabled {
		v = 1
	}
	C.encoder_set_dtx(e.enc, C.int(v))
}

// encode encodes a frame of mono int16 PCM samples into out.
// len(pcm) must equal the frame size (e.g. 960 for 20 ms at 48 kHz).
// Returns the number of bytes written to out.
func (e *opusEncoder) encode(pcm []int16, out []byte) (int, error) {
	if len(pcm) == 0 || len(out) == 0 {
		return 0, fmt.Errorf("encode: empty input or output buffer")
	}
	n := C.opus_encode(
		e.enc,
		(*C.opus_int16)(unsafe.Pointer(&pcm[0])),
		C.int(len(pcm)), // samples per channel
		(*C.uchar)(unsafe.Pointer(&out[0])),
		C.opus_int32(len(out)),
	)
	if n < 0 {
		return 0, fmt.Errorf("opus_encode error: %d", int(n))
	}
	return int(n), nil
}

func (e *opusEncoder) close() {
	if e.enc != nil {
		C.opus_encoder_destroy(e.enc)
		e.enc = nil
	}
}

type opusDecoder struct {
	dec *C.OpusDecoder
}

func newOpusDecoder(sampleRate, channels int) (*opusDecoder, error) {
	var errCode C.int
	dec := C.opus_decoder_create(C.opus_int32(sampleRate), C.int(channels), &errCode)
	if errCode != C.OPUS_OK {
		return nil, fmt.Errorf("opus_decoder_create: error %d", int(errCode))
	}
	return &opusDecoder{dec: dec}, nil
}

// decode decodes an Opus packet into pcm (int16 samples, mono).
// Returns the number of samples decoded per channel.
func (d *opusDecoder) decode(data []byte, pcm []int16) (int, error) {
	if len(data) == 0 || len(pcm) == 0 {
		return 0, fmt.Errorf("decode: empty input or output buffer")
	}
	n := C.opus_decode(
		d.dec,
		(*C.uchar)(unsafe.Pointer(&data[0])),
		C.opus_int32(len(data)),
		(*C.opus_int16)(unsafe.Pointer(&pcm[0])),
		C.int(len(pcm)), // max samples per channel
		0,               // no FEC
	)
	if n < 0 {
		return 0, fmt.Errorf("opus_decode error: %d", int(n))
	}
	return int(n), nil
}

// decodePLC runs packet loss concealment for one missing frame.
// Returns the number of samples written per channel.
func (d *opusDecoder) decodePLC(pcm []int16) (int, error) {
	if len(pcm) == 0 {
		return 0, fmt.Errorf("decodePLC: empty output buffer")
	}
	n := C.opus_decode(
		d.dec,
		nil,
		0,
		(*C.opus_int16)(unsafe.Pointer(&pcm[0])),
		C.int(len(pcm)),
		0,
	)
	if n < 0 {
		return 0, fmt.Errorf("opus_decode PLC error: %d", int(n))
	}
	return int(n), nil
}

func (d *opusDecoder) close() {
	if d.dec != nil {
		C.opus_decoder_destroy(d.dec)
		d.dec = nil
	}
}
