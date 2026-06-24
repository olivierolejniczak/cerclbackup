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

// EncryptedBytes returns the encrypted manifest blob that Save() would write to
// disk, without performing any I/O.  Use this to push the manifest to buddies
// after a backup without requiring a second disk write.
func (m *Manifest) EncryptedBytes() ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	plaintext, err := json.Marshal(m.d)
	if err != nil {
		return nil, fmt.Errorf("manifest: marshal: %w", err)
	}
	return bbcrypto.Encrypt(m.masterKey, plaintext)
}

// Upsert creates a new versioned entry for the given file path.
// Each call creates a fresh FileID (UUID), increments the version number,
// and preserves all previous versions for the same path.
// Use PruneVersions to apply a retention policy.
func (m *Manifest) Upsert(srcPath string, contentHash [32]byte, size int64, scheme protocol.RSScheme, shards []protocol.ShardLocation) (*protocol.ManifestEntry, error) {
	info, err := os.Stat(srcPath)
	if err != nil {
		return nil, fmt.Errorf("manifest: stat %q: %w", srcPath, err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Determine the next version number by scanning existing entries.
	nextVersion := 1
	for _, e := range m.d.Entries {
		if e.Path == srcPath {
			v := e.Version
			if v == 0 {
				v = 1 // unversioned compat: treat as v1
			}
			if v >= nextVersion {
				nextVersion = v + 1
			}
		}
	}

	fileID := uuid.New().String()
	entry := &protocol.ManifestEntry{
		FileID:   fileID,
		Path:     srcPath,
		Hash:     hex.EncodeToString(contentHash[:]),
		Size:     size,
		Modified: info.ModTime().UTC(),
		Scheme:   scheme,
		Shards:   shards,
		Version:  nextVersion,
		BackedAt: time.Now().UTC(),
	}
	m.d.Entries[fileID] = entry
	return entry, nil
}

// ListVersions returns all versions for a given path, sorted oldest → newest.
func (m *Manifest) ListVersions(path string) []*protocol.ManifestEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var out []*protocol.ManifestEntry
	for _, e := range m.d.Entries {
		if e.Path == path {
			out = append(out, e)
		}
	}
	sortByVersion(out)
	return out
}

// Latest returns the most recent version of a file by path, or nil if not found.
func (m *Manifest) Latest(path string) *protocol.ManifestEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var best *protocol.ManifestEntry
	for _, e := range m.d.Entries {
		if e.Path != path {
			continue
		}
		if best == nil || versionOf(e) > versionOf(best) {
			best = e
		}
	}
	return best
}

// RetentionPolicy controls how many historical versions to keep per file.
type RetentionPolicy struct {
	KeepAllDays    int // keep every version within this many days (default 30)
	KeepWeeklyDays int // keep one per week within this many days (default 90)
	// Beyond KeepWeeklyDays: keep one per calendar month.
	MaxVersions int // hard cap per file path; 0 means no cap (default 50)
}

// DefaultRetentionPolicy returns the recommended retention settings.
func DefaultRetentionPolicy() RetentionPolicy {
	return RetentionPolicy{KeepAllDays: 30, KeepWeeklyDays: 90, MaxVersions: 50}
}

// PruneVersions removes old versions from the in-memory manifest according to
// policy.  Call Save() afterwards to persist the change.
// Versions that are pruned are removed from m.d.Entries; the caller is
// responsible for deleting the corresponding shards from buddies.
// Returns the list of FileIDs that were pruned.
func (m *Manifest) PruneVersions(policy RetentionPolicy) []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UTC()

	// Group entries by path.
	byPath := make(map[string][]*protocol.ManifestEntry)
	for _, e := range m.d.Entries {
		byPath[e.Path] = append(byPath[e.Path], e)
	}

	var pruned []string
	for _, versions := range byPath {
		sortByVersion(versions)
		keep := selectVersionsToKeep(versions, policy, now)
		keepSet := make(map[string]bool, len(keep))
		for _, e := range keep {
			keepSet[e.FileID] = true
		}
		for _, e := range versions {
			if !keepSet[e.FileID] {
				delete(m.d.Entries, e.FileID)
				pruned = append(pruned, e.FileID)
			}
		}
	}
	return pruned
}

func selectVersionsToKeep(versions []*protocol.ManifestEntry, policy RetentionPolicy, now time.Time) []*protocol.ManifestEntry {
	if len(versions) == 0 {
		return nil
	}
	// Always keep the most recent version.
	keep := []*protocol.ManifestEntry{versions[len(versions)-1]}
	seen := map[string]bool{versions[len(versions)-1].FileID: true}

	for i := len(versions) - 2; i >= 0; i-- {
		e := versions[i]
		backedAt := e.BackedAt
		if backedAt.IsZero() {
			backedAt = e.Modified
		}
		age := now.Sub(backedAt)

		switch {
		case age <= time.Duration(policy.KeepAllDays)*24*time.Hour:
			keep = append(keep, e)
			seen[e.FileID] = true
		case age <= time.Duration(policy.KeepWeeklyDays)*24*time.Hour:
			// Keep one per ISO week.
			yr, wk := backedAt.ISOWeek()
			weekKey := fmt.Sprintf("%d-W%02d", yr, wk)
			if !seen[weekKey] {
				seen[weekKey] = true
				keep = append(keep, e)
				seen[e.FileID] = true
			}
		default:
			// Keep one per calendar month.
			monthKey := fmt.Sprintf("%d-%02d", backedAt.Year(), backedAt.Month())
			if !seen[monthKey] {
				seen[monthKey] = true
				keep = append(keep, e)
				seen[e.FileID] = true
			}
		}
	}

	sortByVersion(keep)

	// Apply hard cap.
	if policy.MaxVersions > 0 && len(keep) > policy.MaxVersions {
		keep = keep[len(keep)-policy.MaxVersions:]
	}
	return keep
}

func versionOf(e *protocol.ManifestEntry) int {
	if e.Version == 0 {
		return 1
	}
	return e.Version
}

func sortByVersion(entries []*protocol.ManifestEntry) {
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0 && versionOf(entries[j]) < versionOf(entries[j-1]); j-- {
			entries[j], entries[j-1] = entries[j-1], entries[j]
		}
	}
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
