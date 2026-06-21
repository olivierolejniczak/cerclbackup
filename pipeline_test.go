// Package pipeline_test validates the full Phase 1 backup/restore pipeline:
// File → Chunker → Reed-Solomon → AES-256-GCM → Store → Restore → File
package pipeline_test

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/cerclbackup/cerclbackup/internal/chunker"
	"github.com/cerclbackup/cerclbackup/internal/codec"
	bbcrypto "github.com/cerclbackup/cerclbackup/internal/crypto"
	"github.com/cerclbackup/cerclbackup/internal/storage"
	"github.com/cerclbackup/cerclbackup/pkg/protocol"
)

// ─── Helpers ─────────────────────────────────────────────────────────────────

func randomBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	_, err := io.ReadFull(rand.Reader, b)
	if err != nil {
		t.Fatalf("randomBytes: %v", err)
	}
	return b
}

func hashBytes(data []byte) [32]byte {
	return sha256.Sum256(data)
}

// ─── Unit Tests ───────────────────────────────────────────────────────────────

func TestChunker_SmallFile(t *testing.T) {
	data := randomBytes(t, 100)
	chunks, err := chunker.ChunkReader(bytes.NewReader(data), 64)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if chunks[0].Size != 64 {
		t.Errorf("chunk 0 size: got %d, want 64", chunks[0].Size)
	}
	if chunks[1].Size != 36 {
		t.Errorf("chunk 1 size: got %d, want 36", chunks[1].Size)
	}
}

func TestChunker_Reassemble(t *testing.T) {
	original := randomBytes(t, 10_000)
	chunks, err := chunker.ChunkReader(bytes.NewReader(original), 1024)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := chunker.Reassemble(&buf, chunks); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf.Bytes(), original) {
		t.Error("reassembled data does not match original")
	}
}

func TestReedSolomon_EncodeReconstructMissingShards(t *testing.T) {
	scheme := protocol.RSScheme{DataShards: 3, ParityShards: 2}
	enc, err := codec.NewEncoder(scheme)
	if err != nil {
		t.Fatal(err)
	}

	// Chunk must be divisible by DataShards.
	chunkData := randomBytes(t, 3*1024) // 3 KB, divisible by 3
	shards, err := enc.SplitChunkToShards(chunkData)
	if err != nil {
		t.Fatal(err)
	}
	if len(shards) != scheme.TotalShards() {
		t.Fatalf("expected %d shards, got %d", scheme.TotalShards(), len(shards))
	}

	// Simulate losing 2 shards (maximum tolerable for 3/5 scheme).
	shards[1] = nil
	shards[3] = nil

	recovered, err := enc.MergeShardToChunk(shards)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(recovered, chunkData) {
		t.Error("recovered data does not match original after shard loss")
	}
}

func TestReedSolomon_TooManyMissingShards(t *testing.T) {
	scheme := protocol.RSScheme{DataShards: 3, ParityShards: 2}
	enc, err := codec.NewEncoder(scheme)
	if err != nil {
		t.Fatal(err)
	}
	chunkData := randomBytes(t, 3*1024)
	shards, err := enc.SplitChunkToShards(chunkData)
	if err != nil {
		t.Fatal(err)
	}

	// Lose 3 shards (one more than parity count) — should fail.
	shards[0] = nil
	shards[1] = nil
	shards[2] = nil

	_, err = enc.MergeShardToChunk(shards)
	if err == nil {
		t.Error("expected error when too many shards missing, got nil")
	}
}

func TestCrypto_EncryptDecrypt(t *testing.T) {
	key := randomBytes(t, 32)
	plaintext := randomBytes(t, 512)

	ciphertext, err := bbcrypto.Encrypt(key, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(ciphertext, plaintext) {
		t.Error("ciphertext equals plaintext")
	}

	recovered, err := bbcrypto.Decrypt(key, ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(recovered, plaintext) {
		t.Error("decrypted data does not match original")
	}
}

func TestCrypto_WrongKeyFails(t *testing.T) {
	key := randomBytes(t, 32)
	plaintext := randomBytes(t, 128)
	ciphertext, _ := bbcrypto.Encrypt(key, plaintext)

	wrongKey := randomBytes(t, 32)
	_, err := bbcrypto.Decrypt(wrongKey, ciphertext)
	if err == nil {
		t.Error("expected decryption error with wrong key, got nil")
	}
}

func TestCrypto_Argon2KeyDerivation(t *testing.T) {
	salt, err := bbcrypto.NewSalt()
	if err != nil {
		t.Fatal(err)
	}
	k1 := bbcrypto.DeriveKey("my-password", salt)
	k2 := bbcrypto.DeriveKey("my-password", salt)
	if !bytes.Equal(k1, k2) {
		t.Error("same password+salt should produce same key")
	}
	k3 := bbcrypto.DeriveKey("other-password", salt)
	if bytes.Equal(k1, k3) {
		t.Error("different passwords should produce different keys")
	}
}

func TestKeystore_CreateAndUnlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keystore.enc")
	password := "test-password-123"

	ks1 := bbcrypto.NewKeystore(path)
	if err := ks1.Create(password); err != nil {
		t.Fatal(err)
	}
	key1 := ks1.MasterKey()

	ks2 := bbcrypto.NewKeystore(path)
	if err := ks2.Unlock(password); err != nil {
		t.Fatal(err)
	}
	key2 := ks2.MasterKey()

	if !bytes.Equal(key1, key2) {
		t.Error("master key mismatch after save/load")
	}
}

func TestKeystore_WrongPasswordFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keystore.enc")

	ks := bbcrypto.NewKeystore(path)
	if err := ks.Create("correct-password"); err != nil {
		t.Fatal(err)
	}

	ks2 := bbcrypto.NewKeystore(path)
	if err := ks2.Unlock("wrong-password"); err == nil {
		t.Error("expected error unlocking with wrong password")
	}
}

func TestStorage_PutGet(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.New(dir)
	if err != nil {
		t.Fatal(err)
	}

	data := randomBytes(t, 256)
	if err := store.Put("file-001", 0, false, data); err != nil {
		t.Fatal(err)
	}

	got, err := store.Get("file-001", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Error("retrieved data does not match stored data")
	}
}

func TestBestScheme(t *testing.T) {
	cases := []struct {
		buddies int
		wantD   int
		wantP   int
	}{
		{3, 2, 1},
		{5, 3, 2},
		{8, 5, 3},
		{10, 6, 4},
	}
	for _, tc := range cases {
		s, err := protocol.BestScheme(tc.buddies)
		if err != nil {
			t.Fatalf("BestScheme(%d): unexpected error: %v", tc.buddies, err)
		}
		if s.DataShards != tc.wantD || s.ParityShards != tc.wantP {
			t.Errorf("BestScheme(%d): got %d/%d, want %d/%d",
				tc.buddies, s.DataShards, s.ParityShards, tc.wantD, tc.wantP)
		}
	}
}

func TestBestScheme_RejectsFewerThanThreeBuddies(t *testing.T) {
	// CerclBackup never falls back to a 1/1 mirror scheme: a single buddy
	// must never be able to hold a fully reconstructible copy of a file.
	for _, buddies := range []int{0, 1, 2} {
		_, err := protocol.BestScheme(buddies)
		if err == nil {
			t.Errorf("BestScheme(%d): expected ErrInsufficientBuddies, got nil", buddies)
		}
	}
}

// ─── Integration Test — Full Pipeline ────────────────────────────────────────

func TestFullPipeline_BackupAndRestore(t *testing.T) {
	// Create a test file of ~12 KB (3 chunks of 4 KB each).
	original := randomBytes(t, 12_000)
	originalHash := hashBytes(original)

	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "testfile.bin")
	if err := os.WriteFile(srcPath, original, 0600); err != nil {
		t.Fatal(err)
	}

	// ── Setup ─────────────────────────────────────────────────────────────────
	password := "integration-test-pw"
	ksPath := filepath.Join(tmpDir, "keystore.enc")
	ks := bbcrypto.NewKeystore(ksPath)
	if err := ks.Create(password); err != nil {
		t.Fatal(err)
	}
	masterKey := ks.MasterKey()

	store, err := storage.New(filepath.Join(tmpDir, "store"))
	if err != nil {
		t.Fatal(err)
	}

	scheme, err := protocol.BestScheme(5) // 3/5
	if err != nil {
		t.Fatal(err)
	}
	enc, err := codec.NewEncoder(scheme)
	if err != nil {
		t.Fatal(err)
	}

	// ── BACKUP ────────────────────────────────────────────────────────────────
	chunkSize := 4096 // 4 KB for test
	chunks, err := chunker.ChunkFile(srcPath, chunkSize)
	if err != nil {
		t.Fatal(err)
	}

	// Derive file key.
	fileHashInput := sha256.New()
	for _, c := range chunks {
		fileHashInput.Write(c.Hash[:])
	}
	var fileHashArr [32]byte
	copy(fileHashArr[:], fileHashInput.Sum(nil))
	fileKey, err := bbcrypto.DeriveFileKey(masterKey, fileHashArr)
	if err != nil {
		t.Fatal(err)
	}

	fileID := fmt.Sprintf("%x", fileHashArr[:8])
	shardCounter := 0

	for _, chunk := range chunks {
		shards, err := enc.SplitChunkToShards(chunk.Data)
		if err != nil {
			t.Fatal(err)
		}
		for si, shard := range shards {
			ct, err := bbcrypto.EncryptShard(fileKey, shardCounter, shard)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Put(fileID, shardCounter, si >= scheme.DataShards, ct); err != nil {
				t.Fatal(err)
			}
			shardCounter++
		}
	}

	totalShards := shardCounter
	t.Logf("backed up %d chunks → %d shards (scheme %d/%d)",
		len(chunks), totalShards, scheme.DataShards, scheme.ParityShards)

	// ── SIMULATE BUDDY LOSS: nil out one parity shard ─────────────────────────
	// We'll just skip reading shard index 4 (a parity shard) during restore.
	missingShardIdx := 4

	// ── RESTORE ───────────────────────────────────────────────────────────────
	shardsPerChunk := scheme.TotalShards()
	numChunks := totalShards / shardsPerChunk

	var restored []byte
	for ci := 0; ci < numChunks; ci++ {
		rawShards := make([][]byte, shardsPerChunk)
		for si := 0; si < shardsPerChunk; si++ {
			globalIdx := ci*shardsPerChunk + si
			if globalIdx == missingShardIdx {
				t.Logf("simulating missing shard %d", globalIdx)
				rawShards[si] = nil
				continue
			}
			ct, err := store.Get(fileID, globalIdx)
			if err != nil {
				t.Fatalf("get shard %d: %v", globalIdx, err)
			}
			pt, err := bbcrypto.DecryptShard(fileKey, globalIdx, ct)
			if err != nil {
				t.Fatalf("decrypt shard %d: %v", globalIdx, err)
			}
			rawShards[si] = pt
		}

		chunkData, err := enc.MergeShardToChunk(rawShards)
		if err != nil {
			t.Fatalf("merge chunk %d: %v", ci, err)
		}

		// Trim RS padding: codec pads to next multiple of DataShards;
		// chunks[ci].Size holds the original byte count.
		if originalSize := chunks[ci].Size; originalSize < len(chunkData) {
			chunkData = chunkData[:originalSize]
		}
		restored = append(restored, chunkData...)
	}

	// ── VERIFY ────────────────────────────────────────────────────────────────
	restoredHash := hashBytes(restored)
	if originalHash != restoredHash {
		t.Errorf("hash mismatch: original %x, restored %x", originalHash, restoredHash)
	}
	if !bytes.Equal(original, restored) {
		t.Error("restored bytes do not match original")
	}
	t.Log("✅ full pipeline test passed — backup + restore with 1 missing shard")
}
