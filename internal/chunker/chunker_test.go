package chunker_test

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"os"
	"testing"

	"github.com/cerclbackup/cerclbackup/internal/chunker"
)

func TestChunkFileCorrectness(t *testing.T) {
	cases := []struct {
		name       string
		size       int
		wantChunks int
	}{
		{"empty", 0, 0},
		{"1 byte", 1, 1},
		{"small", 53, 1},
		{"exact chunk boundary", chunker.DefaultChunkSize, 1},
		{"chunk+1", chunker.DefaultChunkSize + 1, 2},
		{"two full chunks", 2 * chunker.DefaultChunkSize, 2},
		{"two chunks+1", 2*chunker.DefaultChunkSize + 1, 3},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			original := make([]byte, tc.size)
			if tc.size > 0 {
				rand.Read(original)
			}

			f, err := os.CreateTemp("", "chunker-test-*")
			if err != nil {
				t.Fatal(err)
			}
			path := f.Name()
			defer os.Remove(path)
			if _, err := f.Write(original); err != nil {
				t.Fatal(err)
			}
			f.Close()

			chunks, err := chunker.ChunkFile(path, chunker.DefaultChunkSize)
			if err != nil {
				t.Fatalf("ChunkFile: %v", err)
			}

			// Correct number of chunks.
			if len(chunks) != tc.wantChunks {
				t.Fatalf("got %d chunks, want %d", len(chunks), tc.wantChunks)
			}

			if len(chunks) == 0 {
				return
			}

			// Each chunk: Data[:Size] == original slice, hash correct, VerifyChunk passes.
			offset := 0
			for i, c := range chunks {
				wantSize := chunker.DefaultChunkSize
				if i == len(chunks)-1 {
					wantSize = tc.size - offset
				}
				if c.Size != wantSize {
					t.Errorf("chunk %d: Size=%d, want %d", i, c.Size, wantSize)
				}
				if len(c.Data) != chunker.DefaultChunkSize {
					t.Errorf("chunk %d: len(Data)=%d, want %d", i, len(c.Data), chunker.DefaultChunkSize)
				}
				// Data[:Size] must match original bytes.
				if !bytes.Equal(c.Data[:c.Size], original[offset:offset+c.Size]) {
					t.Errorf("chunk %d: Data[:Size] != original bytes", i)
				}
				// Trailing bytes must be zero-padded.
				for j := c.Size; j < len(c.Data); j++ {
					if c.Data[j] != 0 {
						t.Errorf("chunk %d: byte %d after Size is %d, want 0", i, j, c.Data[j])
						break
					}
				}
				// Hash must be sha256(Data[:Size]).
				wantHash := sha256.Sum256(c.Data[:c.Size])
				if c.Hash != wantHash {
					t.Errorf("chunk %d: Hash mismatch\n  got:  %x\n  want: %x", i, c.Hash, wantHash)
				}
				if !chunker.VerifyChunk(c) {
					t.Errorf("chunk %d: VerifyChunk returned false", i)
				}
				offset += c.Size
			}

			// Reassemble must reproduce original bytes exactly.
			var buf bytes.Buffer
			if err := chunker.Reassemble(&buf, chunks); err != nil {
				t.Fatalf("Reassemble: %v", err)
			}
			if !bytes.Equal(buf.Bytes(), original) {
				t.Fatalf("Reassemble mismatch: got %d bytes, want %d bytes", buf.Len(), len(original))
			}
		})
	}
}
