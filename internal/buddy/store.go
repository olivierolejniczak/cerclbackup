package buddy

import (
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
