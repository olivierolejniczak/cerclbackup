package circle_test

import (
	"sync"
	"testing"

	"github.com/cerclbackup/cerclbackup/internal/circle"
)

// stubStore is an in-memory extraStore for tests (no keystore dependency).
type stubStore struct {
	mu   sync.Mutex
	data map[string][]byte
}

func newStub() *stubStore { return &stubStore{data: make(map[string][]byte)} }

func (s *stubStore) LoadExtra(name string) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data[name]
}
func (s *stubStore) StoreExtra(name string, data []byte, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[name] = append([]byte(nil), data...)
	return nil
}

func TestAddAndList(t *testing.T) {
	m := circle.NewManager(newStub(), "pw")

	if _, err := m.Add("Famille", "3/2"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Add("Travail", "2/1"); err != nil {
		t.Fatal(err)
	}

	list, err := m.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 circles, got %d", len(list))
	}
}

func TestDuplicateName(t *testing.T) {
	m := circle.NewManager(newStub(), "pw")
	if _, err := m.Add("Famille", "3/2"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Add("Famille", "2/1"); err == nil {
		t.Fatal("expected error for duplicate name, got nil")
	}
}

func TestRemove(t *testing.T) {
	m := circle.NewManager(newStub(), "pw")
	if _, err := m.Add("Famille", "3/2"); err != nil {
		t.Fatal(err)
	}
	if err := m.Remove("Famille"); err != nil {
		t.Fatal(err)
	}
	list, _ := m.List()
	if len(list) != 0 {
		t.Fatalf("expected 0 circles after remove, got %d", len(list))
	}
}

func TestRemoveNotFound(t *testing.T) {
	m := circle.NewManager(newStub(), "pw")
	if err := m.Remove("Nope"); err == nil {
		t.Fatal("expected error for non-existent circle")
	}
}

func TestGetOrDefaultCreatesDefault(t *testing.T) {
	m := circle.NewManager(newStub(), "pw")
	c, err := m.GetOrDefault("", "pw")
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != circle.DefaultName {
		t.Fatalf("expected %q, got %q", circle.DefaultName, c.Name)
	}
	// Second call must return the same circle (not re-create).
	c2, err := m.GetOrDefault("", "pw")
	if err != nil {
		t.Fatal(err)
	}
	if c.ID != c2.ID {
		t.Fatalf("GetOrDefault returned different IDs: %s vs %s", c.ID, c2.ID)
	}
}

func TestDeriveKeyDifferentPerCircle(t *testing.T) {
	password := "hunter2"
	stub := newStub()
	m := circle.NewManager(stub, password)

	c1, _ := m.Add("Famille", "3/2")
	c2, _ := m.Add("Travail", "2/1")

	k1 := c1.DeriveKey(password)
	k2 := c2.DeriveKey(password)

	for i := range k1 {
		if k1[i] != k2[i] {
			return // keys differ — pass
		}
	}
	t.Fatal("DeriveKey returned the same key for two distinct circles")
}

func TestPersistAcrossManagers(t *testing.T) {
	stub := newStub()
	m1 := circle.NewManager(stub, "pw")
	c, err := m1.Add("Famille", "3/2")
	if err != nil {
		t.Fatal(err)
	}

	// Simulate restart: new Manager on the same store.
	m2 := circle.NewManager(stub, "pw")
	got, err := m2.Get("Famille")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("circle not found after reload")
	}
	if got.ID != c.ID {
		t.Fatalf("ID mismatch after reload: %s vs %s", got.ID, c.ID)
	}
}
