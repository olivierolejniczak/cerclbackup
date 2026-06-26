package codec_test

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"testing"

	"github.com/cerclbackup/cerclbackup/internal/codec"
	"github.com/cerclbackup/cerclbackup/internal/compress"
	bbcrypto "github.com/cerclbackup/cerclbackup/internal/crypto"
	"github.com/cerclbackup/cerclbackup/pkg/protocol"
)

func TestCompressRSRoundtrip(t *testing.T) {
	scheme := protocol.RSScheme{DataShards: 3, ParityShards: 2}
	enc, err := codec.NewEncoder(scheme)
	if err != nil {
		t.Fatal(err)
	}

	masterKey := make([]byte, 32)
	rand.Read(masterKey)

	cases := []struct {
		name string
		data []byte
	}{
		{"small text", []byte("Hello from CerclBackup smoke test\nLine two\nLine three")},
		{"31 bytes", []byte("Gamma file in subdirectory")},
		{"50 KB random", func() []byte { b := make([]byte, 51200); rand.Read(b); return b }()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Derive file key the same way backup does.
			fileHash := sha256.Sum256(tc.data)
			fileKey, err := bbcrypto.DeriveFileKey(masterKey, fileHash)
			if err != nil {
				t.Fatalf("DeriveFileKey: %v", err)
			}

			compressed, err := compress.Compress(tc.data)
			if err != nil {
				t.Fatalf("Compress: %v", err)
			}

			shards, err := enc.SplitChunkToShards(compressed)
			if err != nil {
				t.Fatalf("Split: %v", err)
			}

			// Encrypt each shard.
			ciphertexts := make([][]byte, len(shards))
			for i, shard := range shards {
				ct, err := bbcrypto.EncryptShard(fileKey, i, shard)
				if err != nil {
					t.Fatalf("EncryptShard %d: %v", i, err)
				}
				ciphertexts[i] = ct
			}

			// Decrypt each shard.
			decShards := make([][]byte, len(shards))
			for i, ct := range ciphertexts {
				pt, err := bbcrypto.DecryptShard(fileKey, i, ct)
				if err != nil {
					t.Fatalf("DecryptShard %d: %v", i, err)
				}
				decShards[i] = pt
			}

			merged, err := enc.MergeShardToChunk(decShards)
			if err != nil {
				t.Fatalf("Merge: %v", err)
			}

			restored, err := compress.Decompress(merged)
			if err != nil {
				t.Fatalf("Decompress: %v", err)
			}

			if !bytes.Equal(restored, tc.data) {
				t.Fatalf("roundtrip mismatch: got %d bytes, want %d bytes\nfirst 32 got:  %x\nfirst 32 want: %x",
					len(restored), len(tc.data),
					safeSlice(restored, 32), safeSlice(tc.data, 32))
			}
		})
	}
}

func safeSlice(b []byte, n int) []byte {
	if len(b) < n {
		return b
	}
	return b[:n]
}
