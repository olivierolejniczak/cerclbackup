// Package codec wraps klauspost/reedsolomon to encode and decode
// CerclBackup shards.  Each []byte shard has the same length (chunkSize).
package codec

import (
	"fmt"

	"github.com/cerclbackup/cerclbackup/pkg/protocol"
	"github.com/klauspost/reedsolomon"
)

// Encoder holds a configured Reed-Solomon encoder for a given RSScheme.
type Encoder struct {
	rs     reedsolomon.Encoder
	scheme protocol.RSScheme
}

// NewEncoder creates an Encoder for the given RSScheme.
func NewEncoder(scheme protocol.RSScheme) (*Encoder, error) {
	rs, err := reedsolomon.New(scheme.DataShards, scheme.ParityShards)
	if err != nil {
		return nil, fmt.Errorf("codec: create RS encoder (%d/%d): %w",
			scheme.DataShards, scheme.ParityShards, err)
	}
	return &Encoder{rs: rs, scheme: scheme}, nil
}

// Scheme returns the RSScheme this Encoder was built with.
func (e *Encoder) Scheme() protocol.RSScheme { return e.scheme }

// Encode takes dataShards [][]byte of equal length and appends parityShards.
// Returns the full slice (data + parity), each shard being shardSize bytes.
//
// The caller must supply exactly scheme.DataShards slices in data;
// each must have the same length.
func (e *Encoder) Encode(data [][]byte) ([][]byte, error) {
	if len(data) != e.scheme.DataShards {
		return nil, fmt.Errorf("codec: Encode expects %d data shards, got %d",
			e.scheme.DataShards, len(data))
	}

	// Allocate parity shards of the same size as data shards.
	shardSize := len(data[0])
	shards := make([][]byte, e.scheme.TotalShards())
	for i := 0; i < e.scheme.DataShards; i++ {
		shards[i] = data[i]
	}
	for i := e.scheme.DataShards; i < e.scheme.TotalShards(); i++ {
		shards[i] = make([]byte, shardSize)
	}

	if err := e.rs.Encode(shards); err != nil {
		return nil, fmt.Errorf("codec: RS encode: %w", err)
	}
	return shards, nil
}

// Verify returns true if all shards are consistent (no corruption detected).
// Any nil shard is treated as missing.
func (e *Encoder) Verify(shards [][]byte) (bool, error) {
	ok, err := e.rs.Verify(shards)
	if err != nil {
		return false, fmt.Errorf("codec: RS verify: %w", err)
	}
	return ok, nil
}

// Reconstruct repairs missing (nil) shards in-place.
// At least scheme.DataShards non-nil shards must be present.
// After a successful call, all shards are non-nil and consistent.
func (e *Encoder) Reconstruct(shards [][]byte) error {
	if err := e.rs.Reconstruct(shards); err != nil {
		return fmt.Errorf("codec: RS reconstruct: %w", err)
	}
	return nil
}

// SplitChunkToShards takes one protocol.Chunk (already padded to uniform size)
// and produces scheme.TotalShards() sub-shards by splitting the chunk data
// across DataShards and computing ParityShards.
//
// This is the main entry point used by the backup pipeline:
//
//	chunk (4 MB) → split into DataShards pieces → RS encode → TotalShards pieces
func (e *Encoder) SplitChunkToShards(chunk []byte) ([][]byte, error) {
	// Pad to next multiple of DataShards so RS can split evenly.
	// The caller knows the original size (chunk.Size) and trims on restore.
	if rem := len(chunk) % e.scheme.DataShards; rem != 0 {
		chunk = append(chunk, make([]byte, e.scheme.DataShards-rem)...)
	}
	shardSize := len(chunk) / e.scheme.DataShards

	data := make([][]byte, e.scheme.DataShards)
	for i := range data {
		data[i] = chunk[i*shardSize : (i+1)*shardSize]
	}
	return e.Encode(data)
}

// MergeShardToChunk is the inverse of SplitChunkToShards.
// It reconstructs missing shards then concatenates the DataShards
// to recover the original chunk bytes.
func (e *Encoder) MergeShardToChunk(shards [][]byte) ([]byte, error) {
	if err := e.Reconstruct(shards); err != nil {
		return nil, err
	}

	var out []byte
	for i := 0; i < e.scheme.DataShards; i++ {
		out = append(out, shards[i]...)
	}
	return out, nil
}
