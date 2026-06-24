// Package protocol defines shared types used across all CerclBackup components.
package protocol

import (
	"errors"
	"time"
)

// Op represents a file system operation detected by the watcher.
type Op uint32

const (
	OpCreate Op = iota
	OpWrite
	OpDelete
	OpRename
)

func (o Op) String() string {
	switch o {
	case OpCreate:
		return "CREATE"
	case OpWrite:
		return "WRITE"
	case OpDelete:
		return "DELETE"
	case OpRename:
		return "RENAME"
	default:
		return "UNKNOWN"
	}
}

// FileEvent is emitted by the Watcher when a monitored file changes.
type FileEvent struct {
	Path      string
	Operation Op
	Timestamp time.Time
}

// Chunk is a raw piece of a file produced by the Chunker.
type Chunk struct {
	// Index is the position of this chunk within the original file.
	Index int
	// Data is the raw bytes of the chunk (may be padded on the last chunk).
	Data []byte
	// Hash is the SHA-256 of Data before any encryption.
	Hash [32]byte
	// Size is the actual number of meaningful bytes (before padding).
	Size int
}

// RSScheme describes a Reed-Solomon (data shards / parity shards) configuration.
type RSScheme struct {
	DataShards   int
	ParityShards int
}

// TotalShards returns DataShards + ParityShards.
func (s RSScheme) TotalShards() int {
	return s.DataShards + s.ParityShards
}

// OverheadPct returns the storage overhead percentage vs raw data.
func (s RSScheme) OverheadPct() float64 {
	return float64(s.ParityShards) / float64(s.DataShards) * 100
}

// BestScheme returns the recommended RSScheme given the number of available
// buddies. Reed-Solomon is always mandatory: a 1/1 "mirror" scheme is never
// returned, because it would let a single buddy hold a fully reconstructible
// copy of the file — exactly the property RS is meant to prevent. Callers
// must enforce a minimum of 3 buddies/devices per circle; ErrInsufficientBuddies
// signals when that minimum isn't met.
func BestScheme(buddies int) (RSScheme, error) {
	switch {
	case buddies >= 10:
		return RSScheme{DataShards: 6, ParityShards: 4}, nil
	case buddies >= 8:
		return RSScheme{DataShards: 5, ParityShards: 3}, nil
	case buddies >= 5:
		return RSScheme{DataShards: 3, ParityShards: 2}, nil
	case buddies >= 3:
		return RSScheme{DataShards: 2, ParityShards: 1}, nil
	default:
		return RSScheme{}, ErrInsufficientBuddies
	}
}

// ErrInsufficientBuddies is returned when fewer than 3 buddies/devices are
// available, since CerclBackup never falls back to a 1/1 mirror scheme.
var ErrInsufficientBuddies = errors.New("protocol: at least 3 buddies/devices are required (no 1/1 mirror fallback)")

// EncodedShard is one Reed-Solomon shard after encoding and encryption.
type EncodedShard struct {
	// FileID is the UUID of the source file backup entry.
	FileID string
	// ShardIndex is the position among all shards (0..TotalShards-1).
	ShardIndex int
	// IsParity is true for parity shards, false for data shards.
	IsParity bool
	// Ciphertext is the AES-256-GCM encrypted shard data.
	Ciphertext []byte
	// Nonce is the GCM nonce used for this shard (12 bytes).
	Nonce []byte
	// PlaintextHash is the SHA-256 of the plaintext shard (for integrity verification).
	PlaintextHash [32]byte
	// Scheme is the RS scheme that produced this shard.
	Scheme RSScheme
	// ChunkSize is the padded size of each shard in bytes.
	ChunkSize int
}

// ManifestEntry records all metadata needed to restore one file.
type ManifestEntry struct {
	// FileID is a UUID that uniquely identifies this specific version.
	FileID string `json:"file_id"`
	// Path is the original absolute path on the owner's machine.
	Path string `json:"path"`
	// Hash is the SHA-256 of the complete original file.
	Hash string `json:"hash"`
	// Size is the original file size in bytes.
	Size int64 `json:"size"`
	// Modified is the mtime of the file at backup time.
	Modified time.Time `json:"modified"`
	// Scheme is the Reed-Solomon scheme used.
	Scheme RSScheme `json:"scheme"`
	// Shards lists the location of each encoded shard.
	Shards []ShardLocation `json:"shards"`
	// Version is the 1-based version number within this path's history.
	// Zero means "unversioned" (entry created before Phase 3b).
	Version int `json:"version,omitempty"`
	// BackedAt is the time this version was committed.
	BackedAt time.Time `json:"backed_at,omitempty"`
	// Compressed indicates the chunk data was zstd-compressed before RS encoding.
	// Clients must decompress after RS reconstruction. False for legacy backups.
	Compressed bool `json:"compressed,omitempty"`
}

// ShardLocation records where one shard is stored.
type ShardLocation struct {
	// ShardIndex matches EncodedShard.ShardIndex.
	ShardIndex int `json:"shard_index"`
	// IsParity mirrors EncodedShard.IsParity.
	IsParity bool `json:"is_parity"`
	// BuddyID is the libp2p PeerID of the buddy holding this shard (Phase 2).
	// In Phase 1 (local store) this is the string "local".
	BuddyID string `json:"buddy_id"`
	// StorageKey is the key used to retrieve the shard from the buddy's store.
	StorageKey string `json:"storage_key"`
}
