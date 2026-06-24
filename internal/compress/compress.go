// Package compress provides zstd compression for backup chunks.
// Compression is applied after chunking and before Reed-Solomon encoding
// so that each shard carries compressed bytes; this maximises storage
// efficiency while keeping the encryption layer unchanged.
package compress

import (
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

// Compress returns a zstd-compressed copy of src.
// The encoder is a long-lived singleton; this function is safe for concurrent use.
func Compress(src []byte) ([]byte, error) {
	enc, err := encoder()
	if err != nil {
		return nil, fmt.Errorf("compress: init encoder: %w", err)
	}
	return enc.EncodeAll(src, make([]byte, 0, len(src)/2)), nil
}

// Decompress decodes a zstd-compressed buffer.
func Decompress(src []byte) ([]byte, error) {
	dec, err := decoder()
	if err != nil {
		return nil, fmt.Errorf("compress: init decoder: %w", err)
	}
	out, err := dec.DecodeAll(src, make([]byte, 0, len(src)*2))
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
