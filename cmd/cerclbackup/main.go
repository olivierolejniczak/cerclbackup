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
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/cerclbackup/cerclbackup/internal/buddy"
	"github.com/cerclbackup/cerclbackup/internal/chunker"
	"github.com/cerclbackup/cerclbackup/internal/codec"
	bbcrypto "github.com/cerclbackup/cerclbackup/internal/crypto"
	"github.com/cerclbackup/cerclbackup/internal/emailinvite"
	"github.com/cerclbackup/cerclbackup/internal/identity"
	"github.com/cerclbackup/cerclbackup/internal/invite"
	"github.com/cerclbackup/cerclbackup/internal/manifest"
	p2pmod "github.com/cerclbackup/cerclbackup/internal/p2p"
	"github.com/cerclbackup/cerclbackup/internal/rebalance"
	scrubpkg "github.com/cerclbackup/cerclbackup/internal/scrub"
	"github.com/cerclbackup/cerclbackup/internal/storage"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/cerclbackup/cerclbackup/pkg/protocol"
	"github.com/cerclbackup/cerclbackup/pkg/wire"
	"github.com/multiformats/go-multiaddr"
)

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
		runBuddy(os.Args[2:])
	case "revoke":
		runRevoke(os.Args[2:])
	case "rebalance":
		runRebalance(os.Args[2:])
	case "show-phrase":
		runShowPhrase(os.Args[2:])
	case "recover":
		runRecover(os.Args[2:])
	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `CerclBackup

Commands (Phase 1 — local):
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
  join-email   --payload <file> --words "<12 words>" --password <pwd>    accept email invite`)
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

	// -- 7. Push shards to buddies (Phase 2b) -----------------------------------------------
	pushToBuddies(ks, *password, fileIDFromHash(fileHash), shardLocations, store)
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

	for ci := 0; ci < numChunks; ci++ {
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

// ---------------------------------------------------------------------------
// P2P helpers
// ---------------------------------------------------------------------------

func openKeystore(password string) (*bbcrypto.Keystore, error) {
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	ksPath := filepath.Join(cfgDir, "cerclbackup", "keystore.enc")
	ks := bbcrypto.NewKeystore(ksPath)
	if err := ks.Unlock(password); err != nil {
		return nil, fmt.Errorf("keystore unlock: %w", err)
	}
	return ks, nil
}

func openRegistry(ks *bbcrypto.Keystore) (*buddy.Registry, error) {
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	regPath := filepath.Join(cfgDir, "cerclbackup", "buddies.enc")
	return buddy.NewRegistry(regPath, ks.MasterKey())
}

func openInviteManager() *invite.Manager {
	cfgDir, _ := os.UserConfigDir()
	invPath := filepath.Join(cfgDir, "cerclbackup", "invites.json")
	return invite.NewManager(invPath)
}

// ---------------------------------------------------------------------------
// serve
// ---------------------------------------------------------------------------

func runServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	password := fs.String("password", "", "keystore password (required)")
	port := fs.Int("port", p2pmod.DefaultPort, "TCP/UDP port for libp2p")
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

	h, err := p2pmod.NewHost(privKey, *port)
	if err != nil {
		log.Fatal(err)
	}
	defer h.Close()

	reg, err := openRegistry(ks)
	if err != nil {
		log.Fatal(err)
	}

	cfgDir, _ := os.UserConfigDir()
	storeDir := filepath.Join(cfgDir, "cerclbackup", "shards")
	bs := buddy.NewStore(storeDir)
	invMgr := openInviteManager()

	p2pmod.RegisterHandlers(h, reg, bs, invMgr)

	q := p2pmod.NewQueue(filepath.Join(cfgDir, "cerclbackup", "queue.json"))
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

	fmt.Printf("CerclBackup daemon running\n")
	fmt.Printf("Peer ID : %s\n", h.ID())
	for _, a := range h.Addrs() {
		fmt.Printf("Address : %s/p2p/%s\n", a, h.ID())
	}

	<-ctx.Done()
	fmt.Println("\nShutting down.")
}

// ---------------------------------------------------------------------------
// invite
// ---------------------------------------------------------------------------

func runInvite(args []string) {
	fs := flag.NewFlagSet("invite", flag.ExitOnError)
	password := fs.String("password", "", "keystore password (required)")
	if err := fs.Parse(args); err != nil {
		log.Fatal(err)
	}
	if *password == "" {
		fs.Usage()
		os.Exit(1)
	}

	if _, err := openKeystore(*password); err != nil {
		log.Fatal(err)
	}

	invMgr := openInviteManager()
	code, err := invMgr.Generate()
	if err != nil {
		log.Fatal(err)
	}

	words := code.Words
	// split and show last 3 for verbal confirmation
	wlist := splitWords(words)
	verbally := ""
	if len(wlist) >= 3 {
		verbally = fmt.Sprintf("%s %s %s", wlist[len(wlist)-3], wlist[len(wlist)-2], wlist[len(wlist)-1])
	}

	fmt.Printf("Invite code (give to your buddy):\n\n  %s\n\n", words)
	fmt.Printf("Ask your buddy to verbally confirm the LAST 3 WORDS: %q\n", verbally)
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
	password := fs.String("password", "", "keystore password (required)")
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

func runBuddy(args []string) {
	if len(args) == 0 || args[0] != "list" {
		fmt.Fprintln(os.Stderr, "usage: cerclbackup buddy list --password <pwd>")
		os.Exit(1)
	}

	fs := flag.NewFlagSet("buddy list", flag.ExitOnError)
	password := fs.String("password", "", "keystore password (required)")
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
	password := fs.String("password", "", "keystore password (required)")
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

	cfgDir, _ := os.UserConfigDir()
	q := p2pmod.NewQueue(filepath.Join(cfgDir, "cerclbackup", "queue.json"))
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
	password := fs.String("password", "", "keystore password (required)")
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
// Phase 2g -- Recovery phrase: show-phrase / recover
// ---------------------------------------------------------------------------

func runShowPhrase(args []string) {
	fs := flag.NewFlagSet("show-phrase", flag.ExitOnError)
	password := fs.String("password", "", "Keystore password (required)")
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
	password := fs.String("password", "", "New keystore password (required)")
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
	password := fs.String("password", "", "keystore password (required)")
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
	password := fs.String("password", "", "keystore password (required)")
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
