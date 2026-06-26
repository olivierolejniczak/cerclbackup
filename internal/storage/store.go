// Package storage manages the local on-disk store for shards received from
// buddies (i.e. shards we hold on behalf of others) as well as our own
// encrypted shards during Phase 1 (no network yet).
//
// Layout on disk:
//
//	<root>/
//	  <fileID>/
//	    <shardIndex>.shard   — raw encrypted bytes
//	    meta.json            — ShardMeta for each shard in this file
package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// DefaultStorePath returns a sensible default root directory.
func DefaultStorePath() string {
	if d := os.Getenv("CERCLBACKUP_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "store")
	}
	if appdata := os.Getenv("APPDATA"); appdata != "" {
		return filepath.Join(appdata, "CerclBackup", "store")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "cerclbackup", "store")
}

// ShardMeta records metadata for one stored shard.
type ShardMeta struct {
	FileID     string `json:"file_id"`
	ShardIndex int    `json:"shard_index"`
	IsParity   bool   `json:"is_parity"`
	Size       int    `json:"size"` // bytes of encrypted data
}

// Store is a simple flat-file shard store.
type Store struct {
	root string
}

// New creates a Store rooted at root.  The directory is created if absent.
func New(root string) (*Store, error) {
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, fmt.Errorf("storage: mkdir %q: %w", root, err)
	}
	return &Store{root: root}, nil
}

// Put writes encrypted shard data for (fileID, shardIndex) to disk.
func (s *Store) Put(fileID string, shardIndex int, isParity bool, data []byte) error {
	dir := filepath.Join(s.root, fileID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("storage: mkdir %q: %w", dir, err)
	}

	shardPath := s.shardPath(fileID, shardIndex)
	if err := os.WriteFile(shardPath, data, 0600); err != nil {
		return fmt.Errorf("storage: write shard %d for %q: %w", shardIndex, fileID, err)
	}

	// Update meta.json.
	meta, _ := s.readMeta(fileID) // ignore error if missing
	updated := false
	for i, m := range meta {
		if m.ShardIndex == shardIndex {
			meta[i].Size = len(data)
			meta[i].IsParity = isParity
			updated = true
			break
		}
	}
	if !updated {
		meta = append(meta, ShardMeta{
			FileID:     fileID,
			ShardIndex: shardIndex,
			IsParity:   isParity,
			Size:       len(data),
		})
	}
	return s.writeMeta(fileID, meta)
}

// Get retrieves the encrypted shard data for (fileID, shardIndex).
func (s *Store) Get(fileID string, shardIndex int) ([]byte, error) {
	data, err := os.ReadFile(s.shardPath(fileID, shardIndex))
	if err != nil {
		return nil, fmt.Errorf("storage: get shard %d for %q: %w", shardIndex, fileID, err)
	}
	return data, nil
}

// Delete removes all shards and metadata for fileID.
func (s *Store) Delete(fileID string) error {
	dir := filepath.Join(s.root, fileID)
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("storage: delete %q: %w", fileID, err)
	}
	return nil
}

// ListFiles returns all fileIDs currently in the store.
func (s *Store) ListFiles() ([]string, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("storage: list: %w", err)
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() {
			ids = append(ids, e.Name())
		}
	}
	return ids, nil
}

// Meta returns the shard metadata for fileID.
func (s *Store) Meta(fileID string) ([]ShardMeta, error) {
	return s.readMeta(fileID)
}

// DiskUsageBytes returns the total bytes used by the store.
func (s *Store) DiskUsageBytes() (int64, error) {
	var total int64
	err := filepath.Walk(s.root, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total, err
}

func (s *Store) shardPath(fileID string, shardIndex int) string {
	return filepath.Join(s.root, fileID, strconv.Itoa(shardIndex)+".shard")
}

func (s *Store) metaPath(fileID string) string {
	return filepath.Join(s.root, fileID, "meta.json")
}

func (s *Store) readMeta(fileID string) ([]ShardMeta, error) {
	data, err := os.ReadFile(s.metaPath(fileID))
	if err != nil {
		return nil, err
	}
	var meta []ShardMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return meta, nil
}

func (s *Store) writeMeta(fileID string, meta []ShardMeta) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.metaPath(fileID), data, 0600)
}
