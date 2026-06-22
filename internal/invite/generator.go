package invite

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/tyler-smith/go-bip39"
)

const (
	tokenSize    = 16              // 128 bits → 12 BIP39 words
	inviteTTL    = 24 * time.Hour  // invites expire after 24 h
)

// Code is a 12-word BIP39 mnemonic representing an invite token.
type Code struct {
	Words string // space-separated 12 BIP39 words
	Token []byte // raw 16-byte token (for in-memory matching)
}

// Pending stores an unaccepted invite.
type Pending struct {
	Token     []byte    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Commitment holds the SHA-256 hash of an email-invite OOB secret.
type Commitment struct {
	Hash      []byte    `json:"hash"`       // SHA-256 of the pre-image
	ExpiresAt time.Time `json:"expires_at"`
}

type pendingStore struct {
	Pending     []*Pending    `json:"pending"`
	Commitments []*Commitment `json:"commitments,omitempty"`
}

// Manager generates and validates invite codes.
type Manager struct {
	path string
	mu   sync.Mutex
}

// NewManager creates a Manager backed by the given file path.
func NewManager(path string) *Manager {
	return &Manager{path: path}
}

// Generate creates a new invite code and persists it as a pending invite.
func (m *Manager) Generate() (Code, error) {
	token := make([]byte, tokenSize)
	if _, err := rand.Read(token); err != nil {
		return Code{}, fmt.Errorf("invite: rand: %w", err)
	}

	mnemonic, err := bip39.NewMnemonic(token)
	if err != nil {
		return Code{}, fmt.Errorf("invite: bip39: %w", err)
	}

	if err := m.addPending(&Pending{
		Token:     token,
		ExpiresAt: time.Now().Add(inviteTTL),
	}); err != nil {
		return Code{}, err
	}

	return Code{Words: mnemonic, Token: token}, nil
}

// Consume validates and removes an invite token. Returns an error if the
// token is unknown or expired.
func (m *Manager) Consume(token []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	store, err := m.loadLocked()
	if err != nil {
		return err
	}

	now := time.Now()
	found := false
	filtered := store.Pending[:0]
	for _, p := range store.Pending {
		if p.ExpiresAt.Before(now) {
			continue // drop expired
		}
		if string(p.Token) == string(token) {
			found = true
			continue // consume it
		}
		filtered = append(filtered, p)
	}
	if !found {
		return fmt.Errorf("invite: unknown or expired token")
	}
	store.Pending = filtered
	return m.saveLocked(store)
}

// TokenFromMnemonic decodes a mnemonic back to the raw 16-byte token.
// go-bip39 MnemonicToByteArray returns 17 bytes for 128-bit entropy where the
// 4-bit BIP39 checksum occupies the high nibble of byte[0] and the actual
// entropy is stored in bits 4..131 (i.e. shifted right by 4). We undo that
// shift to recover the original 16 bytes.
func TokenFromMnemonic(mnemonic string) ([]byte, error) {
	raw, err := bip39.MnemonicToByteArray(mnemonic)
	if err != nil {
		return nil, fmt.Errorf("invite: decode mnemonic: %w", err)
	}
	if len(raw) < 17 {
		// Older library version that returns plain entropy — use as-is.
		if len(raw) < tokenSize {
			return nil, fmt.Errorf("invite: token too short (%d bytes)", len(raw))
		}
		return raw[:tokenSize], nil
	}
	// Shift-left 4 bits to strip the leading checksum nibble.
	out := make([]byte, tokenSize)
	for i := 0; i < tokenSize; i++ {
		out[i] = (raw[i] << 4) | (raw[i+1] >> 4)
	}
	return out, nil
}

func (m *Manager) addPending(p *Pending) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	store, err := m.loadLocked()
	if err != nil {
		return err
	}
	// Prune expired
	now := time.Now()
	fresh := store.Pending[:0]
	for _, pp := range store.Pending {
		if pp.ExpiresAt.After(now) {
			fresh = append(fresh, pp)
		}
	}
	store.Pending = append(fresh, p)
	return m.saveLocked(store)
}

func (m *Manager) loadLocked() (*pendingStore, error) {
	data, err := os.ReadFile(m.path)
	if os.IsNotExist(err) {
		return &pendingStore{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("invite: read %q: %w", m.path, err)
	}
	var s pendingStore
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("invite: unmarshal: %w", err)
	}
	return &s, nil
}

func (m *Manager) saveLocked(s *pendingStore) error {
	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("invite: marshal: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(m.path), 0700); err != nil {
		return fmt.Errorf("invite: mkdir: %w", err)
	}
	return os.WriteFile(m.path, data, 0600)
}

// AddCommitment registers the SHA-256 hash of an email-invite OOB secret so
// that ConsumeCommitment can verify the pre-image when the joiner connects.
func (m *Manager) AddCommitment(hash []byte, expiresAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	store, err := m.loadLocked()
	if err != nil {
		return err
	}
	store.Commitments = append(store.Commitments, &Commitment{
		Hash:      hash,
		ExpiresAt: expiresAt,
	})
	return m.saveLocked(store)
}

// ConsumeCommitment verifies that SHA-256(preimage) matches a non-expired
// pending commitment and removes it (one-time use).  Returns an error if no
// matching commitment is found.
func (m *Manager) ConsumeCommitment(preimage []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	store, err := m.loadLocked()
	if err != nil {
		return err
	}

	h := sha256.Sum256(preimage)
	now := time.Now()
	remaining := store.Commitments[:0]
	found := false
	for _, c := range store.Commitments {
		if !found && !now.After(c.ExpiresAt) && bytes.Equal(c.Hash, h[:]) {
			found = true // consume — do not append
			continue
		}
		remaining = append(remaining, c)
	}
	if !found {
		return fmt.Errorf("invite: no valid commitment for presented pre-image")
	}
	store.Commitments = remaining
	return m.saveLocked(store)
}
