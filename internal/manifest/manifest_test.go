package manifest_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cerclbackup/cerclbackup/internal/manifest"
	"github.com/cerclbackup/cerclbackup/pkg/protocol"
)

func newTestManifest(t *testing.T) *manifest.Manifest {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.enc")
	return manifest.New(path, key)
}

func upsert(t *testing.T, m *manifest.Manifest, path string, version byte) *protocol.ManifestEntry {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "src")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("data")
	f.Close()

	var hash [32]byte
	hash[0] = version
	entry, err := m.Upsert(f.Name(), hash, 4, protocol.RSScheme{DataShards: 3, ParityShards: 2}, nil)
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	// Override Path so ListVersions matches the logical path, not the temp path.
	_ = entry
	return entry
}

func TestUpsertCreatesNewVersion(t *testing.T) {
	m := newTestManifest(t)

	// Create a real temp file to stat.
	f, _ := os.CreateTemp(t.TempDir(), "file")
	f.WriteString("hello")
	f.Close()

	var h1, h2 [32]byte
	h1[0] = 1
	h2[0] = 2

	e1, err := m.Upsert(f.Name(), h1, 5, protocol.RSScheme{DataShards: 3, ParityShards: 2}, nil)
	if err != nil {
		t.Fatalf("Upsert 1: %v", err)
	}
	e2, err := m.Upsert(f.Name(), h2, 5, protocol.RSScheme{DataShards: 3, ParityShards: 2}, nil)
	if err != nil {
		t.Fatalf("Upsert 2: %v", err)
	}

	if e1.FileID == e2.FileID {
		t.Error("same FileID for two versions — want distinct UUIDs")
	}
	if e1.Version != 1 {
		t.Errorf("first version: got %d, want 1", e1.Version)
	}
	if e2.Version != 2 {
		t.Errorf("second version: got %d, want 2", e2.Version)
	}
	if e1.BackedAt.IsZero() || e2.BackedAt.IsZero() {
		t.Error("BackedAt not set")
	}
}

func TestListVersionsOrder(t *testing.T) {
	m := newTestManifest(t)

	f, _ := os.CreateTemp(t.TempDir(), "file")
	f.WriteString("data")
	f.Close()

	var h [32]byte
	for i := 0; i < 4; i++ {
		h[0] = byte(i)
		if _, err := m.Upsert(f.Name(), h, 4, protocol.RSScheme{DataShards: 3, ParityShards: 2}, nil); err != nil {
			t.Fatalf("Upsert %d: %v", i, err)
		}
	}

	versions := m.ListVersions(f.Name())
	if len(versions) != 4 {
		t.Fatalf("got %d versions, want 4", len(versions))
	}
	for i, v := range versions {
		if v.Version != i+1 {
			t.Errorf("versions[%d].Version = %d, want %d", i, v.Version, i+1)
		}
	}
}

func TestLatest(t *testing.T) {
	m := newTestManifest(t)

	f, _ := os.CreateTemp(t.TempDir(), "file")
	f.WriteString("data")
	f.Close()

	var h [32]byte
	for i := 0; i < 3; i++ {
		h[0] = byte(i)
		m.Upsert(f.Name(), h, 4, protocol.RSScheme{DataShards: 3, ParityShards: 2}, nil)
	}

	latest := m.Latest(f.Name())
	if latest == nil {
		t.Fatal("Latest returned nil")
	}
	if latest.Version != 3 {
		t.Errorf("Latest version = %d, want 3", latest.Version)
	}
}

func TestLatestUnknownPath(t *testing.T) {
	m := newTestManifest(t)
	if got := m.Latest("/does/not/exist"); got != nil {
		t.Errorf("Latest on missing path = %v, want nil", got)
	}
}

func TestPruneVersionsKeepsRecent(t *testing.T) {
	m := newTestManifest(t)

	f, _ := os.CreateTemp(t.TempDir(), "file")
	f.WriteString("data")
	f.Close()

	var h [32]byte
	for i := 0; i < 5; i++ {
		h[0] = byte(i)
		m.Upsert(f.Name(), h, 4, protocol.RSScheme{DataShards: 3, ParityShards: 2}, nil)
	}

	policy := manifest.RetentionPolicy{KeepAllDays: 30, KeepWeeklyDays: 90, MaxVersions: 3}
	pruned := m.PruneVersions(policy)

	remaining := m.ListVersions(f.Name())
	if len(remaining) != 3 {
		t.Errorf("after prune: got %d versions, want 3", len(remaining))
	}
	if len(pruned) != 2 {
		t.Errorf("pruned count: got %d, want 2", len(pruned))
	}
	// Newest must survive.
	if remaining[len(remaining)-1].Version != 5 {
		t.Errorf("latest version after prune = %d, want 5", remaining[len(remaining)-1].Version)
	}
}

func TestPruneVersionsDefaultPolicy(t *testing.T) {
	policy := manifest.DefaultRetentionPolicy()
	if policy.KeepAllDays != 30 || policy.KeepWeeklyDays != 90 || policy.MaxVersions != 50 {
		t.Errorf("unexpected default policy: %+v", policy)
	}
}

func TestUpsertBackedAtNonZero(t *testing.T) {
	m := newTestManifest(t)

	f, _ := os.CreateTemp(t.TempDir(), "file")
	f.WriteString("data")
	f.Close()

	before := time.Now()
	var h [32]byte
	e, _ := m.Upsert(f.Name(), h, 4, protocol.RSScheme{DataShards: 3, ParityShards: 2}, nil)
	after := time.Now()

	if e.BackedAt.Before(before) || e.BackedAt.After(after) {
		t.Errorf("BackedAt %v not between %v and %v", e.BackedAt, before, after)
	}
}
