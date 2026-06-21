// Package manifest maintains the encrypted index of all backed-up files.
// The manifest is stored as an AES-256-GCM encrypted JSON file.
// In Phase 2 it will also be distributed as chunks among buddies.
package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	bbcrypto "github.com/cerclbackup/cerclbackup/internal/crypto"
	"github.com/cerclbackup/cerclbackup/pkg/protocol"
	"github.com/google/uuid"
)

// DefaultManifestPath returns the platform-appropriate default path.
func DefaultManifestPath() string {
	if appdata := os.Getenv("APPDATA"); appdata != "" {
		return filepath.Join(appdata, "CerclBackup", "manifest.enc")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "cerclbackup", "manifest.enc")
}

// data is the decrypted in-memory manifest structure.
type data struct {
	Version int                                `json:"version"`
	Entries map[string]*protocol.ManifestEntry `json:"entries"` // keyed by FileID
}

// Manifest is the encrypted file index.  All public methods are safe for
// concurrent use.
type Manifest struct {
	mu        sync.RWMutex
	path      string
	masterKey []byte
	d         data
}

// New returns an empty Manifest bound to path and masterKey.
// Call Load to populate from disk, or use directly for a fresh install.
func New(path string, masterKey []byte) *Manifest {
	return &Manifest{
		path:      path,
		masterKey: masterKey,
		d: data{
			Version: 1,
			Entries: make(map[string]*protocol.ManifestEntry),
		},
	}
}

// Load decrypts and loads the manifest from disk.
// Returns nil error if the file does not exist yet (first run).
func (m *Manifest) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	blob, err := os.ReadFile(m.path)
	if os.IsNotExist(err) {
		return nil // first run
	}
	if err != nil {
		return fmt.Errorf("manifest: read: %w", err)
	}

	plaintext, err := bbcrypto.Decrypt(m.masterKey, blob)
	if err != nil {
		return fmt.Errorf("manifest: decrypt: %w", err)
	}

	var d data
	if err := json.Unmarshal(plaintext, &d); err != nil {
		return fmt.Errorf("manifest: parse: %w", err)
	}
	m.d = d
	return nil
}

// Save encrypts and persists the manifest to disk.
func (m *Manifest) Save() error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	plaintext, err := json.Marshal(m.d)
	if err != nil {
		return fmt.Errorf("manifest: marshal: %w", err)
	}

	encrypted, err := bbcrypto.Encrypt(m.masterKey, plaintext)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(m.path), 0700); err != nil {
		return fmt.Errorf("manifest: mkdir: %w", err)
	}
	if err := os.WriteFile(m.path, encrypted, 0600); err != nil {
		return fmt.Errorf("manifest: write: %w", err)
	}
	return nil
}

// Upsert creates or replaces the entry for the given file path.
// contentHash must be the same hash used to derive the store fileID (fileHashFromChunks).
func (m *Manifest) Upsert(srcPath string, contentHash [32]byte, size int64, scheme protocol.RSScheme, shards []protocol.ShardLocation) (*protocol.ManifestEntry, error) {
	hash := contentHash

	info, err := os.Stat(srcPath)
	if err != nil {
		return nil, fmt.Errorf("manifest: stat %q: %w", srcPath, err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Reuse existing FileID if the path was already backed up.
	fileID := ""
	for id, e := range m.d.Entries {
		if e.Path == srcPath {
			fileID = id
			break
		}
	}
	if fileID == "" {
		fileID = uuid.New().String()
	}

	entry := &protocol.ManifestEntry{
		FileID:   fileID,
		Path:     srcPath,
		Hash:     hex.EncodeToString(hash[:]),
		Size:     size,
		Modified: info.ModTime().UTC(),
		Scheme:   scheme,
		Shards:   shards,
	}
	m.d.Entries[fileID] = entry
	return entry, nil
}

// Get returns the manifest entry for fileID, or nil if absent.
func (m *Manifest) Get(fileID string) *protocol.ManifestEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e := m.d.Entries[fileID]
	return e
}

// FindByPath returns the manifest entry whose Path matches, or nil.
func (m *Manifest) FindByPath(path string) *protocol.ManifestEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, e := range m.d.Entries {
		if e.Path == path {
			return e
		}
	}
	return nil
}

// All returns a snapshot of all entries.
func (m *Manifest) All() []*protocol.ManifestEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*protocol.ManifestEntry, 0, len(m.d.Entries))
	for _, e := range m.d.Entries {
		out = append(out, e)
	}
	return out
}

// Remove deletes the entry for fileID from the manifest.
func (m *Manifest) Remove(fileID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.d.Entries, fileID)
}

// hashFile computes the SHA-256 of the file at path.
func hashFile(path string) ([32]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return [32]byte{}, fmt.Errorf("manifest: open for hash %q: %w", path, err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return [32]byte{}, fmt.Errorf("manifest: hash %q: %w", path, err)
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out, nil
}

// LastModified returns the most recent Modified timestamp across all entries.
// Returns zero time if the manifest is empty.
func (m *Manifest) LastModified() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var t time.Time
	for _, e := range m.d.Entries {
		if e.Modified.After(t) {
			t = e.Modified
		}
	}
	return t
}
