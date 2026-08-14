package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/cerclbackup/cerclbackup/internal/buddy"
	"github.com/cerclbackup/cerclbackup/internal/chunker"
	"github.com/cerclbackup/cerclbackup/internal/codec"
	bbcompress "github.com/cerclbackup/cerclbackup/internal/compress"
	bbcrypto "github.com/cerclbackup/cerclbackup/internal/crypto"
	p2pmod "github.com/cerclbackup/cerclbackup/internal/p2p"
	"github.com/cerclbackup/cerclbackup/pkg/protocol"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
)

// RestoreParams selects which manifest entry to restore and where to write it.
type RestoreParams struct {
	StoreDir string
	Password string
	Out      string // required, output file path

	// Exactly one of FileID, or (FilePath [+ Version]), must be set.
	FileID   string // legacy manifest UUID lookup
	FilePath string // original path; looks up latest version, or a specific Version
	Version  int    // 0 = latest, requires FilePath

	Progress func(line string)
}

// RestoreResult reports the manifest entry that was restored.
type RestoreResult struct {
	Entry           *protocol.ManifestEntry
	IntegrityPassed bool
}

func (p *RestoreParams) log(format string, args ...any) {
	if p.Progress != nil {
		p.Progress(fmt.Sprintf(format, args...))
	}
}

// Restore reconstructs a single file from local + buddy shards and writes
// it to params.Out.
func Restore(params RestoreParams) (*RestoreResult, error) {
	if params.Out == "" || params.Password == "" {
		return nil, fmt.Errorf("out and password are required")
	}
	if params.FileID == "" && params.FilePath == "" {
		return nil, fmt.Errorf("one of FileID or FilePath is required")
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

	var entry *protocol.ManifestEntry
	switch {
	case params.FileID != "":
		entry = mf.Get(params.FileID)
		if entry == nil {
			return nil, fmt.Errorf("file-id %q not found in manifest", params.FileID)
		}
	case params.Version > 0:
		for _, e := range mf.ListVersions(params.FilePath) {
			if e.Version == params.Version {
				entry = e
				break
			}
		}
		if entry == nil {
			return nil, fmt.Errorf("%q version %d not found in manifest", params.FilePath, params.Version)
		}
	default:
		entry = mf.Latest(params.FilePath)
		if entry == nil {
			return nil, fmt.Errorf("%q not found in manifest", params.FilePath)
		}
		params.log("using latest version %d (backed %s)", entry.Version, entry.BackedAt.Format("2006-01-02 15:04:05"))
	}
	params.log("restoring %q (%d bytes, scheme %d/%d) ...", entry.Path, entry.Size, entry.Scheme.DataShards, entry.Scheme.ParityShards)

	hashBytes, err := hexToHash(entry.Hash)
	if err != nil {
		return nil, err
	}
	fileKey, err := bbcrypto.DeriveFileKey(masterKey, hashBytes)
	if err != nil {
		return nil, err
	}
	storeFileID := fileIDFromHash(hashBytes)

	var restoreHost host.Host
	var buddyReg *buddy.Registry
	restoreCtx := context.Background()
	if privKey, err := p2pmod.EnsurePeerIdentity(ks, params.Password); err == nil {
		if rh, err := p2pmod.NewHost(privKey, 0); err == nil {
			restoreHost = rh
			defer rh.Close()
			if reg, err := OpenRegistry(ks); err == nil {
				buddyReg = reg
				for _, e := range reg.List() {
					pID, err := peer.Decode(e.PeerID)
					if err != nil {
						continue
					}
					var addrs []multiaddr.Multiaddr
					for _, a := range e.Addrs {
						if ma, _ := multiaddr.NewMultiaddr(a); ma != nil {
							addrs = append(addrs, ma)
						}
					}
					_ = rh.Connect(restoreCtx, peer.AddrInfo{ID: pID, Addrs: addrs})
				}
				params.log("P2P host ready, connected to %d buddy addr(s)", len(reg.List()))
			}
		}
	}
	ownPeerID := ""
	if restoreHost != nil {
		ownPeerID = restoreHost.ID().String()
	}

	enc, err := codec.NewEncoder(entry.Scheme)
	if err != nil {
		return nil, err
	}

	shardsPerChunk := entry.Scheme.TotalShards()
	totalShards := len(entry.Shards)
	numChunks := totalShards / shardsPerChunk
	if totalShards%shardsPerChunk != 0 {
		return nil, fmt.Errorf("shard count %d not divisible by %d", totalShards, shardsPerChunk)
	}

	outFile, err := os.Create(params.Out)
	if err != nil {
		return nil, err
	}
	defer outFile.Close()

	verifyHasher := sha256.New()

	for ci := 0; ci < numChunks; ci++ {
		if numChunks > 1 {
			params.log("chunk %d/%d", ci+1, numChunks)
		}
		rawShards := make([][]byte, shardsPerChunk)
		for si := 0; si < shardsPerChunk; si++ {
			globalShardIdx := ci*shardsPerChunk + si
			loc := entry.Shards[globalShardIdx]

			ciphertext, err := store.Get(storeFileID, loc.ShardIndex)
			if err != nil {
				if restoreHost != nil && buddyReg != nil {
					if fetched, ok := tryFetchFromBuddies(restoreCtx, restoreHost, buddyReg, ownPeerID, storeFileID, loc.ShardIndex); ok {
						params.log("fetched shard %d from buddy", globalShardIdx)
						ciphertext = fetched
						err = nil
					}
				}
				if err != nil {
					params.log("shard %d missing, will reconstruct", globalShardIdx)
					rawShards[si] = nil
					continue
				}
			}

			plaintext, err := bbcrypto.DecryptShard(fileKey, loc.ShardIndex, ciphertext)
			if err != nil {
				params.log("shard %d decrypt error: %v -- treating as missing", globalShardIdx, err)
				rawShards[si] = nil
				continue
			}
			rawShards[si] = plaintext
		}

		chunkData, err := enc.MergeShardToChunk(rawShards)
		if err != nil {
			return nil, err
		}

		if entry.Compressed {
			chunkData, err = bbcompress.Decompress(chunkData)
			if err != nil {
				return nil, fmt.Errorf("decompress chunk %d: %w", ci, err)
			}
		} else {
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
			return nil, fmt.Errorf("write chunk %d: %w", ci, err)
		}
	}

	result := &RestoreResult{Entry: entry}
	if entry.Hash != "" {
		var gotHash [32]byte
		copy(gotHash[:], verifyHasher.Sum(nil))
		gotHex := hex.EncodeToString(gotHash[:])
		if gotHex != entry.Hash {
			outFile.Close()
			os.Remove(params.Out)
			return nil, fmt.Errorf("INTEGRITY CHECK FAILED: hash mismatch (corrupted data, output deleted)")
		}
		result.IntegrityPassed = true
		params.log("integrity check passed")
	}

	params.log("restored to %q", params.Out)
	return result, nil
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
