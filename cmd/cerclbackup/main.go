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
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/cerclbackup/cerclbackup/internal/buddy"
	"github.com/cerclbackup/cerclbackup/internal/chunker"
	"github.com/cerclbackup/cerclbackup/internal/codec"
	cerclConfig "github.com/cerclbackup/cerclbackup/internal/config"
	bbcrypto "github.com/cerclbackup/cerclbackup/internal/crypto"
	"github.com/cerclbackup/cerclbackup/internal/emailinvite"
	"github.com/cerclbackup/cerclbackup/internal/identity"
	"github.com/cerclbackup/cerclbackup/internal/invite"
	"github.com/cerclbackup/cerclbackup/internal/manifdist"
	"github.com/cerclbackup/cerclbackup/internal/manifest"
	"github.com/cerclbackup/cerclbackup/internal/circle"
	"github.com/cerclbackup/cerclbackup/internal/archive"
	bbcompress "github.com/cerclbackup/cerclbackup/internal/compress"
	bbexclude "github.com/cerclbackup/cerclbackup/internal/exclude"
	"github.com/cerclbackup/cerclbackup/internal/keyring"
	p2pmod "github.com/cerclbackup/cerclbackup/internal/p2p"
	traystatus "github.com/cerclbackup/cerclbackup/internal/tray"
	"github.com/cerclbackup/cerclbackup/internal/version"
	"github.com/cerclbackup/cerclbackup/internal/watcher"
	"github.com/cerclbackup/cerclbackup/internal/rebalance"
	scrubpkg "github.com/cerclbackup/cerclbackup/internal/scrub"
	"github.com/cerclbackup/cerclbackup/internal/storage"
	"github.com/libp2p/go-libp2p/core/host"
	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/cerclbackup/cerclbackup/pkg/protocol"
	"github.com/cerclbackup/cerclbackup/pkg/wire"
	"github.com/multiformats/go-multiaddr"
	ipfslog "github.com/ipfs/go-log/v2"
)

// cfg holds values loaded from the user's config.yaml, applied as flag defaults.
var cfg cerclConfig.Config

// Thin wrappers so the identity package functions are accessible inside main
// without repeating the import path in each function body.
const identitySeedKeyName = identity.KeyName

func identityMnemonicFromSeed(seed []byte) (string, error) {
	return identity.MnemonicFromSeed(seed)
}

func identitySeedFromMnemonic(mnemonic string) ([]byte, error) {
	return identity.SeedFromMnemonic(mnemonic)
}

func main() {
	// Suppress third-party log.Printf noise that cannot be filtered via
	// ipfs/go-log subsystem levels (zeroconf multicast, quic-go buffer).
	log.SetOutput(&serveLogFilter{out: os.Stderr})

	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	cfg = cerclConfig.Load()
	// Password resolution order: env var → OS keyring → config file.
	if p := os.Getenv("CERCLBACKUP_PASSWORD"); p != "" {
		cfg.Password = p
	} else if p, err := keyring.Get(); err == nil && p != "" {
		cfg.Password = p
	}

	switch os.Args[1] {
	case "init":
		os.Exit(runInit(os.Args[2:]))
	case "backup":
		runBackup(os.Args[2:])
	case "restore":
		runRestore(os.Args[2:])
	case "list":
		runList(os.Args[2:])
	case "serve":
		runServe(os.Args[2:])
	case "invite":
		runInvite(os.Args[2:])
	case "invite-email":
		runInviteEmail(os.Args[2:])
	case "join-email":
		runJoinEmail(os.Args[2:])
	case "join":
		runJoin(os.Args[2:])
	case "buddy":
		os.Exit(runBuddy(os.Args[2:]))
	case "revoke":
		runRevoke(os.Args[2:])
	case "rebalance":
		runRebalance(os.Args[2:])
	case "manifest-pull":
		runManifestPull(os.Args[2:])
	case "show-phrase":
		runShowPhrase(os.Args[2:])
	case "recover":
		runRecover(os.Args[2:])
	case "watch":
		runWatch(os.Args[2:])
	case "prune":
		os.Exit(runPrune(os.Args[2:]))
	case "storage":
		os.Exit(runStorage(os.Args[2:]))
	case "scrub":
		os.Exit(runScrub(os.Args[2:]))
	case "audit":
		os.Exit(runAudit(os.Args[2:]))
	case "export":
		os.Exit(runExport(os.Args[2:]))
	case "import":
		os.Exit(runImport(os.Args[2:]))
	case "diff":
		os.Exit(runDiff(os.Args[2:]))
	case "doctor":
		os.Exit(runDoctor(os.Args[2:]))
	case "passwd":
		os.Exit(runPasswd(os.Args[2:]))
	case "config":
		os.Exit(runConfig(os.Args[2:]))
	case "circle":
		os.Exit(runCircle(os.Args[2:]))
	case "versions":
		os.Exit(runVersions(os.Args[2:]))
	case "set-password":
		os.Exit(runSetPassword(os.Args[2:]))
	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "CerclBackup %s\n\n", version.AppVersion)
	fmt.Fprintln(os.Stderr, `Commands (Phase 1 — local):
  backup   --src <path> --store <dir> --password <pwd> [--buddies N]
  restore  --file-id <uuid> --store <dir> --out <path> --password <pwd>
  list     --store <dir> --password <pwd>

Commands (Phase 2a — P2P):
  serve    --password <pwd> [--port N]          start P2P daemon
  invite   --password <pwd>                      generate invite code
  join     --addr <multiaddr> --words "<mnemonic>" --password <pwd>
  buddy    list --password <pwd>                 list known buddies
  revoke    --peer-id <id> --password <pwd>       remove a buddy and rebalance
  rebalance    --password <pwd> [--store <dir>]     redistribute shards to all buddies
  invite-email --to <email> --circle <name> --password <pwd> [--smtp-*]  email MFA invite
  join-email   --payload <file> --words "<12 words>" --password <pwd>    accept email invite
  manifest-pull --buddy-addr <multiaddr> --password <pwd>               recover manifest from buddy
  show-phrase   --password <pwd>                                         show 12-word recovery phrase
  recover       --phrase "<12 words>" --password <pwd>                   restore identity from phrase

Commands (Phase 3 -- multi-circle & versioning):
  circle add  --name <n> --scheme <d/p> --password <pwd>
  circle list --password <pwd>
  circle rm   --name <n> --confirm-name <n> --password <pwd>
  versions    --file <path> --password <pwd>                             list file version history`)
}

// ─── BACKUP ──────────────────────────────────────────────────────────────────

func runBackup(args []string) {
	fs := flag.NewFlagSet("backup", flag.ExitOnError)
	src        := fs.String("src", cfg.Src, "Source file to back up")
	storeDir   := fs.String("store", storage.DefaultStorePath(), "Store directory")
	password   := fs.String("password", cfg.Password, "Encryption password")
	buddies    := fs.Int("buddies", 5, "Number of simulated buddies (determines RS scheme)")
	excl       := fs.String("exclude", cfg.Exclude, "Comma-separated glob patterns to skip (e.g. '*.tmp,.git')")
	uploadKbps := fs.Int("upload-kbps", cfg.UploadKbps, "Max upload speed in KB/s (0 = unlimited)")
	autoPrune  := fs.Bool("auto-prune", cfg.AutoPrune, "Apply default retention policy after each backup")
	_ = fs.Parse(args)

	if *src == "" || *password == "" {
		fs.Usage()
		os.Exit(1)
	}

	if *uploadKbps > 0 {
		p2pmod.SetUploadRate(*uploadKbps * 1024)
	}

	var ef *bbexclude.Filter
	if *excl != "" {
		var err error
		ef, err = bbexclude.Parse(*excl)
		if err != nil {
			log.Fatalf("[backup] --exclude: %v", err)
		}
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

	// ── 3. Walk src (file or directory) and back up each file ───────────────
	var lastFile string
	walkErr := filepath.Walk(*src, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fi.IsDir() {
			return nil
		}
		if ef != nil && ef.Match(path) {
			log.Printf("[backup] skip (excluded): %s", path)
			return nil
		}
		if err := backupOneFile(path, fi, store, ks, *password, masterKey, mf, scheme); err != nil {
			log.Printf("[backup] %s: %v", path, err)
		} else {
			lastFile = path
		}
		return nil
	})
	if walkErr != nil {
		log.Fatalf("[backup] walk %q: %v", *src, walkErr)
	}

	// ── 4. Save manifest ─────────────────────────────────────────────────────
	must(mf.Save())

	if *autoPrune {
		pruned := mf.PruneVersions(manifest.DefaultRetentionPolicy())
		if len(pruned) > 0 {
			st2, err := storage.New(*storeDir)
			if err == nil {
				for _, id := range pruned {
					st2.Delete(id)
				}
			}
			must(mf.Save())
			log.Printf("[backup] auto-prune: removed %d old version(s)", len(pruned))
		}
	}

	// Write tray status so the systray app can show last-backup time.
	if lastFile != "" {
		if cfgDir, err := cerclConfigDir(); err == nil {
			st := traystatus.Status{LastBackupAt: time.Now().UTC(), LastFile: lastFile}
			if werr := traystatus.Write(cfgDir, st); werr != nil {
				log.Printf("[backup] status write: %v", werr)
			}
		}
	}

	// ── 5. Push encrypted manifest to connected buddies (Phase 2i) ──────────
	blob, err := mf.EncryptedBytes()
	if err != nil {
		log.Printf("[backup] manifest encrypt: %v (skipping buddy push)", err)
	} else {
		priv, err := p2pmod.EnsurePeerIdentity(ks, *password)
		if err == nil {
			h, err := p2pmod.NewHost(priv, 0)
			if err == nil {
				defer h.Close()
				n := manifdist.PushToAll(context.Background(), h, h.ID().String(), blob)
				if n > 0 {
					log.Printf("[backup] manifest pushed to %d buddy/buddies", n)
				}
			}
		}
	}
}

// backupOneFile chunks, Reed-Solomon encodes, encrypts and stores a single
// file, then adds it to the manifest. Shard pushes to buddies happen inline.
func backupOneFile(src string, fi os.FileInfo, store *storage.Store, ks *bbcrypto.Keystore, password string, masterKey []byte, mf *manifest.Manifest, scheme protocol.RSScheme) error {
	log.Printf("[backup] chunking %q ...", src)
	chunks, err := chunker.ChunkFile(src, chunker.DefaultChunkSize)
	if err != nil {
		return fmt.Errorf("chunk: %w", err)
	}
	log.Printf("[backup] %d chunk(s)", len(chunks))

	fileHash := fileHashFromChunks(chunks)
	fileKey, err := bbcrypto.DeriveFileKey(masterKey, fileHash)
	if err != nil {
		return fmt.Errorf("derive key: %w", err)
	}

	enc, err := codec.NewEncoder(scheme)
	if err != nil {
		return fmt.Errorf("encoder: %w", err)
	}

	var shardLocations []protocol.ShardLocation
	shardCounter := 0

	for _, chunk := range chunks {
		chunkBytes, err := bbcompress.Compress(chunk.Data[:chunk.Size])
		if err != nil {
			return fmt.Errorf("compress chunk %d: %w", chunk.Index, err)
		}
		rawShards, err := enc.SplitChunkToShards(chunkBytes)
		if err != nil {
			return fmt.Errorf("RS encode chunk %d: %w", chunk.Index, err)
		}
		fileID := fileIDFromHash(fileHash)
		for si, shard := range rawShards {
			isParity := si >= scheme.DataShards
			idx := shardCounter
			shardCounter++
			ciphertext, err := bbcrypto.EncryptShard(fileKey, idx, shard)
			if err != nil {
				return fmt.Errorf("encrypt shard: %w", err)
			}
			if err := store.Put(fileID, idx, isParity, ciphertext); err != nil {
				return fmt.Errorf("store shard: %w", err)
			}
			shardLocations = append(shardLocations, protocol.ShardLocation{
				ShardIndex: idx,
				IsParity:   isParity,
				BuddyID:    "local",
				StorageKey: fmt.Sprintf("chunk%d-shard%d", chunk.Index, si),
			})
		}
	}

	entry, err := mf.Upsert(src, fileHash, fi.Size(), scheme, shardLocations)
	if err != nil {
		return fmt.Errorf("manifest upsert: %w", err)
	}
	entry.Compressed = true

	log.Printf("[backup] ✅ %s — file-id: %s  shards: %d",
		filepath.Base(src), entry.FileID, len(shardLocations))

	pushToBuddies(ks, password, fileIDFromHash(fileHash), shardLocations, store)
	return nil
}

// ─── RESTORE ─────────────────────────────────────────────────────────────────

func runRestore(args []string) {
	fs := flag.NewFlagSet("restore", flag.ExitOnError)
	fileID   := fs.String("file-id", "", "FileID UUID from the manifest (legacy; prefer --file)")
	filePath := fs.String("file", "", "Original file path to restore (looks up latest version)")
	ver      := fs.Int("version", 0, "Version number to restore (0 = latest, requires --file)")
	storeDir := fs.String("store", storage.DefaultStorePath(), "Store directory")
	out      := fs.String("out", "", "Output file path (required)")
	password := fs.String("password", cfg.Password, "Encryption password (required)")
	_ = fs.Parse(args)

	if *out == "" || *password == "" {
		fs.Usage()
		os.Exit(1)
	}
	if *fileID == "" && *filePath == "" {
		log.Fatal("[restore] one of --file-id or --file is required")
	}

	store := mustStore(*storeDir)
	ks := openOrCreateKeystore(*password)
	masterKey := ks.MasterKey()
	mf := openManifest(masterKey)

	var entry *protocol.ManifestEntry
	switch {
	case *fileID != "":
		entry = mf.Get(*fileID)
		if entry == nil {
			log.Fatalf("[restore] file-id %q not found in manifest", *fileID)
		}
	case *ver > 0:
		for _, e := range mf.ListVersions(*filePath) {
			if e.Version == *ver {
				entry = e
				break
			}
		}
		if entry == nil {
			log.Fatalf("[restore] %q version %d not found in manifest", *filePath, *ver)
		}
	default:
		entry = mf.Latest(*filePath)
		if entry == nil {
			log.Fatalf("[restore] %q not found in manifest", *filePath)
		}
		log.Printf("[restore] using latest version %d (backed %s)",
			entry.Version, entry.BackedAt.Format("2006-01-02 15:04:05"))
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

	// -- Phase 2b: open ephemeral P2P host to fetch missing shards from buddies --
	var restoreHost host.Host
	var buddyReg *buddy.Registry
	restoreCtx := context.Background()
	if privKey, err := p2pmod.EnsurePeerIdentity(ks, *password); err == nil {
		if rh, err := p2pmod.NewHost(privKey, 0); err == nil {
			restoreHost = rh
			defer rh.Close()
			if reg, err := openRegistry(ks); err == nil {
				buddyReg = reg
				// Connect to known buddies
				for _, entry := range reg.List() {
					pID, err := peer.Decode(entry.PeerID)
					if err != nil { continue }
					var addrs []multiaddr.Multiaddr
					for _, a := range entry.Addrs {
						ma, _ := multiaddr.NewMultiaddr(a)
						if ma != nil { addrs = append(addrs, ma) }
					}
					_ = rh.Connect(restoreCtx, peer.AddrInfo{ID: pID, Addrs: addrs})
				}
				log.Printf("[restore] P2P host ready, connected to %d buddy addr(s)", len(reg.List()))
			}
		}
	}
	ownPeerID := ""
	if restoreHost != nil {
		ownPeerID = restoreHost.ID().String()
	}

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

	// Accumulate chunk hashes to recompute the file's Merkle hash for verification.
	verifyHasher := sha256.New()

	for ci := 0; ci < numChunks; ci++ {
		if numChunks > 1 {
			log.Printf("[restore] chunk %d/%d", ci+1, numChunks)
		}
		// Collect and decrypt the shards for this original chunk.
		rawShards := make([][]byte, shardsPerChunk)
		for si := 0; si < shardsPerChunk; si++ {
			globalShardIdx := ci*shardsPerChunk + si
			loc := entry.Shards[globalShardIdx]

			ciphertext, err := store.Get(storeFileID, loc.ShardIndex)
			if err != nil {
				// Try to fetch from a connected buddy before giving up.
				if restoreHost != nil && buddyReg != nil {
					if fetched, ok := tryFetchFromBuddies(restoreCtx, restoreHost, buddyReg, ownPeerID, storeFileID, loc.ShardIndex); ok {
						log.Printf("[restore] fetched shard %d from buddy", globalShardIdx)
						ciphertext = fetched
						err = nil
					}
				}
				if err != nil {
					log.Printf("[restore] shard %d missing, will reconstruct", globalShardIdx)
					rawShards[si] = nil
					continue
				}
			}

			plaintext, err := bbcrypto.DecryptShard(fileKey, loc.ShardIndex, ciphertext)
			if err != nil {
				log.Printf("[restore] shard %d decrypt error: %v -- treating as missing", globalShardIdx, err)
				rawShards[si] = nil
				continue
			}
			rawShards[si] = plaintext
		}

		// RS reconstruct (handles nil shards).
		chunkData, err := enc.MergeShardToChunk(rawShards)
		must(err)

		if entry.Compressed {
			chunkData, err = bbcompress.Decompress(chunkData)
			if err != nil {
				log.Fatalf("[restore] decompress chunk %d: %v", ci, err)
			}
		} else {
			// Trim RS padding from every chunk: non-last chunks are DefaultChunkSize,
			// last chunk is entry.Size % DefaultChunkSize (or DefaultChunkSize if divisible).
			expectedSize := chunker.DefaultChunkSize
			if ci == numChunks-1 {
				if rem := int(entry.Size) % chunker.DefaultChunkSize; rem != 0 {
					expectedSize = rem
				}
			}
			if expectedSize < len(chunkData) {
				chunkData = chunkData[:expectedSize]
			}
		}

		chunkHash := sha256.Sum256(chunkData)
		verifyHasher.Write(chunkHash[:])

		if _, err := outFile.Write(chunkData); err != nil {
			log.Fatalf("[restore] write chunk %d: %v", ci, err)
		}
	}

	// Integrity verification: recompute the Merkle hash and compare to entry.Hash.
	if entry.Hash != "" {
		var gotHash [32]byte
		copy(gotHash[:], verifyHasher.Sum(nil))
		gotHex := hex.EncodeToString(gotHash[:])
		if gotHex != entry.Hash {
			outFile.Close()
			os.Remove(*out)
			log.Fatalf("[restore] INTEGRITY CHECK FAILED: hash mismatch (corrupted data, output deleted)")
		}
		log.Printf("[restore] integrity check passed")
	}

	log.Printf("[restore] ✅ restored to %q", *out)
}

// ─── LIST ─────────────────────────────────────────────────────────────────────

func runList(args []string) {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	storeDir := fs.String("store", storage.DefaultStorePath(), "Store directory")
	password := fs.String("password", cfg.Password, "Encryption password")
	all      := fs.Bool("all", false, "Show all versions (default: latest per path only)")
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

	if !*all {
		// Deduplicate: keep only the latest version per path.
		latest := make(map[string]*protocol.ManifestEntry)
		for _, e := range entries {
			prev, ok := latest[e.Path]
			if !ok || e.Version > prev.Version {
				latest[e.Path] = e
			}
		}
		entries = entries[:0]
		for _, e := range latest {
			entries = append(entries, e)
		}
	}

	fmt.Printf("%-4s  %-36s  %-50s  %10s  %s\n", "VER", "FILE-ID", "PATH", "SIZE", "BACKED AT")
	fmt.Println("─────────────────────────────────────────────────────────────────────────────────────────────────────────────────")
	for _, e := range entries {
		backedAt := e.BackedAt.Format("2006-01-02 15:04")
		if e.BackedAt.IsZero() {
			backedAt = e.Modified.Format("2006-01-02 15:04")
		}
		fmt.Printf("%-4d  %-36s  %-50s  %10d  %s\n",
			e.Version, e.FileID, e.Path, e.Size, backedAt)
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
	cfgDir, _ := cerclConfigDir()
	ksPath := filepath.Join(cfgDir, "keystore.enc")
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
	if err := mf.Load(); err != nil {
		if strings.Contains(err.Error(), "message authentication failed") {
			log.Fatal("manifest: decryption failed — the keystore master key does not match.\n" +
				"  This usually means 'cerclbackup init' was run after a backup was already created.\n" +
				"  To start fresh: cerclbackup init --force  (WARNING: deletes existing backup metadata)")
		}
		log.Fatalf("manifest: %v", err)
	}
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

// ---------------------------------------------------------------------------
// Config-dir helper
// ---------------------------------------------------------------------------

// cerclConfigDir returns the root directory for all CerclBackup data files.
// Override with CERCLBACKUP_CONFIG_DIR for testing or multi-instance setups.
func cerclConfigDir() (string, error) {
	if d := os.Getenv("CERCLBACKUP_CONFIG_DIR"); d != "" {
		return d, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "cerclbackup"), nil
}

// ---------------------------------------------------------------------------
// P2P helpers
// ---------------------------------------------------------------------------

func openKeystore(password string) (*bbcrypto.Keystore, error) {
	cfgDir, err := cerclConfigDir()
	if err != nil {
		return nil, err
	}
	ksPath := filepath.Join(cfgDir, "keystore.enc")
	ks := bbcrypto.NewKeystore(ksPath)
	if err := ks.Unlock(password); err != nil {
		return nil, fmt.Errorf("keystore unlock: %w", err)
	}
	return ks, nil
}

func openRegistry(ks *bbcrypto.Keystore) (*buddy.Registry, error) {
	cfgDir, err := cerclConfigDir()
	if err != nil {
		return nil, err
	}
	regPath := filepath.Join(cfgDir, "buddies.enc")
	return buddy.NewRegistry(regPath, ks.MasterKey())
}

func openInviteManager() *invite.Manager {
	cfgDir, _ := cerclConfigDir()
	invPath := filepath.Join(cfgDir, "invites.json")
	return invite.NewManager(invPath)
}

// ---------------------------------------------------------------------------
// serve
// ---------------------------------------------------------------------------

// serveLogFilter is a log.Writer that drops known-noisy lines emitted by
// third-party libraries (zeroconf, quic-go) that use log.Printf directly and
// therefore cannot be silenced via ipfs/go-log subsystem levels.
type serveLogFilter struct{ out io.Writer }

func (f *serveLogFilter) Write(p []byte) (int, error) {
	s := string(p)
	if strings.Contains(s, "Failed to set multicast interface") ||
		strings.Contains(s, "failed to sufficiently increase receive buffer size") {
		return len(p), nil
	}
	return f.out.Write(p)
}

func runServe(args []string) {
	// Silence ipfs/go-log subsystems that are verbose during normal operation.
	ipfslog.SetLogLevel("mdns", "error")             //nolint:errcheck
	ipfslog.SetLogLevel("dht", "error")              //nolint:errcheck
	ipfslog.SetLogLevel("dht/RtRefreshManager", "error") //nolint:errcheck

	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	password   := fs.String("password", cfg.Password, "keystore password (required)")
	port       := fs.Int("port", p2pmod.DefaultPort, "TCP/UDP port for libp2p")
	uploadKbps := fs.Int("upload-kbps", cfg.UploadKbps, "Max upload speed in KB/s (0 = unlimited)")
	healthAddr := fs.String("health-addr", cfg.HealthAddr, "HTTP health/metrics endpoint address (empty = disabled)")
	if err := fs.Parse(args); err != nil {
		log.Fatal(err)
	}
	if *password == "" {
		fs.Usage()
		os.Exit(1)
	}
	if *uploadKbps > 0 {
		p2pmod.SetUploadRate(*uploadKbps * 1024)
	}

	ks, err := openKeystore(*password)
	if err != nil {
		log.Fatal(err)
	}

	privKey, err := p2pmod.EnsurePeerIdentity(ks, *password)
	if err != nil {
		log.Fatal(err)
	}

	h, err := p2pmod.NewHost(privKey, *port)
	if err != nil {
		log.Fatal(err)
	}
	defer h.Close()

	reg, err := openRegistry(ks)
	if err != nil {
		log.Fatal(err)
	}

	cfgDir, _ := cerclConfigDir()
	storeDir := filepath.Join(cfgDir, "shards")
	bs := buddy.NewStore(storeDir)
	invMgr := openInviteManager()

	p2pmod.RegisterHandlers(h, reg, bs, invMgr)

	q := p2pmod.NewQueue(filepath.Join(cfgDir, "queue.json"))
	if _, err := p2pmod.StartMDNS(h, reg, q); err != nil {
		log.Printf("[serve] mDNS start: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Start Kademlia DHT for Internet peer discovery + hole punching.
	d, err := p2pmod.StartDHT(ctx, h)
	if err != nil {
		log.Printf("[serve] DHT start: %v (Internet buddies unavailable)", err)
	} else {
		defer d.Close()
		// Try to reach all registered buddies (LAN addrs first, then DHT).
		go p2pmod.DialAllBuddies(ctx, h, d, reg)
	}

	scrubpkg.New(bs, h, reg).Start(ctx, 6*time.Hour)

	serveStart := time.Now()

	// Optional HTTP health / metrics endpoint.
	if *healthAddr != "" {
		mux := http.NewServeMux()
		mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			peers := len(h.Network().Peers())
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status":    "ok",
				"version":   version.AppVersion,
				"peer_id":   h.ID().String(),
				"peers":     peers,
				"uptime_s":  int(time.Since(serveStart).Seconds()),
			})
		})
		mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain; version=0.0.4")
			uptime := int(time.Since(serveStart).Seconds())
			peers := len(h.Network().Peers())
			buddies := reg.List()
			shards, _ := bs.ListAll()
			fmt.Fprintf(w, "# HELP cerclbackup_uptime_seconds Seconds since daemon start\n")
			fmt.Fprintf(w, "cerclbackup_uptime_seconds %d\n", uptime)
			fmt.Fprintf(w, "# HELP cerclbackup_peers_connected Connected libp2p peers\n")
			fmt.Fprintf(w, "cerclbackup_peers_connected %d\n", peers)
			fmt.Fprintf(w, "# HELP cerclbackup_buddies_registered Registered buddy count\n")
			fmt.Fprintf(w, "cerclbackup_buddies_registered %d\n", len(buddies))
			fmt.Fprintf(w, "# HELP cerclbackup_shards_stored Shard files on disk\n")
			fmt.Fprintf(w, "cerclbackup_shards_stored %d\n", len(shards))
		})
		srv := &http.Server{Addr: *healthAddr, Handler: mux}
		go func() {
			log.Printf("[serve] health endpoint: http://%s/health", *healthAddr)
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Printf("[serve] health server: %v", err)
			}
		}()
		go func() {
			<-ctx.Done()
			srv.Close()
		}()
	}

	fmt.Printf("CerclBackup daemon running\n")
	fmt.Printf("Peer ID : %s\n", h.ID())
	for _, a := range h.Addrs() {
		fmt.Printf("Address : %s/p2p/%s\n", a, h.ID())
	}
	if *healthAddr != "" {
		fmt.Printf("Health  : http://%s/health\n", *healthAddr)
	}

	<-ctx.Done()
	fmt.Println("\nShutting down.")
}

// ---------------------------------------------------------------------------
// invite
// ---------------------------------------------------------------------------

func runInvite(args []string) {
	fs := flag.NewFlagSet("invite", flag.ExitOnError)
	password  := fs.String("password", cfg.Password, "keystore password (required)")
	servePort := fs.Int("port", 7742, "port your cerclbackup serve is listening on")
	name      := fs.String("name", "", "friendly name to show your buddy (optional)")
	if err := fs.Parse(args); err != nil {
		log.Fatal(err)
	}
	if *password == "" {
		fs.Usage()
		os.Exit(1)
	}

	ks, err := openKeystore(*password)
	if err != nil {
		log.Fatal(err)
	}

	privKey, err := p2pmod.EnsurePeerIdentity(ks, *password)
	if err != nil {
		log.Fatal(err)
	}

	// Open a temporary host on a random port just to collect local interface
	// addresses, then replace the random port with the real serve port.
	tmpHost, err := p2pmod.NewHost(privKey, 0)
	if err != nil {
		log.Fatal(err)
	}
	peerID := tmpHost.ID().String()
	var addrs []string
	for _, ma := range tmpHost.Addrs() {
		s := ma.String()
		// Keep only IPv4 and IPv6 routable/loopback addresses; skip QUIC duplicates.
		if strings.Contains(s, "/udp/") {
			continue
		}
		// Replace the random port with the real serve port.
		parts := strings.Split(s, "/tcp/")
		if len(parts) == 2 {
			s = parts[0] + fmt.Sprintf("/tcp/%d", *servePort)
		}
		addrs = append(addrs, s+"/p2p/"+peerID)
	}
	tmpHost.Close()

	invMgr := openInviteManager()
	code, err := invMgr.Generate()
	if err != nil {
		log.Fatal(err)
	}

	words := code.Words
	wlist := splitWords(words)
	verbally := ""
	if len(wlist) >= 3 {
		verbally = fmt.Sprintf("%s %s %s", wlist[len(wlist)-3], wlist[len(wlist)-2], wlist[len(wlist)-1])
	}

	// Build the join command the buddy should run.
	// Priority: real LAN (192.168/10./172.[16-31].) > loopback > anything else.
	// Skip link-local (169.254.x.x) — those are APIPA/virtual-adapter addresses
	// that are unreachable even on the same machine.
	joinAddr := ""
	for _, a := range addrs {
		if strings.Contains(a, "/169.254.") {
			continue
		}
		if strings.Contains(a, "/127.0.0.1/") || strings.Contains(a, "/::1/") {
			continue
		}
		joinAddr = a
		break
	}
	if joinAddr == "" {
		// Fall back to loopback — always reachable on the same machine.
		for _, a := range addrs {
			if strings.Contains(a, "/127.0.0.1/tcp/") {
				joinAddr = a
				break
			}
		}
	}
	if joinAddr == "" && len(addrs) > 0 {
		joinAddr = addrs[0]
	}

	nameFlag := ""
	if *name != "" {
		nameFlag = fmt.Sprintf(" --name %q", *name)
	}

	fmt.Println()
	fmt.Println("── Your addresses (share ONE with your buddy) ──────────────────────────")
	for _, a := range addrs {
		fmt.Printf("  %s\n", a)
	}
	fmt.Println()
	fmt.Println("── Invite code (give to your buddy) ────────────────────────────────────")
	fmt.Printf("  %s\n", words)
	fmt.Println()
	fmt.Println("── Your buddy should run ───────────────────────────────────────────────")
	fmt.Printf("  cerclbackup join --addr %s --words %q%s --password <their-pw>\n",
		joinAddr, words, nameFlag)
	fmt.Println()
	fmt.Printf("Verbally confirm the LAST 3 WORDS with your buddy: %q\n", verbally)
	fmt.Printf("Code expires in 24 hours.\n")
}

func splitWords(s string) []string {
	var words []string
	start := 0
	for i, c := range s {
		if c == ' ' {
			if i > start {
				words = append(words, s[start:i])
			}
			start = i + 1
		}
	}
	if start < len(s) {
		words = append(words, s[start:])
	}
	return words
}

// ---------------------------------------------------------------------------
// join
// ---------------------------------------------------------------------------

func runJoin(args []string) {
	fs := flag.NewFlagSet("join", flag.ExitOnError)
	addr := fs.String("addr", "", "full multiaddr of the inviter, e.g. /ip4/1.2.3.4/tcp/7742/p2p/<peerID>")
	words := fs.String("words", "", "12-word invite mnemonic from your buddy")
	password := fs.String("password", cfg.Password, "keystore password (required)")
	name := fs.String("name", "", "friendly name for this buddy (optional)")
	if err := fs.Parse(args); err != nil {
		log.Fatal(err)
	}
	if *addr == "" || *words == "" || *password == "" {
		fs.Usage()
		os.Exit(1)
	}

	ks, err := openKeystore(*password)
	if err != nil {
		log.Fatal(err)
	}
	privKey, err := p2pmod.EnsurePeerIdentity(ks, *password)
	if err != nil {
		log.Fatal(err)
	}
	h, err := p2pmod.NewHost(privKey, 0)
	if err != nil {
		log.Fatal(err)
	}
	defer h.Close()

	token, err := invite.TokenFromMnemonic(*words)
	if err != nil {
		log.Fatal(err)
	}

	maddr, err := multiaddr.NewMultiaddr(*addr)
	if err != nil {
		log.Fatalf("invalid addr: %v", err)
	}
	addrInfo, err := peer.AddrInfoFromP2pAddr(maddr)
	if err != nil {
		log.Fatalf("addr parse: %v", err)
	}

	if err := h.Connect(context.Background(), *addrInfo); err != nil {
		log.Fatalf("connect: %v", err)
	}

	reg, err := openRegistry(ks)
	if err != nil {
		log.Fatal(err)
	}

	if err := p2pmod.SendInviteRequest(context.Background(), h, reg, addrInfo.ID, token, *name); err != nil {
		log.Fatalf("invite: %v", err)
	}

	fmt.Printf("Paired with buddy %s\n", addrInfo.ID)
}

// ---------------------------------------------------------------------------
// buddy list
// ---------------------------------------------------------------------------

func runBuddyLegacy(args []string) {
	if len(args) == 0 || args[0] != "list" {
		fmt.Fprintln(os.Stderr, "usage: cerclbackup buddy list --password <pwd>")
		os.Exit(1)
	}

	fs := flag.NewFlagSet("buddy list", flag.ExitOnError)
	password := fs.String("password", cfg.Password, "keystore password (required)")
	if err := fs.Parse(args[1:]); err != nil {
		log.Fatal(err)
	}
	if *password == "" {
		fs.Usage()
		os.Exit(1)
	}

	ks, err := openKeystore(*password)
	if err != nil {
		log.Fatal(err)
	}
	reg, err := openRegistry(ks)
	if err != nil {
		log.Fatal(err)
	}

	entries := reg.List()
	if len(entries) == 0 {
		fmt.Println("No buddies yet.")
		return
	}
	fmt.Printf("%-20s  %s\n", "Friendly Name", "Peer ID")
	fmt.Printf("%-20s  %s\n", "-------------", "-------")
	for _, e := range entries {
		name := e.FriendlyName
		if name == "" {
			name = "(unnamed)"
		}
		fmt.Printf("%-20s  %s\n", name, e.PeerID)
	}
}

// ---------------------------------------------------------------------------
// revoke
// ---------------------------------------------------------------------------

func runRevoke(args []string) {
	fs := flag.NewFlagSet("revoke", flag.ExitOnError)
	peerID := fs.String("peer-id", "", "peer ID to remove (required)")
	password := fs.String("password", cfg.Password, "keystore password (required)")
	if err := fs.Parse(args); err != nil {
		log.Fatal(err)
	}
	if *peerID == "" || *password == "" {
		fs.Usage()
		os.Exit(1)
	}

	ks, err := openKeystore(*password)
	if err != nil {
		log.Fatal(err)
	}
	reg, err := openRegistry(ks)
	if err != nil {
		log.Fatal(err)
	}

	if err := reg.Remove(*peerID); err != nil {
		log.Fatalf("revoke: %v", err)
	}
	fmt.Printf("Buddy %s removed.\n", *peerID)

	// Auto-rebalance: push all locally-stored shards to the surviving buddies
	// so redundancy is restored without manual intervention.
	fmt.Println("Rebalancing shards across remaining buddies...")
	rebalanceWithKeystore(ks, *password)
}

// ---------------------------------------------------------------------------
// Phase 2b -- P2P push/fetch helpers
// ---------------------------------------------------------------------------

// pushToBuddies opens an ephemeral P2P host and pushes all shards for fileID
// to every registered buddy. Offline buddies are enqueued for retry.
func pushToBuddies(ks *bbcrypto.Keystore, password, fileID string, locs []protocol.ShardLocation, store *storage.Store) {
	privKey, err := p2pmod.EnsurePeerIdentity(ks, password)
	if err != nil {
		log.Printf("[backup] P2P identity: %v", err)
		return
	}
	h, err := p2pmod.NewHost(privKey, 0)
	if err != nil {
		log.Printf("[backup] P2P host: %v", err)
		return
	}
	defer h.Close()

	reg, err := openRegistry(ks)
	if err != nil {
		log.Printf("[backup] registry: %v", err)
		return
	}
	buddies := reg.List()
	if len(buddies) == 0 {
		return
	}

	cfgDir, _ := cerclConfigDir()
	q := p2pmod.NewQueue(filepath.Join(cfgDir, "queue.json"))
	ownerID := h.ID().String()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	for _, entry := range buddies {
		peerID, err := peer.Decode(entry.PeerID)
		if err != nil {
			continue
		}
		// Try to connect via known addresses
		var addrs []multiaddr.Multiaddr
		for _, a := range entry.Addrs {
			ma, err := multiaddr.NewMultiaddr(a)
			if err == nil {
				addrs = append(addrs, ma)
			}
		}
		connected := false
		if len(addrs) > 0 {
			if err := h.Connect(ctx, peer.AddrInfo{ID: peerID, Addrs: addrs}); err == nil {
				connected = true
			}
		}

		if !connected {
			log.Printf("[backup] buddy %s unreachable, enqueueing %d shards", entry.PeerID, len(locs))
			for _, loc := range locs {
				ciphertext, err := store.Get(fileID, loc.ShardIndex)
				if err != nil {
					continue
				}
				_ = q.Enqueue(entry.PeerID, wire.ShardPush{
					Type:       wire.TypeShardPush,
					OwnerID:    ownerID,
					FileID:     fileID,
					ShardIndex: loc.ShardIndex,
					IsParity:   loc.IsParity,
					Data:       ciphertext,
				})
			}
			continue
		}

		pushed := 0
		for _, loc := range locs {
			ciphertext, err := store.Get(fileID, loc.ShardIndex)
			if err != nil {
				continue
			}
			if err := p2pmod.PushShard(ctx, h, peerID, ownerID, fileID, loc.ShardIndex, loc.IsParity, ciphertext); err != nil {
				log.Printf("[backup] push shard %d to %s: %v", loc.ShardIndex, entry.PeerID, err)
			} else {
				pushed++
			}
		}
		log.Printf("[backup] pushed %d/%d shards to buddy %s", pushed, len(locs), entry.PeerID)
	}
}


// tryFetchFromBuddies tries each buddy in reg to fetch a missing encrypted shard.
func tryFetchFromBuddies(ctx context.Context, h host.Host, reg *buddy.Registry, ownerPeerID, fileID string, shardIdx int) ([]byte, bool) {
	for _, entry := range reg.List() {
		peerID, err := peer.Decode(entry.PeerID)
		if err != nil {
			continue
		}
		data, err := p2pmod.FetchShard(ctx, h, peerID, ownerPeerID, fileID, shardIdx)
		if err == nil {
			return data, true
		}
	}
	return nil, false
}

// ---------------------------------------------------------------------------
// Phase 2d -- Rebalance
// ---------------------------------------------------------------------------

func runRebalance(args []string) {
	fs := flag.NewFlagSet("rebalance", flag.ExitOnError)
	password := fs.String("password", cfg.Password, "keystore password (required)")
	storeDir := fs.String("store", storage.DefaultStorePath(), "local shard store directory")
	if err := fs.Parse(args); err != nil {
		log.Fatal(err)
	}
	if *password == "" {
		fs.Usage()
		os.Exit(1)
	}
	_ = storeDir // used by rebalanceWithKeystore via DefaultStorePath or explicit value

	ks, err := openKeystore(*password)
	if err != nil {
		log.Fatal(err)
	}
	rebalanceWithKeystore(ks, *password)
}

// rebalanceWithKeystore pushes every locally-stored shard to every registered
// buddy. It is called both by runRebalance and automatically by runRevoke.
func rebalanceWithKeystore(ks *bbcrypto.Keystore, password string) {
	privKey, err := p2pmod.EnsurePeerIdentity(ks, password)
	if err != nil {
		log.Printf("[rebalance] P2P identity: %v", err)
		return
	}
	h, err := p2pmod.NewHost(privKey, 0)
	if err != nil {
		log.Printf("[rebalance] P2P host: %v", err)
		return
	}
	defer h.Close()

	reg, err := openRegistry(ks)
	if err != nil {
		log.Printf("[rebalance] registry: %v", err)
		return
	}

	localStore, err := storage.New(storage.DefaultStorePath())
	if err != nil {
		log.Printf("[rebalance] open local store: %v", err)
		return
	}

	mf := openManifest(ks.MasterKey())

	entries := mf.All()
	fileIDs := make([]string, 0, len(entries))
	for _, e := range entries {
		fileIDs = append(fileIDs, e.FileID)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	ownerID := h.ID().String()
	rb := rebalance.New(ownerID, localStore, reg, h)
	res, err := rb.Run(ctx, fileIDs)
	if err != nil {
		log.Printf("[rebalance] run: %v", err)
		return
	}

	fmt.Printf("Rebalance complete: %d file(s), %d/%d shards pushed to buddies.\n",
		res.FilesProcessed, res.ShardsOK, res.ShardsAttempted)
	if len(res.Errors) > 0 {
		fmt.Printf("  %d error(s):\n", len(res.Errors))
		for _, e := range res.Errors {
			fmt.Printf("    - %s\n", e)
		}
	}
}

// ---------------------------------------------------------------------------
// Phase 2i -- Distributed manifest: manifest-pull
// ---------------------------------------------------------------------------

// runManifestPull fetches the encrypted manifest from a buddy and writes it
// to the default local manifest path, overwriting any existing file.
// Used when the owner's machine is replaced and the local manifest is lost.
func runManifestPull(args []string) {
	fs := flag.NewFlagSet("manifest-pull", flag.ExitOnError)
	buddyAddr := fs.String("addr", "", "Buddy multiaddr (required, e.g. /ip4/1.2.3.4/tcp/7742/p2p/<peerID>)")
	password := fs.String("password", cfg.Password, "Keystore password (required)")
	out := fs.String("out", manifest.DefaultManifestPath(), "Output path for recovered manifest")
	_ = fs.Parse(args)
	if *buddyAddr == "" || *password == "" {
		fs.Usage()
		os.Exit(1)
	}

	ks, err := openKeystore(*password)
	if err != nil {
		log.Fatalf("manifest-pull: %v", err)
	}

	priv, err := p2pmod.EnsurePeerIdentity(ks, *password)
	if err != nil {
		log.Fatalf("manifest-pull: peer identity: %v", err)
	}

	h, err := p2pmod.NewHost(priv, 0)
	if err != nil {
		log.Fatalf("manifest-pull: host: %v", err)
	}
	defer h.Close()

	ma, err := multiaddr.NewMultiaddr(*buddyAddr)
	if err != nil {
		log.Fatalf("manifest-pull: parse addr %q: %v", *buddyAddr, err)
	}
	pi, err := peer.AddrInfoFromP2pAddr(ma)
	if err != nil {
		log.Fatalf("manifest-pull: addr info: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := h.Connect(ctx, *pi); err != nil {
		log.Fatalf("manifest-pull: connect to buddy: %v", err)
	}

	blob, err := manifdist.PullFromBuddy(ctx, h, pi.ID, h.ID().String())
	if err != nil {
		log.Fatalf("manifest-pull: %v", err)
	}

	if err := os.MkdirAll(filepath.Dir(*out), 0700); err != nil {
		log.Fatalf("manifest-pull: mkdir: %v", err)
	}
	if err := os.WriteFile(*out, blob, 0600); err != nil {
		log.Fatalf("manifest-pull: write: %v", err)
	}
	fmt.Printf("Manifest recovered from buddy %s → %s (%d bytes)\n", pi.ID, *out, len(blob))
}

// ---------------------------------------------------------------------------
// Phase 2g -- Recovery phrase: show-phrase / recover
// ---------------------------------------------------------------------------

func runShowPhrase(args []string) {
	fs := flag.NewFlagSet("show-phrase", flag.ExitOnError)
	password := fs.String("password", cfg.Password, "Keystore password (required)")
	_ = fs.Parse(args)
	if *password == "" {
		fs.Usage()
		os.Exit(1)
	}

	ks, err := openKeystore(*password)
	if err != nil {
		log.Fatalf("show-phrase: %v", err)
	}

	seed := ks.LoadExtra(identitySeedKeyName)
	if len(seed) == 0 {
		log.Fatal("show-phrase: this keystore has no identity seed (created before Phase 2g). " +
			"Your peer identity cannot be recovered by phrase. " +
			"Back up your keystore file directly.")
	}

	mnemonic, err := identityMnemonicFromSeed(seed)
	if err != nil {
		log.Fatalf("show-phrase: %v", err)
	}

	fmt.Println("Your 12-word recovery phrase (write this down in a safe place):")
	fmt.Println()
	fmt.Println(" ", mnemonic)
	fmt.Println()
	fmt.Println("Anyone with this phrase can restore your CerclBackup identity.")
}

func runRecover(args []string) {
	fs := flag.NewFlagSet("recover", flag.ExitOnError)
	phrase := fs.String("phrase", "", "12-word recovery phrase (required)")
	password := fs.String("password", cfg.Password, "New keystore password (required)")
	_ = fs.Parse(args)
	if *phrase == "" || *password == "" {
		fs.Usage()
		os.Exit(1)
	}

	seed, err := identitySeedFromMnemonic(*phrase)
	if err != nil {
		log.Fatalf("recover: %v", err)
	}

	// Create a fresh keystore at the default location.
	ksPath := bbcrypto.DefaultKeystorePath()
	ks := bbcrypto.NewKeystore(ksPath)
	if err := ks.Create(*password); err != nil {
		log.Fatalf("recover: create keystore: %v", err)
	}

	priv, err := p2pmod.EnsurePeerIdentityFromSeed(ks, seed, *password)
	if err != nil {
		log.Fatalf("recover: derive identity: %v", err)
	}

	peerID, err := peer.IDFromPrivateKey(priv)
	if err != nil {
		log.Fatalf("recover: peer ID: %v", err)
	}

	fmt.Printf("Identity restored successfully.\nPeer ID: %s\n", peerID)
	fmt.Println("Run `cerclbackup serve` to reconnect with your buddies.")
}

// ---------------------------------------------------------------------------
// Phase 2f -- Email invite (dual-channel MFA)
// ---------------------------------------------------------------------------

func runInviteEmail(args []string) {
	fs := flag.NewFlagSet("invite-email", flag.ExitOnError)
	to := fs.String("to", "", "recipient email address (required)")
	circle := fs.String("circle", "CerclBackup", "circle name shown in email")
	password := fs.String("password", cfg.Password, "keystore password (required)")
	smtpHost := fs.String("smtp-host", "", "SMTP host (omit to print email to stdout)")
	smtpPort := fs.Int("smtp-port", 587, "SMTP port")
	smtpUser := fs.String("smtp-user", "", "SMTP username")
	smtpPass := fs.String("smtp-pass", "", "SMTP password")
	smtpFrom := fs.String("smtp-from", "", "SMTP sender address")
	if err := fs.Parse(args); err != nil {
		log.Fatal(err)
	}
	if *to == "" || *password == "" {
		fs.Usage()
		os.Exit(1)
	}

	ks, err := openKeystore(*password)
	if err != nil {
		log.Fatal(err)
	}

	privKey, err := p2pmod.EnsurePeerIdentity(ks, *password)
	if err != nil {
		log.Fatal(err)
	}
	h, err := p2pmod.NewHost(privKey, 0)
	if err != nil {
		log.Fatal(err)
	}
	defer h.Close()

	// Extract raw Ed25519 private key from libp2p key.
	rawPriv, err := privKey.Raw()
	if err != nil {
		log.Fatalf("invite-email: raw private key: %v", err)
	}
	edPriv := rawPriv // go ed25519.PrivateKey is []byte

	payload, words, err := emailinvite.Generate(edPriv, h.ID().String(), *circle, 48*time.Hour)
	if err != nil {
		log.Fatalf("invite-email: generate: %v", err)
	}

	// Register commitment in invite manager so join-email can verify.
	invMgr := openInviteManager()
	secret, _ := emailinvite.SecretFromWords(words)
	expiry, _ := time.Parse(time.RFC3339, payload.Expiry)
	sum := sha256Sum(secret)
	if err := invMgr.AddCommitment(sum[:], expiry); err != nil {
		log.Fatalf("invite-email: register commitment: %v", err)
	}

	// Send or print the payload.
	if *smtpHost != "" {
		cfg := emailinvite.SMTPConfig{
			Host:     *smtpHost,
			Port:     *smtpPort,
			Username: *smtpUser,
			Password: *smtpPass,
			From:     *smtpFrom,
		}
		if err := emailinvite.Send(cfg, *to, payload); err != nil {
			log.Fatalf("invite-email: send: %v", err)
		}
		fmt.Printf("Email sent to %s\n", *to)
	} else {
		data, _ := emailinvite.ToJSON(payload)
		fmt.Println("=== PASTE THIS INTO YOUR EMAIL ===")
		fmt.Println(string(data))
		fmt.Println("==================================")
	}

	fmt.Println("\n*** SHARE THIS CODE VIA SMS / SIGNAL / VOICE — NOT BY EMAIL ***")
	fmt.Printf("12-word OOB code: %s\n", words)
	fmt.Printf("Peer ID: %s\n", h.ID())
}

func runJoinEmail(args []string) {
	fs := flag.NewFlagSet("join-email", flag.ExitOnError)
	payloadFile := fs.String("payload", "", "path to invite JSON file (required)")
	wordsStr := fs.String("words", "", "12-word OOB code received out-of-band (required)")
	password := fs.String("password", cfg.Password, "keystore password (required)")
	if err := fs.Parse(args); err != nil {
		log.Fatal(err)
	}
	if *payloadFile == "" || *wordsStr == "" || *password == "" {
		fs.Usage()
		os.Exit(1)
	}

	data, err := os.ReadFile(*payloadFile)
	if err != nil {
		log.Fatalf("join-email: read payload: %v", err)
	}
	payload, err := emailinvite.FromJSON(data)
	if err != nil {
		log.Fatalf("join-email: parse payload: %v", err)
	}

	// Dual-channel verification: signature + commitment.
	if err := emailinvite.Verify(payload, *wordsStr); err != nil {
		log.Fatalf("join-email: verification failed: %v", err)
	}
	fmt.Println("Invite verified (signature + OOB commitment match).")

	ks, err := openKeystore(*password)
	if err != nil {
		log.Fatal(err)
	}

	privKey, err := p2pmod.EnsurePeerIdentity(ks, *password)
	if err != nil {
		log.Fatal(err)
	}
	h, err := p2pmod.NewHost(privKey, 0)
	if err != nil {
		log.Fatal(err)
	}
	defer h.Close()

	reg, err := openRegistry(ks)
	if err != nil {
		log.Fatal(err)
	}

	// Decode the OOB secret to use as the invite token.
	secret, err := emailinvite.SecretFromWords(*wordsStr)
	if err != nil {
		log.Fatalf("join-email: decode words: %v", err)
	}

	// Resolve inviter's peer ID and connect.
	inviterPeerID, err := peer.Decode(payload.PeerID)
	if err != nil {
		log.Fatalf("join-email: decode peer ID: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := p2pmod.SendInviteRequest(ctx, h, reg, inviterPeerID, secret, h.ID().String()); err != nil {
		log.Fatalf("join-email: P2P handshake: %v", err)
	}

	fmt.Printf("Joined circle \"%s\" — buddy %s added.\n", payload.Circle, payload.PeerID)
}

func sha256Sum(data []byte) [32]byte {
	return sha256.Sum256(data)
}

// ── runInit ──────────────────────────────────────────────────────────────────

func runInit(args []string) int {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	password := fs.String("password", cfg.Password, "Keystore password (skips interactive prompt)")
	noPrompt := fs.Bool("no-prompt", false, "Skip all interactive prompts (for scripted use)")
	storeDir := fs.String("store", storage.DefaultStorePath(), "Shard store directory to create")
	force    := fs.Bool("force", false, "Overwrite existing keystore and manifest (WARNING: loses access to previous backups)")
	_ = fs.Parse(args)

	// ── 1. Password ──────────────────────────────────────────────────────────
	pw := *password
	if pw == "" {
		if *noPrompt {
			fmt.Fprintln(os.Stderr, "init: --password is required when --no-prompt is set")
			return 1
		}
		var err error
		pw, err = promptPassword("Choose a keystore password: ")
		if err != nil {
			log.Printf("init: password prompt: %v", err)
			return 1
		}
		confirm, err := promptPassword("Confirm password: ")
		if err != nil {
			log.Printf("init: confirm prompt: %v", err)
			return 1
		}
		if pw != confirm {
			fmt.Fprintln(os.Stderr, "Passwords do not match.")
			return 1
		}
	}

	// ── 2. Create keystore ───────────────────────────────────────────────────
	cfgDir, err := cerclConfigDir()
	if err != nil {
		log.Printf("init: config dir: %v", err)
		return 1
	}
	ksDir := cfgDir
	if err := os.MkdirAll(ksDir, 0o700); err != nil {
		log.Printf("init: mkdir: %v", err)
		return 1
	}
	ksPath := filepath.Join(ksDir, "keystore.enc")

	if _, err := os.Stat(ksPath); err == nil {
		if !*force {
			fmt.Fprintln(os.Stderr, "error: keystore already exists at", ksPath)
			fmt.Fprintln(os.Stderr, "       Run 'cerclbackup init --force' to reinitialize.")
			fmt.Fprintln(os.Stderr, "       WARNING: --force deletes all existing backup metadata.")
			return 1
		}
		// Remove keystore, manifest, and shard store — all three are
		// keyed to the old master key and cannot be used after reinit.
		os.Remove(ksPath)
		os.Remove(manifest.DefaultManifestPath())
		os.RemoveAll(storage.DefaultStorePath())
	}

	ks := bbcrypto.NewKeystore(ksPath)
	if err := ks.Create(pw); err != nil {
		log.Printf("init: create keystore: %v", err)
		return 1
	}

	// ── 3. Generate peer identity ────────────────────────────────────────────
	privKey, err := p2pmod.EnsurePeerIdentity(ks, pw)
	if err != nil {
		log.Printf("init: peer identity: %v", err)
		return 1
	}
	peerID, err := peer.IDFromPrivateKey(privKey)
	if err != nil {
		log.Printf("init: peer id: %v", err)
		return 1
	}

	// ── 4. Show recovery phrase ───────────────────────────────────────────────
	seedBytes := ks.LoadExtra(identity.KeyName)
	phrase := ""
	if len(seedBytes) > 0 {
		phrase, err = identity.MnemonicFromSeed(seedBytes)
		if err != nil {
			log.Printf("init: mnemonic: %v", err)
		}
	}

	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════════════════╗")
	fmt.Println("║           CerclBackup — First-Run Setup                  ║")
	fmt.Println("╚══════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Printf("Peer ID : %s\n", peerID)
	fmt.Println()

	if phrase != "" {
		fmt.Println("Recovery phrase (write this down — it restores your identity):")
		fmt.Println()
		fmt.Printf("  %s\n", phrase)
		fmt.Println()
		if !*noPrompt {
			fmt.Print("Press Enter once you have written down the phrase... ")
			bufio.NewReader(os.Stdin).ReadString('\n')
		}
	}

	// ── 5. Create Default circle ─────────────────────────────────────────────
	mgr := circle.NewManager(ks, pw)
	if _, err := mgr.GetOrDefault("", pw); err != nil {
		log.Printf("init: create default circle: %v", err)
		return 1
	}

	// ── 6. Create store directory ─────────────────────────────────────────────
	if err := os.MkdirAll(*storeDir, 0o700); err != nil {
		log.Printf("init: store dir: %v", err)
		return 1
	}

	// ── 7. Summary ────────────────────────────────────────────────────────────
	fmt.Println("Setup complete.")
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  cerclbackup backup  --src <file>  --password <pw>")
	fmt.Println("  cerclbackup watch   --src <dir>   --password <pw>")
	fmt.Println("  cerclbackup invite  --buddy-addr <multiaddr> --password <pw>")
	fmt.Printf("\nKeystore : %s\n", ksPath)
	fmt.Printf("Store    : %s\n", *storeDir)
	return 0
}

// ── runBuddy ──────────────────────────────────────────────────────────────────

// runBuddy dispatches sub-commands: buddy list (existing), buddy status, buddy rm.
func runBuddy(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: cerclbackup buddy <list|status|rm> [flags]")
		return 1
	}
	switch args[0] {
	case "status":
		return runBuddyStatus(args[1:])
	case "list":
		runBuddyLegacy(args) // existing list handler
		return 0
	case "rm":
		return runBuddyRm(args[1:])
	default:
		runBuddyLegacy(args)
		return 0
	}
}

func runBuddyRm(args []string) int {
	fs := flag.NewFlagSet("buddy rm", flag.ExitOnError)
	peerID      := fs.String("peer-id", "", "Peer ID to remove (required)")
	password    := fs.String("password", cfg.Password, "Keystore password (required)")
	noRebalance := fs.Bool("no-rebalance", false, "Skip automatic rebalance after removal")
	_ = fs.Parse(args)

	if *peerID == "" || *password == "" {
		fmt.Fprintln(os.Stderr, "buddy rm: --peer-id and --password are required")
		return 1
	}

	ks, err := openKeystore(*password)
	if err != nil {
		log.Printf("buddy rm: %v", err)
		return 1
	}
	reg, err := openRegistry(ks)
	if err != nil {
		log.Printf("buddy rm: registry: %v", err)
		return 1
	}

	if err := reg.Remove(*peerID); err != nil {
		log.Printf("buddy rm: %v", err)
		return 1
	}
	fmt.Printf("Buddy %s removed.\n", *peerID)

	if !*noRebalance {
		fmt.Println("Rebalancing shards across remaining buddies...")
		rebalanceWithKeystore(ks, *password)
	}
	return 0
}

func runBuddyStatus(args []string) int {
	fs := flag.NewFlagSet("buddy status", flag.ExitOnError)
	password := fs.String("password", cfg.Password, "Keystore password (required)")
	timeout  := fs.Duration("timeout", 5*time.Second, "Connect timeout per buddy")
	_ = fs.Parse(args)

	if *password == "" {
		fmt.Fprintln(os.Stderr, "buddy status: --password is required")
		return 1
	}

	ks, err := openKeystore(*password)
	if err != nil {
		log.Printf("buddy status: %v", err)
		return 1
	}

	reg, err := openRegistry(ks)
	if err != nil {
		log.Printf("buddy status: registry: %v", err)
		return 1
	}

	buddies := reg.List()
	if len(buddies) == 0 {
		fmt.Println("No buddies registered yet.  Use 'cerclbackup invite' to add one.")
		return 0
	}

	privKey, err := p2pmod.EnsurePeerIdentity(ks, *password)
	if err != nil {
		log.Printf("buddy status: peer identity: %v", err)
		return 1
	}
	h, err := p2pmod.NewHost(privKey, 0)
	if err != nil {
		log.Printf("buddy status: host: %v", err)
		return 1
	}
	defer h.Close()

	type result struct {
		entry   *buddy.Entry
		ok      bool
		latency time.Duration
	}

	results := make([]result, len(buddies))
	var wg sync.WaitGroup
	for i, e := range buddies {
		wg.Add(1)
		go func(idx int, entry *buddy.Entry) {
			defer wg.Done()
			pid, err := peer.Decode(entry.PeerID)
			if err != nil {
				results[idx] = result{entry: entry}
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), *timeout)
			defer cancel()
			addrs := make([]multiaddr.Multiaddr, 0, len(entry.Addrs))
			for _, a := range entry.Addrs {
				if ma, err := multiaddr.NewMultiaddr(a); err == nil {
					addrs = append(addrs, ma)
				}
			}
			start := time.Now()
			err = h.Connect(ctx, peer.AddrInfo{ID: pid, Addrs: addrs})
			lat := time.Since(start)
			results[idx] = result{entry: entry, ok: err == nil, latency: lat}
		}(i, e)
	}
	wg.Wait()

	fmt.Printf("%-20s  %-12s  %-10s  %s\n", "NAME", "STATUS", "LATENCY", "PEER ID")
	fmt.Println("──────────────────────────────────────────────────────────────────────")
	exitCode := 0
	for _, r := range results {
		name := r.entry.FriendlyName
		if name == "" {
			name = r.entry.PeerID[:12] + "..."
		}
		status := "OFFLINE"
		lat := "-"
		if r.ok {
			status = "online"
			lat = fmt.Sprintf("%dms", r.latency.Milliseconds())
		} else {
			exitCode = 2 // at least one buddy unreachable
		}
		fmt.Printf("%-20s  %-12s  %-10s  %s\n", name, status, lat, r.entry.PeerID)
	}
	return exitCode
}

// ── runAudit ──────────────────────────────────────────────────────────────────

func runAudit(args []string) int {
	fs := flag.NewFlagSet("audit", flag.ExitOnError)
	password := fs.String("password", cfg.Password, "Keystore password (required)")
	storeDir := fs.String("store", storage.DefaultStorePath(), "Shard store to audit")
	_ = fs.Parse(args)

	if *password == "" {
		fmt.Fprintln(os.Stderr, "audit: --password is required")
		return 1
	}

	ks := openOrCreateKeystore(*password)
	masterKey := ks.MasterKey()

	st, err := storage.New(*storeDir)
	if err != nil {
		log.Printf("audit: open store: %v", err)
		return 1
	}

	fileIDs, err := st.ListFiles()
	if err != nil {
		log.Printf("audit: list files: %v", err)
		return 1
	}

	mf := openManifest(masterKey)
	if err := mf.Load(); err != nil {
		log.Printf("audit: load manifest: %v", err)
		return 1
	}

	var checked, valid, corrupted, orphaned int

	for _, fileID := range fileIDs {
		entry := mf.Get(fileID)

		// Try shards 0..TotalShards-1; scan up to a generous ceiling if
		// not in manifest (orphaned file check).
		maxShards := 8
		if entry != nil {
			maxShards = entry.Scheme.DataShards + entry.Scheme.ParityShards
		}

		for idx := 0; idx < maxShards; idx++ {
			blob, err := st.Get(fileID, idx)
			if err != nil {
				break // no more shards for this fileID
			}
			checked++

			if entry == nil {
				orphaned++
				continue
			}

			// Derive the per-shard key the same way the backup did.
			hashBytes, err := hexToHash(entry.Hash)
			if err != nil {
				log.Printf("[audit] bad hash in manifest for %s: %v", fileID, err)
				corrupted++
				continue
			}
			fileKey, err := bbcrypto.DeriveFileKey(masterKey, hashBytes)
			if err != nil {
				log.Printf("[audit] key derivation for %s: %v", fileID, err)
				corrupted++
				continue
			}
			_, decErr := bbcrypto.DecryptShard(fileKey, idx, blob)
			if decErr != nil {
				corrupted++
				log.Printf("[audit] CORRUPTED shard %s/%d: %v", fileID, idx, decErr)
			} else {
				valid++
			}
		}
	}

	fmt.Println("Audit complete")
	fmt.Printf("  Shards checked  : %d\n", checked)
	fmt.Printf("  Valid           : %d\n", valid)
	fmt.Printf("  Corrupted       : %d  (AES-GCM tag mismatch)\n", corrupted)
	fmt.Printf("  Orphaned        : %d  (in store but not in manifest)\n", orphaned)

	if corrupted > 0 {
		fmt.Fprintln(os.Stderr, "WARNING: corruption detected — run 'cerclbackup scrub' to attempt recovery.")
		return 1
	}
	return 0
}

// ── runExport ─────────────────────────────────────────────────────────────────

func runExport(args []string) int {
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	filePath := fs.String("file", "", "File path to export (required)")
	ver      := fs.Int("version", 0, "Version to export (0 = latest)")
	out      := fs.String("out", "", "Output .cbk file (default: <name>_v<N>_<date>.cbk)")
	password := fs.String("password", cfg.Password, "Keystore password (required)")
	storeDir := fs.String("store", storage.DefaultStorePath(), "Shard store")
	_ = fs.Parse(args)

	if *filePath == "" || *password == "" {
		fmt.Fprintln(os.Stderr, "export: --file and --password are required")
		return 1
	}

	ks := openOrCreateKeystore(*password)
	mf := openManifest(ks.MasterKey())
	if err := mf.Load(); err != nil {
		log.Printf("export: load manifest: %v", err)
		return 1
	}

	var entry *protocol.ManifestEntry
	if *ver > 0 {
		for _, e := range mf.ListVersions(*filePath) {
			if e.Version == *ver {
				entry = e
				break
			}
		}
	} else {
		entry = mf.Latest(*filePath)
	}
	if entry == nil {
		log.Printf("export: %q not found in manifest", *filePath)
		return 1
	}

	st, err := storage.New(*storeDir)
	if err != nil {
		log.Printf("export: open store: %v", err)
		return 1
	}

	total := entry.Scheme.TotalShards()
	shards := make([][]byte, total)
	for i := 0; i < total; i++ {
		data, err := st.Get(entry.FileID, i)
		if err != nil {
			log.Printf("export: shard %d: %v", i, err)
			// Leave nil — RS can reconstruct if enough data shards present.
		}
		shards[i] = data
	}

	outPath := *out
	if outPath == "" {
		outPath = archive.Filename(entry)
	}

	f, err := os.Create(outPath)
	if err != nil {
		log.Printf("export: create %q: %v", outPath, err)
		return 1
	}
	defer f.Close()

	if err := archive.Write(f, entry, shards); err != nil {
		log.Printf("export: write archive: %v", err)
		return 1
	}

	fmt.Printf("Exported: %s\n", outPath)
	fmt.Printf("  File   : %s\n", entry.Path)
	fmt.Printf("  Version: %d  (backed %s)\n", entry.Version, entry.BackedAt.Format("2006-01-02 15:04"))
	fmt.Printf("  Shards : %d data + %d parity\n", entry.Scheme.DataShards, entry.Scheme.ParityShards)
	return 0
}

// ── runImport ─────────────────────────────────────────────────────────────────

func runImport(args []string) int {
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	cbk      := fs.String("file", "", ".cbk archive to import (required)")
	password := fs.String("password", cfg.Password, "Keystore password (required)")
	storeDir := fs.String("store", storage.DefaultStorePath(), "Shard store")
	_ = fs.Parse(args)

	if *cbk == "" || *password == "" {
		fmt.Fprintln(os.Stderr, "import: --file and --password are required")
		return 1
	}

	f, err := os.Open(*cbk)
	if err != nil {
		log.Printf("import: open %q: %v", *cbk, err)
		return 1
	}
	defer f.Close()

	entry, shards, err := archive.Read(f)
	if err != nil {
		log.Printf("import: read archive: %v", err)
		return 1
	}

	st, err := storage.New(*storeDir)
	if err != nil {
		log.Printf("import: open store: %v", err)
		return 1
	}

	for i, data := range shards {
		if len(data) == 0 {
			continue
		}
		isParity := i >= entry.Scheme.DataShards
		if err := st.Put(entry.FileID, i, isParity, data); err != nil {
			log.Printf("import: store shard %d: %v", i, err)
			return 1
		}
	}

	ks := openOrCreateKeystore(*password)
	mf := openManifest(ks.MasterKey())
	if err := mf.Load(); err != nil {
		log.Printf("import: load manifest: %v", err)
		return 1
	}

	// Add to manifest only if this FileID isn't already present.
	if mf.Get(entry.FileID) == nil {
		mf.ImportEntry(entry)
		if err := mf.Save(); err != nil {
			log.Printf("import: save manifest: %v", err)
			return 1
		}
	}

	fmt.Printf("Imported: %s\n", *cbk)
	fmt.Printf("  File   : %s\n", entry.Path)
	fmt.Printf("  Version: %d\n", entry.Version)
	fmt.Printf("  FileID : %s\n", entry.FileID)
	fmt.Println("Run 'cerclbackup restore --file <path>' to recover the file.")
	return 0
}

// ── runDiff ───────────────────────────────────────────────────────────────────

func runDiff(args []string) int {
	fs := flag.NewFlagSet("diff", flag.ExitOnError)
	password := fs.String("password", cfg.Password, "Keystore password (required)")
	since    := fs.String("since", "", "Show changes since this time (RFC3339 or YYYY-MM-DD)")
	storeDir := fs.String("store", storage.DefaultStorePath(), "Shard store (for deleted detection)")
	_ = fs.Parse(args)

	if *password == "" || *since == "" {
		fmt.Fprintln(os.Stderr, "diff: --password and --since are required")
		fmt.Fprintln(os.Stderr, "  example: cerclbackup diff --since 2026-06-01 --password <pw>")
		return 1
	}
	_ = storeDir

	var cutoff time.Time
	for _, layout := range []string{time.RFC3339, "2006-01-02", "2006-01-02 15:04:05"} {
		if t, err := time.ParseInLocation(layout, *since, time.Local); err == nil {
			cutoff = t
			break
		}
	}
	if cutoff.IsZero() {
		log.Printf("diff: cannot parse --since %q (try YYYY-MM-DD or RFC3339)", *since)
		return 1
	}

	ks := openOrCreateKeystore(*password)
	mf := openManifest(ks.MasterKey())
	if err := mf.Load(); err != nil {
		log.Printf("diff: load manifest: %v", err)
		return 1
	}

	entries := mf.All()

	// Collect new/changed entries since cutoff.
	type change struct {
		path    string
		version int
		backedAt time.Time
		fileID  string
		size    int64
		kind    string // "new" or "updated"
	}
	latestBefore := make(map[string]int) // path → highest version before cutoff
	for _, e := range entries {
		t := e.BackedAt
		if t.IsZero() {
			t = e.Modified
		}
		if t.Before(cutoff) && e.Version > latestBefore[e.Path] {
			latestBefore[e.Path] = e.Version
		}
	}

	var changes []change
	for _, e := range entries {
		t := e.BackedAt
		if t.IsZero() {
			t = e.Modified
		}
		if !t.After(cutoff) {
			continue
		}
		kind := "updated"
		if latestBefore[e.Path] == 0 {
			kind = "new"
		}
		changes = append(changes, change{
			path:    e.Path,
			version: e.Version,
			backedAt: t,
			fileID:  e.FileID,
			size:    e.Size,
			kind:    kind,
		})
	}

	if len(changes) == 0 {
		fmt.Printf("No changes since %s.\n", cutoff.Format("2006-01-02 15:04"))
		return 0
	}

	fmt.Printf("Changes since %s\n", cutoff.Format("2006-01-02 15:04"))
	fmt.Printf("%-8s  %-4s  %-26s  %-10s  %s\n", "KIND", "VER", "BACKED AT", "SIZE", "PATH")
	fmt.Println("─────────────────────────────────────────────────────────────────────────────")
	for _, c := range changes {
		fmt.Printf("%-8s  %-4d  %-26s  %-10s  %s\n",
			c.kind, c.version,
			c.backedAt.Format("2006-01-02 15:04:05"),
			formatBytes(c.size),
			c.path)
	}
	return 0
}

// ── runDoctor ─────────────────────────────────────────────────────────────────

func runDoctor(args []string) int {
	fs := flag.NewFlagSet("doctor", flag.ExitOnError)
	password     := fs.String("password", cfg.Password, "Keystore password (required)")
	storeDir     := fs.String("store", storage.DefaultStorePath(), "Shard store")
	checkBuddies := fs.Bool("check-buddies", true, "Probe buddy connectivity")
	maxAge       := fs.Duration("max-age", 25*time.Hour, "Warn if last backup is older than this")
	_ = fs.Parse(args)

	if *password == "" {
		fmt.Fprintln(os.Stderr, "doctor: --password is required")
		return 1
	}

	type check struct {
		name string
		ok   bool
		msg  string
	}
	var checks []check
	allOK := true

	add := func(name string, ok bool, msg string) {
		checks = append(checks, check{name, ok, msg})
		if !ok {
			allOK = false
		}
	}

	// 1. Keystore
	ks, err := openKeystore(*password)
	if err != nil {
		add("keystore", false, fmt.Sprintf("cannot open: %v", err))
	} else {
		add("keystore", true, "opened OK")
	}

	// 2. Peer identity
	var privKey libp2pcrypto.PrivKey
	if ks != nil {
		privKey, err = p2pmod.EnsurePeerIdentity(ks, *password)
		if err != nil {
			add("peer identity", false, fmt.Sprintf("%v", err))
		} else {
			pid, _ := peer.IDFromPrivateKey(privKey)
			add("peer identity", true, pid.String()[:20]+"…")
		}
	}

	// 3. Store writable
	st, err := storage.New(*storeDir)
	if err != nil {
		add("shard store", false, fmt.Sprintf("cannot open %s: %v", *storeDir, err))
	} else {
		fileIDs, err := st.ListFiles()
		if err != nil {
			add("shard store", false, fmt.Sprintf("list error: %v", err))
		} else {
			add("shard store", true, fmt.Sprintf("%s — %d file(s) stored", *storeDir, len(fileIDs)))
		}
	}

	// 4. Manifest
	var mf *manifest.Manifest
	if ks != nil {
		mf = openManifest(ks.MasterKey())
		if err := mf.Load(); err != nil {
			add("manifest", false, fmt.Sprintf("load error: %v", err))
			mf = nil
		} else {
			entries := mf.All()
			add("manifest", true, fmt.Sprintf("%d version(s) tracked", len(entries)))
		}
	}

	// 5. Last backup age
	if ks != nil {
		cfgDir, _ := cerclConfigDir()
		st2, err := traystatus.Read(cfgDir)
		if err != nil || st2.LastBackupAt.IsZero() {
			add("last backup", false, "no backup recorded yet")
		} else {
			age := time.Since(st2.LastBackupAt)
			msg := fmt.Sprintf("%s ago — %s", formatAge(age), st2.LastFile)
			add("last backup", age <= *maxAge, msg)
		}
	}

	// 6. Buddy connectivity
	if *checkBuddies && ks != nil {
		reg, err := openRegistry(ks)
		if err != nil {
			add("buddies", false, fmt.Sprintf("registry: %v", err))
		} else {
			buddies := reg.List()
			if len(buddies) == 0 {
				add("buddies", false, "no buddies registered")
			} else if privKey != nil {
				h, err := p2pmod.NewHost(privKey, 0)
				if err == nil {
					defer h.Close()
					reachable := 0
					for _, b := range buddies {
						pid, err := peer.Decode(b.PeerID)
						if err != nil {
							continue
						}
						addrs := make([]multiaddr.Multiaddr, 0)
						for _, a := range b.Addrs {
							if ma, err := multiaddr.NewMultiaddr(a); err == nil {
								addrs = append(addrs, ma)
							}
						}
						ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
						if err := h.Connect(ctx, peer.AddrInfo{ID: pid, Addrs: addrs}); err == nil {
							reachable++
						}
						cancel()
					}
					ok := reachable > 0
					add("buddies", ok, fmt.Sprintf("%d/%d reachable", reachable, len(buddies)))
				} else {
					add("buddies", false, fmt.Sprintf("host: %v", err))
				}
			}
		}
	}

	// 7. Disk space
	checkDir := *storeDir
	if free, ok := diskFreeBytes(checkDir); ok {
		add("disk space", free > 100*1024*1024,
			fmt.Sprintf("%s free in %s", formatBytes(int64(free)), checkDir))
	}

	// ── Print results ──────────────────────────────────────────────────────────
	fmt.Printf("CerclBackup %s — doctor\n\n", version.AppVersion)
	for _, c := range checks {
		mark := "✓"
		if !c.ok {
			mark = "✗"
		}
		fmt.Printf("  %s  %-20s  %s\n", mark, c.name, c.msg)
	}
	fmt.Println()
	if allOK {
		fmt.Println("All checks passed.")
		return 0
	}
	fmt.Fprintln(os.Stderr, "One or more checks failed.")
	return 1
}
// Falls back to plain line read when running under a test harness
// that is not a real TTY.
func promptPassword(prompt string) (string, error) {
	fmt.Print(prompt)
	// Try syscall-level no-echo read; fall back to plain line if not a TTY.
	pw, err := readPassword()
	fmt.Println()
	return pw, err
}

// ── runPrune ────────────────────────────────────────────────────────────────

func runPrune(args []string) int {
	fs := flag.NewFlagSet("prune", flag.ExitOnError)
	password    := fs.String("password", cfg.Password, "Keystore password (required)")
	keepAll     := fs.Int("keep-all-days", 30, "Keep every version within this many days")
	keepWeekly  := fs.Int("keep-weekly-days", 90, "Keep one version/week within this many days")
	maxVersions := fs.Int("max-versions", 50, "Hard cap: max versions per file path")
	dryRun      := fs.Bool("dry-run", false, "Show what would be pruned without deleting")
	storeDir    := fs.String("store", storage.DefaultStorePath(), "Local shard store")
	_ = fs.Parse(args)

	if *password == "" {
		fmt.Fprintln(os.Stderr, "prune: --password is required")
		return 1
	}

	ks := openOrCreateKeystore(*password)
	mf := openManifest(ks.MasterKey())
	if err := mf.Load(); err != nil {
		log.Printf("prune: load manifest: %v", err)
		return 1
	}

	policy := manifest.RetentionPolicy{
		KeepAllDays:    *keepAll,
		KeepWeeklyDays: *keepWeekly,
		MaxVersions:    *maxVersions,
	}

	pruned := mf.PruneVersions(policy)
	if len(pruned) == 0 {
		fmt.Println("Nothing to prune.")
		return 0
	}

	if *dryRun {
		fmt.Printf("Would prune %d shard set(s):\n", len(pruned))
		for _, id := range pruned {
			fmt.Printf("  %s\n", id)
		}
		return 0
	}

	st, err := storage.New(*storeDir)
	if err != nil {
		log.Printf("prune: open store: %v", err)
		return 1
	}

	deleted := 0
	for _, fileID := range pruned {
		if err := st.Delete(fileID); err != nil && !os.IsNotExist(err) {
			log.Printf("prune: delete %s: %v", fileID, err)
		} else {
			deleted++
		}
	}

	if err := mf.Save(); err != nil {
		log.Printf("prune: save manifest: %v", err)
		return 1
	}

	fmt.Printf("Pruned %d version(s), freed %d shard set(s) from store.\n", len(pruned), deleted)
	return 0
}

// ── runStorage ───────────────────────────────────────────────────────────────

func runStorage(args []string) int {
	fs := flag.NewFlagSet("storage", flag.ExitOnError)
	password := fs.String("password", cfg.Password, "Keystore password (required)")
	storeDir := fs.String("store", storage.DefaultStorePath(), "Local shard store")
	_ = fs.Parse(args)

	if *password == "" {
		fmt.Fprintln(os.Stderr, "storage: --password is required")
		return 1
	}

	ks := openOrCreateKeystore(*password)
	mf := openManifest(ks.MasterKey())
	if err := mf.Load(); err != nil {
		log.Printf("storage: load manifest: %v", err)
		return 1
	}

	entries := mf.All()

	// Aggregate per-path stats.
	type pathStat struct {
		versions int
		latest   int64
	}
	byPath := make(map[string]*pathStat)
	var totalLogical int64
	for _, e := range entries {
		s := byPath[e.Path]
		if s == nil {
			s = &pathStat{}
			byPath[e.Path] = s
		}
		s.versions++
		if e.Version == 0 || func() bool {
			lat := mf.Latest(e.Path)
			return lat != nil && lat.FileID == e.FileID
		}() {
			s.latest = e.Size
			totalLogical += e.Size
		}
	}

	// Measure on-disk shard store footprint.
	var diskBytes int64
	filepath.WalkDir(*storeDir, func(_ string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			diskBytes += info.Size()
		}
		return nil
	})

	// Count files with multiple versions.
	multiVersion := 0
	for _, s := range byPath {
		if s.versions > 1 {
			multiVersion++
		}
	}

	fmt.Printf("Manifest\n")
	fmt.Printf("  Files tracked (unique paths) : %d\n", len(byPath))
	fmt.Printf("  Total versions               : %d\n", len(entries))
	fmt.Printf("  Files with >1 version        : %d\n", multiVersion)
	fmt.Printf("  Logical size (latest only)   : %s\n", formatBytes(totalLogical))
	fmt.Printf("\nLocal shard store (%s)\n", *storeDir)
	fmt.Printf("  On-disk usage                : %s\n", formatBytes(diskBytes))
	if totalLogical > 0 {
		ratio := float64(diskBytes) / float64(totalLogical)
		fmt.Printf("  Storage amplification        : %.2fx  (RS+encryption overhead)\n", ratio)
	}
	return 0
}

func formatAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

// ── runScrub ─────────────────────────────────────────────────────────────────

func runScrub(args []string) int {
	fs := flag.NewFlagSet("scrub", flag.ExitOnError)
	password := fs.String("password", cfg.Password, "Keystore password (required)")
	_ = fs.Parse(args)

	if *password == "" {
		fmt.Fprintln(os.Stderr, "scrub: --password is required")
		return 1
	}

	ks := openOrCreateKeystore(*password)
	cfgDir, _ := cerclConfigDir()
	shardDir := filepath.Join(cfgDir, "shards")
	bs := buddy.NewStore(shardDir)

	privKey, err := p2pmod.EnsurePeerIdentity(ks, *password)
	if err != nil {
		log.Printf("scrub: peer identity: %v", err)
		return 1
	}
	h, err := p2pmod.NewHost(privKey, 0)
	if err != nil {
		log.Printf("scrub: host: %v", err)
		return 1
	}
	defer h.Close()

	reg, err := openRegistry(ks)
	if err != nil {
		log.Printf("scrub: registry: %v", err)
		return 1
	}

	mgr := scrubpkg.New(bs, h, reg)
	fmt.Println("Running scrub pass...")
	r, err := mgr.RunOnce(context.Background())
	if err != nil {
		log.Printf("scrub: %v", err)
		return 1
	}

	fmt.Printf("Scrub complete\n")
	fmt.Printf("  Checked   : %d shards\n", r.Checked)
	fmt.Printf("  Healthy   : %d\n", r.OK)
	fmt.Printf("  Corrupted : %d\n", r.Corrupted)
	fmt.Printf("  Revived   : %d\n", r.Revived)
	fmt.Printf("  Failed    : %d\n", r.Failed)

	if r.Failed > 0 {
		fmt.Fprintln(os.Stderr, "WARNING: some shards could not be recovered.")
		return 1
	}
	return 0
}

// runWatch monitors a directory tree and backs up each file when it settles.
// It runs until interrupted (SIGINT/SIGTERM or Ctrl-C).
func runWatch(args []string) {
	fs := flag.NewFlagSet("watch", flag.ExitOnError)
	srcDir   := fs.String("src", cfg.Src, "Directory to monitor (required)")
	storeDir := fs.String("store", storage.DefaultStorePath(), "Store directory")
	password := fs.String("password", cfg.Password, "Encryption password (required)")
	buddies  := fs.Int("buddies", 1, "Reed-Solomon parity shards")
	debounce := fs.Duration("debounce", 3*time.Second, "Quiet period before backup fires")
	excl      := fs.String("exclude", ".git,node_modules,*.tmp,*.swp", "Comma-separated glob patterns to skip")
	autoPrune := fs.Bool("auto-prune", cfg.AutoPrune, "Apply default retention policy after each backup (default on)")
	_ = fs.Parse(args)

	if *srcDir == "" || *password == "" {
		fs.Usage()
		os.Exit(1)
	}

	ef, err := bbexclude.Parse(*excl)
	if err != nil {
		log.Fatalf("[watch] --exclude: %v", err)
	}

	// Pre-flight: open keystore once so bad passwords fail fast.
	ks := openOrCreateKeystore(*password)
	_ = ks.MasterKey()

	log.Printf("[watch] monitoring %s (debounce %s, exclude %q)", *srcDir, *debounce, *excl)

	var watchedCount int64
	w, err := watcher.NewWithDebounce(*srcDir, *debounce, func(path string) {
		if ef.Match(path) {
			return
		}
		n := atomic.AddInt64(&watchedCount, 1)
		log.Printf("[watch] file %d: %s", n, path)
		backupArgs := []string{
			"--src", path,
			"--store", *storeDir,
			"--password", *password,
			"--buddies", fmt.Sprintf("%d", *buddies),
		}
		if *autoPrune {
			backupArgs = append(backupArgs, "--auto-prune")
		}
		runBackup(backupArgs)
	})
	if err != nil {
		log.Fatalf("[watch] init: %v", err)
	}

	// Handle SIGINT / SIGTERM for clean shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		<-sigCh
		log.Println("[watch] shutting down...")
		w.Stop()
	}()

	if err := w.Start(); err != nil {
		log.Fatalf("[watch] %v", err)
	}
}

// runCircle handles: circle add / circle list / circle rm
func runCircle(args []string) int {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: cerclbackup circle <add|list|rm> [flags]\n")
		return 1
	}
	sub := args[0]
	rest := args[1:]

	switch sub {
	case "add":
		fs := flag.NewFlagSet("circle add", flag.ExitOnError)
		name := fs.String("name", "", "Circle name (required)")
		scheme := fs.String("scheme", "3/2", "RS scheme data/parity")
		password := fs.String("password", cfg.Password, "Keystore password (required)")
		fs.Parse(rest)
		if *name == "" || *password == "" {
			fmt.Fprintln(os.Stderr, "circle add: --name and --password are required")
			return 1
		}
		ks, err := openKeystore(*password)
		if err != nil {
			log.Printf("circle add: %v", err)
			return 1
		}
		mgr := circle.NewManager(ks, *password)
		c, err := mgr.Add(*name, *scheme)
		if err != nil {
			log.Printf("circle add: %v", err)
			return 1
		}
		fmt.Printf("Circle added: %s (id=%s scheme=%s)\n", c.Name, c.ID, c.Scheme)
		return 0

	case "list":
		fs := flag.NewFlagSet("circle list", flag.ExitOnError)
		password := fs.String("password", cfg.Password, "Keystore password (required)")
		fs.Parse(rest)
		if *password == "" {
			fmt.Fprintln(os.Stderr, "circle list: --password is required")
			return 1
		}
		ks, err := openKeystore(*password)
		if err != nil {
			log.Printf("circle list: %v", err)
			return 1
		}
		mgr := circle.NewManager(ks, *password)
		circles, err := mgr.List()
		if err != nil {
			log.Printf("circle list: %v", err)
			return 1
		}
		if len(circles) == 0 {
			fmt.Println("No circles configured.")
			return 0
		}
		fmt.Printf("%-24s %-36s %-6s %s\n", "NAME", "ID", "SCHEME", "CREATED")
		for _, c := range circles {
			fmt.Printf("%-24s %-36s %-6s %s\n", c.Name, c.ID, c.Scheme, c.CreatedAt.Format("2006-01-02"))
		}
		return 0

	case "rm":
		fs := flag.NewFlagSet("circle rm", flag.ExitOnError)
		name := fs.String("name", "", "Circle name to remove (required)")
		confirm := fs.String("confirm-name", "", "Must match --name to confirm deletion")
		password := fs.String("password", cfg.Password, "Keystore password (required)")
		fs.Parse(rest)
		if *name == "" || *password == "" {
			fmt.Fprintln(os.Stderr, "circle rm: --name and --password are required")
			return 1
		}
		if *confirm != *name {
			fmt.Fprintf(os.Stderr, "circle rm: --confirm-name must equal %q\n", *name)
			return 1
		}
		ks, err := openKeystore(*password)
		if err != nil {
			log.Printf("circle rm: %v", err)
			return 1
		}
		mgr := circle.NewManager(ks, *password)
		if err := mgr.Remove(*name); err != nil {
			log.Printf("circle rm: %v", err)
			return 1
		}
		fmt.Printf("Circle %q removed.\n", *name)
		return 0

	default:
		fmt.Fprintf(os.Stderr, "Unknown circle sub-command: %q\n", sub)
		return 1
	}
}

// runVersions lists all backed-up versions of a file.
func runVersions(args []string) int {
	fs := flag.NewFlagSet("versions", flag.ExitOnError)
	filePath := fs.String("file", "", "Path of the backed-up file (required)")
	password := fs.String("password", cfg.Password, "Keystore password (required)")
	fs.Parse(args)
	if *filePath == "" || *password == "" {
		fmt.Fprintln(os.Stderr, "versions: --file and --password are required")
		return 1
	}
	ks, err := openKeystore(*password)
	if err != nil {
		log.Printf("versions: %v", err)
		return 1
	}
	masterKey := ks.MasterKey()
	manifestPath := manifest.DefaultManifestPath()
	m := manifest.New(manifestPath, masterKey)
	if err := m.Load(); err != nil {
		log.Printf("versions: load manifest: %v", err)
		return 1
	}
	versions := m.ListVersions(*filePath)
	if len(versions) == 0 {
		fmt.Printf("No versions found for: %s\n", *filePath)
		return 0
	}
	fmt.Printf("%-4s %-26s %-64s %s\n", "VER", "BACKED AT", "FILE ID", "HASH")
	for _, v := range versions {
		backedAt := v.BackedAt.Format("2006-01-02 15:04:05 UTC")
		if v.BackedAt.IsZero() {
			backedAt = "(legacy)"
		}
		fmt.Printf("%-4d %-26s %-64s %s\n", v.Version, backedAt, v.FileID, v.Hash[:16]+"...")
	}
	return 0
}

// ---------------------------------------------------------------------------
// passwd -- change keystore password
// ---------------------------------------------------------------------------

// runSetPassword stores the backup password in the OS keyring (Windows
// Credential Manager, macOS Keychain, Linux Secret Service).  It is intended
// to be opened in a terminal by the tray app so the password never has to be
// typed on the command line or stored in a plain-text file.
func runSetPassword(args []string) int {
	fs := flag.NewFlagSet("set-password", flag.ExitOnError)
	del := fs.Bool("delete", false, "Remove the stored password from the credential store")
	_ = fs.Parse(args)

	if *del {
		if err := keyring.Delete(); err != nil {
			fmt.Fprintln(os.Stderr, "error: could not delete from credential store:", err)
			return 1
		}
		fmt.Println("Password removed from credential store.")
		return 0
	}

	// When stdin is not a terminal (e.g. piped in a test), promptPassword falls
	// back to reading a plain line — no echo suppression needed.
	pass, err := promptPassword("Enter CerclBackup password: ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: could not read password:", err)
		return 1
	}
	if pass == "" {
		fmt.Fprintln(os.Stderr, "error: password cannot be empty")
		return 1
	}
	if err := keyring.Set(pass); err != nil {
		fmt.Fprintln(os.Stderr, "error: could not save to credential store:", err)
		fmt.Fprintln(os.Stderr, "  Tip: set the CERCLBACKUP_PASSWORD environment variable instead.")
		return 1
	}
	fmt.Println("Password saved to credential store.")
	fmt.Println("The tray app will use it automatically on next backup cycle.")
	return 0
}

func runPasswd(args []string) int {
	fs := flag.NewFlagSet("passwd", flag.ExitOnError)
	oldFlag := fs.String("old", "", "Current password (prompted if empty)")
	newFlag := fs.String("new", "", "New password (prompted if empty)")
	_ = fs.Parse(args)

	oldPwd := *oldFlag
	if oldPwd == "" {
		if p := os.Getenv("CERCLBACKUP_PASSWORD"); p != "" {
			oldPwd = p
		} else {
			fmt.Fprint(os.Stderr, "Current password: ")
			b, err := readPassword()
			fmt.Fprintln(os.Stderr)
			if err != nil {
				log.Printf("passwd: read old: %v", err)
				return 1
			}
			oldPwd = b
		}
	}

	newPwd := *newFlag
	if newPwd == "" {
		fmt.Fprint(os.Stderr, "New password: ")
		b, err := readPassword()
		fmt.Fprintln(os.Stderr)
		if err != nil {
			log.Printf("passwd: read new: %v", err)
			return 1
		}
		newPwd = b

		fmt.Fprint(os.Stderr, "Confirm new password: ")
		b2, err := readPassword()
		fmt.Fprintln(os.Stderr)
		if err != nil {
			log.Printf("passwd: read confirm: %v", err)
			return 1
		}
		if b2 != newPwd {
			fmt.Fprintln(os.Stderr, "passwd: passwords do not match")
			return 1
		}
	}

	if newPwd == "" {
		fmt.Fprintln(os.Stderr, "passwd: new password cannot be empty")
		return 1
	}

	ks, err := openKeystore(oldPwd)
	if err != nil {
		log.Printf("passwd: wrong password or corrupted keystore: %v", err)
		return 1
	}

	if err := ks.Save(newPwd); err != nil {
		log.Printf("passwd: save: %v", err)
		return 1
	}

	fmt.Println("Keystore password changed successfully.")
	fmt.Println("Update CERCLBACKUP_PASSWORD or your config.yaml if applicable.")
	return 0
}

// ---------------------------------------------------------------------------
// config -- show / init config file
// ---------------------------------------------------------------------------

func runConfig(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: cerclbackup config <show|init>")
		return 1
	}
	switch args[0] {
	case "show":
		path := cerclConfig.DefaultPath()
		loaded := cerclConfig.LoadFrom(path)
		fmt.Printf("Config file: %s\n\n", path)
		fmt.Printf("password    : %s\n", maskPassword(loaded.Password))
		fmt.Printf("src         : %s\n", loaded.Src)
		fmt.Printf("exclude     : %s\n", loaded.Exclude)
		fmt.Printf("upload_kbps : %d\n", loaded.UploadKbps)
		fmt.Printf("health_addr : %s\n", loaded.HealthAddr)
		fmt.Printf("port        : %d\n", loaded.Port)
		fmt.Printf("debounce    : %s\n", loaded.Debounce)
		fmt.Printf("auto_prune  : %v\n", loaded.AutoPrune)
		fmt.Printf("store_dir   : %s\n", loaded.StoreDir)
		return 0
	case "init":
		path := cerclConfig.DefaultPath()
		if _, err := os.Stat(path); err == nil {
			fmt.Fprintf(os.Stderr, "config init: %s already exists (delete it first to regenerate)\n", path)
			return 1
		}
		if err := cerclConfig.WriteTemplate(path); err != nil {
			log.Printf("config init: %v", err)
			return 1
		}
		fmt.Printf("Sample config written to %s\n", path)
		fmt.Println("Edit it to set your defaults, then uncomment the relevant lines.")
		return 0
	default:
		fmt.Fprintln(os.Stderr, "Usage: cerclbackup config <show|init>")
		return 1
	}
}

func maskPassword(p string) string {
	if p == "" {
		return "(not set)"
	}
	return "***"
}
