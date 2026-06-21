package buddy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	bbcrypto "github.com/cerclbackup/cerclbackup/internal/crypto"
)

// Entry represents a known buddy.
type Entry struct {
	PeerID       string    `json:"peer_id"`
	PubKey       []byte    `json:"pub_key"`  // serialised libp2p pubkey bytes
	FriendlyName string    `json:"friendly_name,omitempty"`
	Addrs        []string  `json:"addrs,omitempty"` // last-seen multiaddrs
	AddedAt      time.Time `json:"added_at"`
}

type registryData struct {
	Entries []*Entry `json:"entries"`
}

// Registry persists known buddies in an encrypted JSON file.
type Registry struct {
	path      string
	masterKey []byte
	mu        sync.RWMutex
	byPeerID  map[string]*Entry
}

// NewRegistry opens or creates the encrypted buddy registry.
func NewRegistry(path string, masterKey []byte) (*Registry, error) {
	r := &Registry{
		path:      path,
		masterKey: masterKey,
		byPeerID:  make(map[string]*Entry),
	}
	if err := r.load(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Registry) load() error {
	ciphertext, err := os.ReadFile(r.path)
	if os.IsNotExist(err) {
		return nil // empty registry is fine
	}
	if err != nil {
		return fmt.Errorf("registry: read %q: %w", r.path, err)
	}
	plain, err := bbcrypto.Decrypt(r.masterKey, ciphertext)
	if err != nil {
		return fmt.Errorf("registry: decrypt: %w", err)
	}
	var d registryData
	if err := json.Unmarshal(plain, &d); err != nil {
		return fmt.Errorf("registry: unmarshal: %w", err)
	}
	for _, e := range d.Entries {
		r.byPeerID[e.PeerID] = e
	}
	return nil
}

func (r *Registry) save() error {
	entries := make([]*Entry, 0, len(r.byPeerID))
	for _, e := range r.byPeerID {
		entries = append(entries, e)
	}
	plain, err := json.Marshal(registryData{Entries: entries})
	if err != nil {
		return fmt.Errorf("registry: marshal: %w", err)
	}
	ciphertext, err := bbcrypto.Encrypt(r.masterKey, plain)
	if err != nil {
		return fmt.Errorf("registry: encrypt: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(r.path), 0700); err != nil {
		return fmt.Errorf("registry: mkdir: %w", err)
	}
	return os.WriteFile(r.path, ciphertext, 0600)
}

// Add adds or replaces a buddy entry.
func (r *Registry) Add(e *Entry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e.AddedAt.IsZero() {
		e.AddedAt = time.Now().UTC()
	}
	r.byPeerID[e.PeerID] = e
	return r.save()
}

// UpdateAddrs persists the latest multiaddrs seen for a buddy.
func (r *Registry) UpdateAddrs(peerID string, addrs []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.byPeerID[peerID]; ok {
		e.Addrs = addrs
		_ = r.save()
	}
}

// Remove deletes a buddy from the registry.
func (r *Registry) Remove(peerID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.byPeerID, peerID)
	return r.save()
}

// IsKnown returns true if the given PeerID is in the registry.
func (r *Registry) IsKnown(peerID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.byPeerID[peerID]
	return ok
}

// Get returns the entry for a PeerID, if present.
func (r *Registry) Get(peerID string) (*Entry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.byPeerID[peerID]
	return e, ok
}

// List returns all known buddies.
func (r *Registry) List() []*Entry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Entry, 0, len(r.byPeerID))
	for _, e := range r.byPeerID {
		out = append(out, e)
	}
	return out
}
