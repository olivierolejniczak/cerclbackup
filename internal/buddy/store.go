package buddy

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// Store persists shards received from remote buddies.
// Layout: <root>/remote/<ownerPeerID>/<fileID>/<shardIndex>.shard
type Store struct {
	root string
}

// NewStore creates a Store rooted at the given directory.
func NewStore(root string) *Store {
	return &Store{root: root}
}

func (s *Store) shardPath(ownerPeerID, fileID string, shardIndex int) string {
	return filepath.Join(s.root, "remote", ownerPeerID, fileID,
		strconv.Itoa(shardIndex)+".shard")
}

// Put stores a shard received from ownerPeerID.
func (s *Store) Put(ownerPeerID, fileID string, shardIndex int, data []byte) error {
	p := s.shardPath(ownerPeerID, fileID, shardIndex)
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return fmt.Errorf("buddystore: mkdir: %w", err)
	}
	if err := os.WriteFile(p, data, 0600); err != nil {
		return fmt.Errorf("buddystore: write: %w", err)
	}
	return nil
}

// Get retrieves a shard stored on behalf of ownerPeerID.
func (s *Store) Get(ownerPeerID, fileID string, shardIndex int) ([]byte, error) {
	p := s.shardPath(ownerPeerID, fileID, shardIndex)
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("buddystore: read: %w", err)
	}
	return data, nil
}

// Has returns true if the shard exists in the store.
func (s *Store) Has(ownerPeerID, fileID string, shardIndex int) bool {
	_, err := os.Stat(s.shardPath(ownerPeerID, fileID, shardIndex))
	return err == nil
}

// Delete removes a shard.
func (s *Store) Delete(ownerPeerID, fileID string, shardIndex int) error {
	return os.Remove(s.shardPath(ownerPeerID, fileID, shardIndex))
}

// DeleteOwner removes all shards stored on behalf of ownerPeerID.
func (s *Store) DeleteOwner(ownerPeerID string) error {
	p := filepath.Join(s.root, "remote", ownerPeerID)
	return os.RemoveAll(p)
}

// hashPath returns the sidecar hash file path for a shard.
func (s *Store) hashPath(ownerPeerID, fileID string, shardIndex int) string {
	return s.shardPath(ownerPeerID, fileID, shardIndex) + ".hash"
}

// PutWithHash stores the shard data and a sidecar SHA-256 hash file used by
// the scrub manager to detect corruption later.
func (s *Store) PutWithHash(ownerPeerID, fileID string, shardIndex int, data []byte) error {
	if err := s.Put(ownerPeerID, fileID, shardIndex, data); err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	return os.WriteFile(s.hashPath(ownerPeerID, fileID, shardIndex), sum[:], 0600)
}

// Verify reads the stored shard, recomputes its SHA-256, and compares it to
// the sidecar hash written by PutWithHash. Returns false if the shard or its
// hash file is missing, or if the hashes do not match.
func (s *Store) Verify(ownerPeerID, fileID string, shardIndex int) bool {
	data, err := s.Get(ownerPeerID, fileID, shardIndex)
	if err != nil {
		return false
	}
	expected, err := os.ReadFile(s.hashPath(ownerPeerID, fileID, shardIndex))
	if err != nil {
		return false
	}
	actual := sha256.Sum256(data)
	return bytes.Equal(actual[:], expected)
}

// ShardRef identifies a single shard stored in the buddy store.
type ShardRef struct {
	OwnerPeerID string
	FileID      string
	ShardIndex  int
}

// ListAll walks the store and returns a reference for every shard present.
func (s *Store) ListAll() ([]ShardRef, error) {
	root := filepath.Join(s.root, "remote")
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return nil, nil // no remote shards yet — not an error
	}
	var refs []ShardRef
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if filepath.Ext(path) != ".shard" {
			return nil
		}
		// path = <root>/remote/<ownerPeerID>/<fileID>/<idx>.shard
		rel, _ := filepath.Rel(root, path)
		parts := filepath.SplitList(filepath.ToSlash(rel))
		// SplitList splits on os.PathListSeparator, not path separator — use Split
		dir, base := filepath.Split(rel)
		dir = filepath.Clean(dir)
		ownerAndFile := filepath.SplitList(dir)
		_ = ownerAndFile
		// Parse manually: rel = owner/fileID/idx.shard
		segs := splitPath(rel)
		if len(segs) != 3 {
			return nil
		}
		idxStr := segs[2][:len(segs[2])-len(".shard")]
		idx, err := strconv.Atoi(idxStr)
		if err != nil {
			return nil
		}
		_ = base
		_ = parts
		refs = append(refs, ShardRef{
			OwnerPeerID: segs[0],
			FileID:      segs[1],
			ShardIndex:  idx,
		})
		return nil
	})
	return refs, err
}

// PutManifest stores an encrypted manifest blob for ownerID.
// The path is <root>/remote/<ownerID>/manifest.enc.
func (s *Store) PutManifest(ownerID string, data []byte) error {
	dir := filepath.Join(s.root, "remote", ownerID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("store: mkdir %s: %w", dir, err)
	}
	p := filepath.Join(dir, "manifest.enc")
	return os.WriteFile(p, data, 0600)
}

// GetManifest retrieves the encrypted manifest blob for ownerID, or returns
// an error if no manifest has been stored yet.
func (s *Store) GetManifest(ownerID string) ([]byte, error) {
	p := filepath.Join(s.root, "remote", ownerID, "manifest.enc")
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("store: no manifest for %s", ownerID)
		}
		return nil, fmt.Errorf("store: read manifest: %w", err)
	}
	return data, nil
}

// splitPath splits a filepath into its slash-separated components.
func splitPath(p string) []string {
	p = filepath.ToSlash(p)
	var parts []string
	for p != "" && p != "." {
		dir, file := filepath.Split(p)
		if file != "" {
			parts = append([]string{file}, parts...)
		}
		p = filepath.Clean(dir)
		if p == "." {
			break
		}
	}
	return parts
}
