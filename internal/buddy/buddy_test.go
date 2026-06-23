package buddy_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cerclbackup/cerclbackup/internal/buddy"
	"github.com/cerclbackup/cerclbackup/internal/testutil"
)

var testMasterKey []byte

func TestMain(m *testing.M) {
	testMasterKey = testutil.RandMasterKey()
	os.Exit(m.Run())
}

func TestRegistry_AddIsKnownRemove(t *testing.T) {
	dir := t.TempDir()
	reg, err := buddy.NewRegistry(filepath.Join(dir, "registry.enc"), testMasterKey)
	if err != nil {
		t.Fatal(err)
	}

	e := &buddy.Entry{
		PeerID:  "peer-alice",
		PubKey:  []byte("fakepubkey"),
		AddedAt: time.Now().UTC(),
	}
	if err := reg.Add(e); err != nil {
		t.Fatal(err)
	}
	if !reg.IsKnown("peer-alice") {
		t.Fatal("alice should be known")
	}
	if reg.IsKnown("peer-bob") {
		t.Fatal("bob should not be known")
	}

	if err := reg.Remove("peer-alice"); err != nil {
		t.Fatal(err)
	}
	if reg.IsKnown("peer-alice") {
		t.Fatal("alice should be removed")
	}
}

func TestRegistry_Persistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.enc")

	reg, _ := buddy.NewRegistry(path, testMasterKey)
	if err := reg.Add(&buddy.Entry{PeerID: "peer-alice", PubKey: []byte("pk")}); err != nil {
		t.Fatal(err)
	}

	// Reload from disk
	reg2, err := buddy.NewRegistry(path, testMasterKey)
	if err != nil {
		t.Fatal(err)
	}
	if !reg2.IsKnown("peer-alice") {
		t.Fatal("alice not found after reload")
	}
}

func TestStore_PutGet(t *testing.T) {
	dir := t.TempDir()
	s := buddy.NewStore(dir)

	data := []byte("encrypted shard bytes")
	if err := s.Put("peer-alice", "file1", 0, data); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get("peer-alice", "file1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(data) {
		t.Fatalf("data mismatch: got %q", got)
	}
}

func TestStore_DeleteOwner(t *testing.T) {
	dir := t.TempDir()
	s := buddy.NewStore(dir)

	if err := s.Put("peer-alice", "file1", 0, []byte("shard0")); err != nil {
		t.Fatal(err)
	}
	if err := s.Put("peer-alice", "file1", 1, []byte("shard1")); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteOwner("peer-alice"); err != nil {
		t.Fatal(err)
	}
	ownerDir := filepath.Join(dir, "remote", "peer-alice")
	if _, err := os.Stat(ownerDir); !os.IsNotExist(err) {
		t.Fatal("owner dir should be gone")
	}
}
