// CerclBackup — Phase 1 CLI entry point.
//
// Usage:
//
//	cerclbackup backup  --src <path> --store <dir> --password <pwd>
//	cerclbackup restore --file-id <uuid> --store <dir> --out <path> --password <pwd>
//	cerclbackup list    --store <dir> --password <pwd>
//
// Phase 1 runs entirely locally: no network, no buddies.
// The pipeline is:
//
//	File → Chunker → Reed-Solomon → AES-256-GCM → Local Store
//
// Restore reverses the pipeline using the encrypted manifest.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/cerclbackup/cerclbackup/internal/chunker"
	"github.com/cerclbackup/cerclbackup/internal/codec"
	bbcrypto "github.com/cerclbackup/cerclbackup/internal/crypto"
	"github.com/cerclbackup/cerclbackup/internal/manifest"
	"github.com/cerclbackup/cerclbackup/internal/storage"
	"github.com/cerclbackup/cerclbackup/pkg/protocol"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "backup":
		runBackup(os.Args[2:])
	case "restore":
		runRestore(os.Args[2:])
	case "list":
		runList(os.Args[2:])
	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `CerclBackup Phase 1

Commands:
  backup   --src <path> --store <dir> --password <pwd> [--buddies N]
  restore  --file-id <uuid> --store <dir> --out <path> --password <pwd>
  list     --store <dir> --password <pwd>`)
}

// ─── BACKUP ──────────────────────────────────────────────────────────────────

func runBackup(args []string) {
	fs := flag.NewFlagSet("backup", flag.ExitOnError)
	src := fs.String("src", "", "Source file to back up")
	storeDir := fs.String("store", storage.DefaultStorePath(), "Store directory")
	password := fs.String("password", "", "Encryption password")
	buddies := fs.Int("buddies", 5, "Number of simulated buddies (determines RS scheme)")
	_ = fs.Parse(args)

	if *src == "" || *password == "" {
		fs.Usage()
		os.Exit(1)
	}

	// ── 1. Setup ──────────────────────────────────────────────────────────────
	store := mustStore(*storeDir)
	ks := openOrCreateKeystore(*password)
	masterKey := ks.MasterKey()
	mf := openManifest(masterKey)

	// ── 2. Determine RS scheme ────────────────────────────────────────────────
	scheme, err := protocol.BestScheme(*buddies)
	if err != nil {
		log.Fatalf("[backup] %v (got --buddies=%d)", err, *buddies)
	}
	log.Printf("[backup] RS scheme: %d data / %d parity (tolerates %d buddy failures)",
		scheme.DataShards, scheme.ParityShards, scheme.ParityShards)

	// ── 3. Chunk the file ─────────────────────────────────────────────────────
	log.Printf("[backup] chunking %q ...", *src)
	chunks, err := chunker.ChunkFile(*src, chunker.DefaultChunkSize)
	must(err)
	log.Printf("[backup] %d chunk(s) of %d MB", len(chunks), chunker.DefaultChunkSize/1024/1024)

	// ── 4. Derive file key ────────────────────────────────────────────────────
	// Hash is over all chunk hashes concatenated to represent the file content.
	fileHash := fileHashFromChunks(chunks)
	fileKey, err := bbcrypto.DeriveFileKey(masterKey, fileHash)
	must(err)

	// ── 5. Reed-Solomon encode + AES-256-GCM encrypt each chunk ──────────────
	enc, err := codec.NewEncoder(scheme)
	must(err)

	var shardLocations []protocol.ShardLocation
	shardCounter := 0

	for _, chunk := range chunks {
		// RS encode: split this chunk into scheme.TotalShards() sub-shards.
		rawShards, err := enc.SplitChunkToShards(chunk.Data)
		must(err)

		for si, shard := range rawShards {
			isParity := si >= scheme.DataShards
			globalShardIdx := shardCounter
			shardCounter++

			// AES-GCM encrypt.
			ciphertext, err := bbcrypto.EncryptShard(fileKey, globalShardIdx, shard)
			must(err)

			// Persist to local store (Phase 1 — all shards go locally).
			// In Phase 2 each shard will be sent to a different buddy.
			fileID := fileIDFromHash(fileHash)
			storageKey := fmt.Sprintf("chunk%d-shard%d", chunk.Index, si)
			must(store.Put(fileID, globalShardIdx, isParity, ciphertext))

			shardLocations = append(shardLocations, protocol.ShardLocation{
				ShardIndex: globalShardIdx,
				IsParity:   isParity,
				BuddyID:    "local", // Phase 1 placeholder
				StorageKey: storageKey,
			})
		}
	}

	// ── 6. Update manifest ────────────────────────────────────────────────────
	info, err := os.Stat(*src)
	must(err)
	entry, err := mf.Upsert(*src, fileHash, info.Size(), scheme, shardLocations)
	must(err)
	must(mf.Save())

	log.Printf("[backup] ✅ done — file-id: %s  shards: %d  scheme: %d/%d",
		entry.FileID, len(shardLocations), scheme.DataShards, scheme.ParityShards)
}

// ─── RESTORE ─────────────────────────────────────────────────────────────────

func runRestore(args []string) {
	fs := flag.NewFlagSet("restore", flag.ExitOnError)
	fileID := fs.String("file-id", "", "FileID from the manifest")
	storeDir := fs.String("store", storage.DefaultStorePath(), "Store directory")
	out := fs.String("out", "", "Output file path")
	password := fs.String("password", "", "Encryption password")
	_ = fs.Parse(args)

	if *fileID == "" || *out == "" || *password == "" {
		fs.Usage()
		os.Exit(1)
	}

	store := mustStore(*storeDir)
	ks := openOrCreateKeystore(*password)
	masterKey := ks.MasterKey()
	mf := openManifest(masterKey)

	entry := mf.Get(*fileID)
	if entry == nil {
		log.Fatalf("[restore] file-id %q not found in manifest", *fileID)
	}
	log.Printf("[restore] restoring %q (%d bytes, scheme %d/%d) ...",
		entry.Path, entry.Size, entry.Scheme.DataShards, entry.Scheme.ParityShards)

	// Derive file key from the hash stored in the manifest.
	hashBytes, err := hexToHash(entry.Hash)
	must(err)
	fileKey, err := bbcrypto.DeriveFileKey(masterKey, hashBytes)
	must(err)

	// storeFileID matches what backup used: hex prefix of the chunk-hash, NOT the manifest UUID.
	storeFileID := fileIDFromHash(hashBytes)

	enc, err := codec.NewEncoder(entry.Scheme)
	must(err)

	// How many RS shards per original chunk?
	shardsPerChunk := entry.Scheme.TotalShards()
	totalShards := len(entry.Shards)
	numChunks := totalShards / shardsPerChunk
	if totalShards%shardsPerChunk != 0 {
		log.Fatalf("[restore] shard count %d not divisible by %d", totalShards, shardsPerChunk)
	}

	outFile, err := os.Create(*out)
	must(err)
	defer outFile.Close()

	for ci := 0; ci < numChunks; ci++ {
		// Collect and decrypt the shards for this original chunk.
		rawShards := make([][]byte, shardsPerChunk)
		for si := 0; si < shardsPerChunk; si++ {
			globalShardIdx := ci*shardsPerChunk + si
			loc := entry.Shards[globalShardIdx]

			ciphertext, err := store.Get(storeFileID, loc.ShardIndex)
			if err != nil {
				// Shard missing — set nil so RS can reconstruct.
				log.Printf("[restore] shard %d missing, will reconstruct", globalShardIdx)
				rawShards[si] = nil
				continue
			}

			plaintext, err := bbcrypto.DecryptShard(fileKey, loc.ShardIndex, ciphertext)
			if err != nil {
				log.Printf("[restore] shard %d decrypt error: %v — treating as missing", globalShardIdx, err)
				rawShards[si] = nil
				continue
			}
			rawShards[si] = plaintext
		}

		// RS reconstruct (handles nil shards).
		chunkData, err := enc.MergeShardToChunk(rawShards)
		must(err)

		// Trim padding on the last chunk.
		if ci == numChunks-1 {
			lastChunkSize := int(entry.Size) % chunker.DefaultChunkSize
			if lastChunkSize == 0 {
				lastChunkSize = chunker.DefaultChunkSize
			}
			if lastChunkSize < len(chunkData) {
				chunkData = chunkData[:lastChunkSize]
			}
		}

		if _, err := outFile.Write(chunkData); err != nil {
			log.Fatalf("[restore] write chunk %d: %v", ci, err)
		}
	}

	log.Printf("[restore] ✅ restored to %q", *out)
}

// ─── LIST ─────────────────────────────────────────────────────────────────────

func runList(args []string) {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	storeDir := fs.String("store", storage.DefaultStorePath(), "Store directory")
	password := fs.String("password", "", "Encryption password")
	_ = fs.Parse(args)

	if *password == "" {
		fs.Usage()
		os.Exit(1)
	}

	ks := openOrCreateKeystore(*password)
	masterKey := ks.MasterKey()
	_ = storeDir
	mf := openManifest(masterKey)

	entries := mf.All()
	if len(entries) == 0 {
		fmt.Println("No files backed up yet.")
		return
	}
	fmt.Printf("%-36s  %-50s  %10s  %s\n", "FILE-ID", "PATH", "SIZE", "MODIFIED")
	fmt.Println("─────────────────────────────────────────────────────────────────────────────────────────────────────────")
	for _, e := range entries {
		fmt.Printf("%-36s  %-50s  %10d  %s\n",
			e.FileID, e.Path, e.Size, e.Modified.Format("2006-01-02 15:04:05"))
	}
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func mustStore(dir string) *storage.Store {
	s, err := storage.New(dir)
	must(err)
	return s
}

func openOrCreateKeystore(password string) *bbcrypto.Keystore {
	ksPath := bbcrypto.DefaultKeystorePath()
	ks := bbcrypto.NewKeystore(ksPath)
	if _, err := os.Stat(ksPath); os.IsNotExist(err) {
		log.Printf("[keystore] creating new keystore at %s", ksPath)
		must(ks.Create(password))
	} else {
		must(ks.Unlock(password))
	}
	return ks
}

func openManifest(masterKey []byte) *manifest.Manifest {
	mf := manifest.New(manifest.DefaultManifestPath(), masterKey)
	must(mf.Load())
	return mf
}

func fileHashFromChunks(chunks []protocol.Chunk) [32]byte {
	h := sha256.New()
	for _, c := range chunks {
		h.Write(c.Hash[:])
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

func fileIDFromHash(h [32]byte) string {
	return fmt.Sprintf("%x", h[:8]) // 16-char prefix — unique enough for Phase 1
}

func hexToHash(s string) ([32]byte, error) {
	var out [32]byte
	b, err := hex.DecodeString(s)
	if err != nil {
		return out, fmt.Errorf("hexToHash: %w", err)
	}
	if len(b) != 32 {
		return out, fmt.Errorf("hexToHash: expected 32 bytes, got %d", len(b))
	}
	copy(out[:], b)
	return out, nil
}
