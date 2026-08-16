package api

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cerclbackup/cerclbackup/internal/chunker"
	"github.com/cerclbackup/cerclbackup/internal/codec"
	bbcompress "github.com/cerclbackup/cerclbackup/internal/compress"
	bbcrypto "github.com/cerclbackup/cerclbackup/internal/crypto"
	bbexclude "github.com/cerclbackup/cerclbackup/internal/exclude"
	"github.com/cerclbackup/cerclbackup/internal/manifdist"
	"github.com/cerclbackup/cerclbackup/internal/manifest"
	p2pmod "github.com/cerclbackup/cerclbackup/internal/p2p"
	"github.com/cerclbackup/cerclbackup/internal/storage"
	traystatus "github.com/cerclbackup/cerclbackup/internal/tray"
	"github.com/cerclbackup/cerclbackup/pkg/protocol"
	"github.com/cerclbackup/cerclbackup/pkg/wire"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
)

// BackupParams configures a Backup call.
type BackupParams struct {
	Src        string // file or directory to back up
	StoreDir   string
	Password   string
	Buddies    int    // determines the Reed-Solomon scheme
	Exclude    string // comma-separated glob patterns
	UploadKbps int    // 0 = unlimited
	AutoPrune  bool

	// Progress, if set, is called with human-readable progress lines as the
	// backup proceeds (chunking, per-file completion, buddy push status).
	// Both the CLI and the GUI use this instead of package-level logging.
	Progress func(line string)
}

// BackedUpFile describes the outcome of backing up a single file.
type BackedUpFile struct {
	Path   string
	FileID string
	Shards int
	Err    string // non-empty if this file failed; backup continues with the rest
}

// BackupResult is the structured outcome of a Backup call.
type BackupResult struct {
	Scheme          protocol.RSScheme
	Files           []BackedUpFile
	PrunedVersions  int
	PushedToBuddies int
}

func (p *BackupParams) log(format string, args ...any) {
	if p.Progress != nil {
		p.Progress(fmt.Sprintf(format, args...))
	}
}

// Backup walks params.Src (a file or directory), encrypts and shards every
// file it finds, stores the shards locally, updates the manifest, then
// pushes shards to any known buddies.
func Backup(params BackupParams) (*BackupResult, error) {
	if params.Src == "" || params.Password == "" {
		return nil, fmt.Errorf("src and password are required")
	}

	if params.UploadKbps > 0 {
		p2pmod.SetUploadRate(params.UploadKbps * 1024)
	}

	var ef *bbexclude.Filter
	if params.Exclude != "" {
		var err error
		ef, err = bbexclude.Parse(params.Exclude)
		if err != nil {
			return nil, fmt.Errorf("exclude: %w", err)
		}
	}

	store, err := OpenStore(params.StoreDir)
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	ks, err := OpenOrCreateKeystore(params.Password)
	if err != nil {
		return nil, err
	}
	masterKey := ks.MasterKey()
	mf, err := OpenManifest(masterKey)
	if err != nil {
		return nil, err
	}

	buddies := params.Buddies
	if buddies <= 0 {
		buddies = 5
	}
	scheme, err := protocol.BestScheme(buddies)
	if err != nil {
		return nil, fmt.Errorf("%w (got buddies=%d)", err, buddies)
	}
	params.log("RS scheme: %d data / %d parity (tolerates %d buddy failures)",
		scheme.DataShards, scheme.ParityShards, scheme.ParityShards)

	result := &BackupResult{Scheme: scheme}
	var lastFile string

	walkErr := filepath.Walk(params.Src, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fi.IsDir() {
			return nil
		}
		if ef != nil && ef.Match(path) {
			params.log("skip (excluded): %s", path)
			return nil
		}
		fileID, shards, err := backupOneFile(path, fi, store, params.Password, masterKey, mf, scheme, params.log)
		if err != nil {
			result.Files = append(result.Files, BackedUpFile{Path: path, Err: err.Error()})
			return nil
		}
		result.Files = append(result.Files, BackedUpFile{Path: path, FileID: fileID, Shards: shards})
		lastFile = path
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("walk %q: %w", params.Src, walkErr)
	}

	if err := mf.Save(); err != nil {
		return nil, fmt.Errorf("manifest save: %w", err)
	}

	if params.AutoPrune {
		pruned := mf.PruneVersions(manifest.DefaultRetentionPolicy())
		if len(pruned) > 0 {
			if st2, err := OpenStore(params.StoreDir); err == nil {
				for _, id := range pruned {
					st2.Delete(id)
				}
			}
			if err := mf.Save(); err != nil {
				return nil, fmt.Errorf("manifest save after prune: %w", err)
			}
			result.PrunedVersions = len(pruned)
			params.log("auto-prune: removed %d old version(s)", len(pruned))
		}
	}

	if lastFile != "" {
		if cfgDir, err := ConfigDir(); err == nil {
			st := traystatus.Status{LastBackupAt: time.Now().UTC(), LastFile: lastFile}
			_ = traystatus.Write(cfgDir, st)
		}
	}

	if blob, err := mf.EncryptedBytes(); err == nil {
		if priv, err := p2pmod.EnsurePeerIdentity(ks, params.Password); err == nil {
			if h, err := p2pmod.NewHost(priv, 0); err == nil {
				defer h.Close()
				n := manifdist.PushToAll(context.Background(), h, h.ID().String(), blob)
				result.PushedToBuddies = n
				if n > 0 {
					params.log("manifest pushed to %d buddy/buddies", n)
				}
			}
		}
	}

	return result, nil
}

// backupOneFile chunks, Reed-Solomon encodes, encrypts and stores a single
// file, adds it to the manifest, then pushes its shards to known buddies.
func backupOneFile(src string, fi os.FileInfo, store *storage.Store, password string, masterKey []byte, mf *manifest.Manifest, scheme protocol.RSScheme, log func(string, ...any)) (fileID string, shardCount int, _ error) {
	log("chunking %q ...", src)
	chunks, err := chunker.ChunkFile(src, chunker.DefaultChunkSize)
	if err != nil {
		return "", 0, fmt.Errorf("chunk: %w", err)
	}

	fileHash := fileHashFromChunks(chunks)
	fileKey, err := bbcrypto.DeriveFileKey(masterKey, fileHash)
	if err != nil {
		return "", 0, fmt.Errorf("derive key: %w", err)
	}

	enc, err := codec.NewEncoder(scheme)
	if err != nil {
		return "", 0, fmt.Errorf("encoder: %w", err)
	}

	id := fileIDFromHash(fileHash)
	var shardLocations []protocol.ShardLocation
	shardCounter := 0

	for _, chunk := range chunks {
		chunkBytes, err := bbcompress.Compress(chunk.Data[:chunk.Size])
		if err != nil {
			return "", 0, fmt.Errorf("compress chunk %d: %w", chunk.Index, err)
		}
		rawShards, err := enc.SplitChunkToShards(chunkBytes)
		if err != nil {
			return "", 0, fmt.Errorf("RS encode chunk %d: %w", chunk.Index, err)
		}
		for si, shard := range rawShards {
			isParity := si >= scheme.DataShards
			idx := shardCounter
			shardCounter++
			ciphertext, err := bbcrypto.EncryptShard(fileKey, idx, shard)
			if err != nil {
				return "", 0, fmt.Errorf("encrypt shard: %w", err)
			}
			if err := store.Put(id, idx, isParity, ciphertext); err != nil {
				return "", 0, fmt.Errorf("store shard: %w", err)
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
		return "", 0, fmt.Errorf("manifest upsert: %w", err)
	}
	entry.Compressed = true

	log("done %s — file-id: %s  shards: %d", filepath.Base(src), entry.FileID, len(shardLocations))

	ks, err := OpenKeystore(password)
	if err == nil {
		pushToBuddies(ks, password, id, shardLocations, store, log)
	}
	return id, len(shardLocations), nil
}

// pushToBuddies delivers shards to every known buddy, enqueueing for later
// delivery via the retry queue when a buddy is unreachable.
func pushToBuddies(ks *bbcrypto.Keystore, password, fileID string, locs []protocol.ShardLocation, store *storage.Store, log func(string, ...any)) {
	privKey, err := p2pmod.EnsurePeerIdentity(ks, password)
	if err != nil {
		log("P2P identity: %v", err)
		return
	}
	h, err := p2pmod.NewHost(privKey, 0)
	if err != nil {
		log("P2P host: %v", err)
		return
	}
	defer h.Close()

	reg, err := OpenRegistry(ks)
	if err != nil {
		log("registry: %v", err)
		return
	}
	buddies := reg.List()
	if len(buddies) == 0 {
		return
	}

	cfgDir, _ := ConfigDir()
	q := p2pmod.NewQueue(filepath.Join(cfgDir, "queue.json"))
	ownerID := h.ID().String()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	for _, entry := range buddies {
		peerID, err := peer.Decode(entry.PeerID)
		if err != nil {
			continue
		}
		var addrs []multiaddr.Multiaddr
		for _, a := range entry.Addrs {
			if ma, err := multiaddr.NewMultiaddr(a); err == nil {
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
			log("buddy %s unreachable, enqueueing %d shards", entry.PeerID, len(locs))
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
				log("push shard %d to %s: %v", loc.ShardIndex, entry.PeerID, err)
			} else {
				pushed++
			}
		}
		log("pushed %d/%d shards to buddy %s", pushed, len(locs), entry.PeerID)
	}
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
	return fmt.Sprintf("%x", h[:8])
}
