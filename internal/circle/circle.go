// Package circle manages named backup circles, each with its own cryptographic
// key, buddy registry, manifest, and Reed-Solomon scheme.
//
// Circles are persisted as an encrypted JSON list in the keystore extras under
// the key "circles_v1", so they are automatically protected by the keystore's
// AES-256-GCM layer without requiring a separate file.
//
// Key isolation: each circle derives its own 32-byte master key via
//   Argon2id(password + "\x00" + circleID, circleSalt)
// A buddy in circle "Famille" cannot decrypt shards belonging to circle "Travail"
// even if they share the same keystore password.
package circle

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	bbcrypto "github.com/cerclbackup/cerclbackup/internal/crypto"
)

const (
	// DefaultName is the implicit circle used when no --circle flag is given.
	// It maps to the pre-Phase-3 single-circle layout for backwards compat.
	DefaultName = "Default"

	storeKey = "circles_v1"
)

// Circle describes one named backup circle.
type Circle struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Salt      []byte    `json:"salt"`      // 32-byte Argon2 salt unique to this circle
	Scheme    string    `json:"scheme"`    // RS data/parity, e.g. "3/2"
	CreatedAt time.Time `json:"created_at"`
}

// Manager stores and retrieves circles from a keystore.
type Manager struct {
	ks       extraStore
	password string
	circles  []*Circle
	loaded   bool
}

// extraStore is the subset of bbcrypto.Keystore used by Manager.
// It is an interface so tests can supply a stub.
type extraStore interface {
	LoadExtra(name string) []byte
	StoreExtra(name string, data []byte, password string) error
}

// NewManager creates a Manager backed by ks.  The password is required to
// re-save the extras after any mutation.
func NewManager(ks extraStore, password string) *Manager {
	return &Manager{ks: ks, password: password}
}

// List returns all circles.  The first call loads from the keystore; subsequent
// calls return the in-memory copy.
func (m *Manager) List() ([]*Circle, error) {
	if err := m.load(); err != nil {
		return nil, err
	}
	out := make([]*Circle, len(m.circles))
	copy(out, m.circles)
	return out, nil
}

// Get returns the circle with the given name (case-sensitive), or nil if not found.
func (m *Manager) Get(name string) (*Circle, error) {
	if err := m.load(); err != nil {
		return nil, err
	}
	for _, c := range m.circles {
		if c.Name == name {
			return c, nil
		}
	}
	return nil, nil
}

// GetOrDefault returns the named circle, or the Default circle if name is "".
// If the Default circle doesn't exist yet it is created automatically.
func (m *Manager) GetOrDefault(name, password string) (*Circle, error) {
	target := name
	if target == "" {
		target = DefaultName
	}
	c, err := m.Get(target)
	if err != nil {
		return nil, err
	}
	if c != nil {
		return c, nil
	}
	if target == DefaultName {
		return m.Add(DefaultName, "3/2")
	}
	return nil, fmt.Errorf("circle: %q not found", target)
}

// Add creates a new circle with the given name and RS scheme.
// Returns an error if a circle with that name already exists.
func (m *Manager) Add(name, scheme string) (*Circle, error) {
	if err := m.load(); err != nil {
		return nil, err
	}
	for _, c := range m.circles {
		if c.Name == name {
			return nil, fmt.Errorf("circle: %q already exists", name)
		}
	}
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("circle: salt: %w", err)
	}
	c := &Circle{
		ID:        uuid.New().String(),
		Name:      name,
		Salt:      salt,
		Scheme:    scheme,
		CreatedAt: time.Now().UTC(),
	}
	m.circles = append(m.circles, c)
	return c, m.save()
}

// Remove deletes the circle named name.  The caller must have confirmed the
// deletion interactively (the CLI enforces type-to-confirm).  Returns an error
// if the circle is not found.
func (m *Manager) Remove(name string) error {
	if err := m.load(); err != nil {
		return err
	}
	before := len(m.circles)
	cs := m.circles[:0]
	for _, c := range m.circles {
		if c.Name != name {
			cs = append(cs, c)
		}
	}
	if len(cs) == before {
		return fmt.Errorf("circle: %q not found", name)
	}
	m.circles = cs
	return m.save()
}

// DeriveKey returns the 32-byte master key for this circle.
// password is the user's keystore password.
func (c *Circle) DeriveKey(password string) []byte {
	return bbcrypto.DeriveCircleKey(password, c.ID, c.Salt)
}

func (m *Manager) load() error {
	if m.loaded {
		return nil
	}
	raw := m.ks.LoadExtra(storeKey)
	if len(raw) == 0 {
		m.circles = nil
		m.loaded = true
		return nil
	}
	if err := json.Unmarshal(raw, &m.circles); err != nil {
		return fmt.Errorf("circle: decode store: %w", err)
	}
	m.loaded = true
	return nil
}

func (m *Manager) save() error {
	b, err := json.Marshal(m.circles)
	if err != nil {
		return fmt.Errorf("circle: encode store: %w", err)
	}
	return m.ks.StoreExtra(storeKey, b, m.password)
}
