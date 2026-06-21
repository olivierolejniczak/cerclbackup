// Package chunker splits files into fixed-size chunks and reassembles them.
// Each chunk carries a SHA-256 hash for integrity verification.
package chunker

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"

	"github.com/cerclbackup/cerclbackup/pkg/protocol"
)

const DefaultChunkSize = 4 * 1024 * 1024 // 4 MB

// ChunkFile splits the file at path into fixed-size chunks.
// The last chunk is zero-padded to chunkSize so that Reed-Solomon
// receives uniform-length shards.
func ChunkFile(path string, chunkSize int) ([]protocol.Chunk, error) {
	if chunkSize <= 0 {
		chunkSize = DefaultChunkSize
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("chunker: open %q: %w", path, err)
	}
	defer f.Close()

	return ChunkReader(f, chunkSize)
}

// ChunkReader splits data from r into fixed-size chunks.
func ChunkReader(r io.Reader, chunkSize int) ([]protocol.Chunk, error) {
	var chunks []protocol.Chunk
	buf := make([]byte, chunkSize)
	index := 0

	for {
		n, err := io.ReadFull(r, buf)
		if n > 0 {
			// Copy to avoid aliasing across loop iterations.
			data := make([]byte, chunkSize)
			copy(data, buf[:n])
			// Zero-pad the remainder (already zero from make, but explicit).
			for i := n; i < chunkSize; i++ {
				data[i] = 0
			}
			chunks = append(chunks, protocol.Chunk{
				Index: index,
				Data:  data,
				Hash:  sha256.Sum256(data[:n]), // hash over actual bytes only
				Size:  n,
			})
			index++
		}
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("chunker: read at index %d: %w", index, err)
		}
	}

	return chunks, nil
}

// Reassemble writes chunks back to w in index order.
// It trims the zero-padding from the last chunk using the recorded Size.
func Reassemble(w io.Writer, chunks []protocol.Chunk) error {
	if len(chunks) == 0 {
		return nil
	}
	for i, c := range chunks {
		data := c.Data
		// Trim padding on the last chunk.
		if i == len(chunks)-1 && c.Size > 0 && c.Size < len(data) {
			data = data[:c.Size]
		}
		if _, err := w.Write(data); err != nil {
			return fmt.Errorf("chunker: write chunk %d: %w", i, err)
		}
	}
	return nil
}

// VerifyChunk recomputes the SHA-256 of chunk.Data[:chunk.Size]
// and compares it against chunk.Hash.
func VerifyChunk(c protocol.Chunk) bool {
	size := c.Size
	if size == 0 || size > len(c.Data) {
		size = len(c.Data)
	}
	h := sha256.Sum256(c.Data[:size])
	return h == c.Hash
}
