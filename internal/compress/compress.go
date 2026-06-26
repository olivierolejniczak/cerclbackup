// Package compress provides zstd compression for backup chunks.
// Compression is applied after chunking and before Reed-Solomon encoding
// so that each shard carries compressed bytes; this maximises storage
// efficiency while keeping the encryption layer unchanged.
//
// Wire format: Compress prepends a 4-byte little-endian frame length before
// the zstd frame.  Reed-Solomon encoding pads the payload with trailing zeros;
// the header lets Decompress slice out exactly the zstd bytes so the decoder
// does not see the padding.
package compress

import (
	"encoding/binary"
	"fmt"
	"sync"

	"github.com/klauspost/compress/zstd"
)

// Level maps to zstd encoder speed presets.
type Level int

const (
	LevelFastest Level = iota
	LevelDefault
	LevelBetter
	LevelBest
)

var (
	encoderOnce sync.Once
	encoderInst *zstd.Encoder
	encoderErr  error

	decoderOnce sync.Once
	decoderInst *zstd.Decoder
	decoderErr  error
)

func encoder() (*zstd.Encoder, error) {
	encoderOnce.Do(func() {
		encoderInst, encoderErr = zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	})
	return encoderInst, encoderErr
}

func decoder() (*zstd.Decoder, error) {
	decoderOnce.Do(func() {
		decoderInst, decoderErr = zstd.NewReader(nil)
	})
	return decoderInst, decoderErr
}

// Compress returns a zstd-compressed copy of src prefixed with a 4-byte
// little-endian frame length.  The header lets Decompress trim RS zero-padding
// before handing the frame to the zstd decoder.
func Compress(src []byte) ([]byte, error) {
	enc, err := encoder()
	if err != nil {
		return nil, fmt.Errorf("compress: init encoder: %w", err)
	}
	frame := enc.EncodeAll(src, make([]byte, 0, len(src)/2))
	out := make([]byte, 4+len(frame))
	binary.LittleEndian.PutUint32(out[:4], uint32(len(frame)))
	copy(out[4:], frame)
	return out, nil
}

// Decompress decodes a buffer produced by Compress.
// It reads the 4-byte length header first so that trailing RS zero-padding
// is ignored by the zstd decoder.
func Decompress(src []byte) ([]byte, error) {
	if len(src) < 4 {
		return nil, fmt.Errorf("compress: decode: buffer too short (%d bytes)", len(src))
	}
	frameLen := int(binary.LittleEndian.Uint32(src[:4]))
	if 4+frameLen > len(src) {
		return nil, fmt.Errorf("compress: decode: header claims %d bytes but buffer is %d", frameLen, len(src)-4)
	}
	dec, err := decoder()
	if err != nil {
		return nil, fmt.Errorf("compress: init decoder: %w", err)
	}
	out, err := dec.DecodeAll(src[4:4+frameLen], make([]byte, 0, frameLen*2))
	if err != nil {
		return nil, fmt.Errorf("compress: decode: %w", err)
	}
	return out, nil
}

// MaybeDecompress decompresses src only when compressed is true.
// This allows callers to handle manifests that were backed up before
// compression was introduced (Version compat flag = false).
func MaybeDecompress(src []byte, compressed bool) ([]byte, error) {
	if !compressed {
		return src, nil
	}
	return Decompress(src)
}
