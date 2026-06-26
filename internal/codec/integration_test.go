package codec_test

// Full backup→restore simulation that mirrors what the CLI does.
// If this fails, the bug is in the in-process pipeline, not in
// Windows I/O, PowerShell encoding, or store file handling.

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/cerclbackup/cerclbackup/internal/codec"
	"github.com/cerclbackup/cerclbackup/internal/compress"
	bbcrypto "github.com/cerclbackup/cerclbackup/internal/crypto"
	"github.com/cerclbackup/cerclbackup/internal/storage"
	"github.com/cerclbackup/cerclbackup/pkg/protocol"
)

func TestFullBackupRestorePipeline(t *testing.T) {
	scheme := protocol.RSScheme{DataShards: 3, ParityShards: 2}
	enc, err := codec.NewEncoder(scheme)
	if err != nil {
		t.Fatal(err)
	}

	storeDir := t.TempDir()
	store, err := storage.New(storeDir)
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
		{"50 KB random", func() []byte {
			b := make([]byte, 51200)
			rand.Read(b) //nolint
			return b
		}()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			original := tc.data

			// ── BACKUP ──────────────────────────────────────────────────────
			chunkHash := sha256.Sum256(original)
			fileHasher := sha256.New()
			fileHasher.Write(chunkHash[:])
			var fileHash [32]byte
			copy(fileHash[:], fileHasher.Sum(nil))

			fileKey, err := bbcrypto.DeriveFileKey(masterKey, fileHash)
			if err != nil {
				t.Fatalf("DeriveFileKey: %v", err)
			}

			compressed, err := compress.Compress(original)
			if err != nil {
				t.Fatalf("Compress: %v", err)
			}

			shards, err := enc.SplitChunkToShards(compressed)
			if err != nil {
				t.Fatalf("Split: %v", err)
			}

			for i, shard := range shards {
				ct, err := bbcrypto.EncryptShard(fileKey, i, shard)
				if err != nil {
					t.Fatalf("EncryptShard %d: %v", i, err)
				}
				if err := store.Put(tc.name, i, i >= scheme.DataShards, ct); err != nil {
					t.Fatalf("store.Put %d: %v", i, err)
				}
			}

			// ── RESTORE ─────────────────────────────────────────────────────
			decShards := make([][]byte, len(shards))
			for i := range shards {
				ct, err := store.Get(tc.name, i)
				if err != nil {
					t.Fatalf("store.Get %d: %v", i, err)
				}
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

			if !bytes.Equal(restored, original) {
				t.Fatalf("mismatch: got %d bytes, want %d bytes\nfirst 32 got:  %x\nfirst 32 want: %x",
					len(restored), len(original), safeSlice(restored, 32), safeSlice(original, 32))
			}

			// Verify hash matches what the CLI would compute.
			restoreChunkHash := sha256.Sum256(restored)
			restoreFileHasher := sha256.New()
			restoreFileHasher.Write(restoreChunkHash[:])
			var restoreFileHash [32]byte
			copy(restoreFileHash[:], restoreFileHasher.Sum(nil))

			if restoreFileHash != fileHash {
				t.Fatalf("merkle hash mismatch\n  backup:  %x\n  restore: %x", fileHash, restoreFileHash)
			}
		})
	}
}

// TestParityRecovery verifies that Reed-Solomon can reconstruct the original
// data when up to ParityShards shards are lost (set to nil).  Every
// combination of lost shards up to the parity limit is exercised.
func TestParityRecovery(t *testing.T) {
	scheme := protocol.RSScheme{DataShards: 3, ParityShards: 2}
	enc, err := codec.NewEncoder(scheme)
	if err != nil {
		t.Fatal(err)
	}

	original := make([]byte, 4096)
	rand.Read(original)

	masterKey := make([]byte, 32)
	rand.Read(masterKey)

	chunkHash := sha256.Sum256(original)
	fileHasher := sha256.New()
	fileHasher.Write(chunkHash[:])
	var fileHash [32]byte
	copy(fileHash[:], fileHasher.Sum(nil))

	fileKey, err := bbcrypto.DeriveFileKey(masterKey, fileHash)
	if err != nil {
		t.Fatal(err)
	}

	compressed, err := compress.Compress(original)
	if err != nil {
		t.Fatal(err)
	}

	shards, err := enc.SplitChunkToShards(compressed)
	if err != nil {
		t.Fatal(err)
	}

	totalShards := scheme.DataShards + scheme.ParityShards

	// Encrypt all shards once; store ciphertexts for reuse across subtests.
	ciphertexts := make([][]byte, totalShards)
	for i, shard := range shards {
		ct, err := bbcrypto.EncryptShard(fileKey, i, shard)
		if err != nil {
			t.Fatalf("EncryptShard %d: %v", i, err)
		}
		ciphertexts[i] = ct
	}

	// Generate all combinations of up to ParityShards lost indices.
	type combo struct {
		lost []int
	}
	var combos []combo
	combos = append(combos, combo{nil}) // no loss
	for i := 0; i < totalShards; i++ {
		combos = append(combos, combo{[]int{i}})
		for j := i + 1; j < totalShards; j++ {
			combos = append(combos, combo{[]int{i, j}})
		}
	}

	for _, c := range combos {
		lostSet := make(map[int]bool, len(c.lost))
		for _, idx := range c.lost {
			lostSet[idx] = true
		}
		name := fmt.Sprintf("lost=%v", c.lost)

		t.Run(name, func(t *testing.T) {
			decShards := make([][]byte, totalShards)
			for i, ct := range ciphertexts {
				if lostSet[i] {
					decShards[i] = nil // simulate missing shard
					continue
				}
				pt, err := bbcrypto.DecryptShard(fileKey, i, ct)
				if err != nil {
					t.Fatalf("DecryptShard %d: %v", i, err)
				}
				decShards[i] = pt
			}

			merged, err := enc.MergeShardToChunk(decShards)
			if err != nil {
				t.Fatalf("MergeShardToChunk: %v", err)
			}

			restored, err := compress.Decompress(merged)
			if err != nil {
				t.Fatalf("Decompress: %v", err)
			}

			if !bytes.Equal(restored, original) {
				t.Fatalf("data mismatch after recovering lost shards %v", c.lost)
			}
		})
	}
}
