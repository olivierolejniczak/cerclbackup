package scrub_test

import (
	"context"
	"crypto/rand"
	"path/filepath"
	"testing"
	"time"

	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/cerclbackup/cerclbackup/internal/buddy"
	"github.com/cerclbackup/cerclbackup/internal/invite"
	"github.com/cerclbackup/cerclbackup/internal/p2p"
	"github.com/cerclbackup/cerclbackup/internal/scrub"
)

var testMasterKey = make([]byte, 32)

// TestScrubAllHealthy: every shard passes the hash check.
func TestScrubAllHealthy(t *testing.T) {
	dir := t.TempDir()
	bs := buddy.NewStore(filepath.Join(dir, "store"))

	ownerID := "owner-peer-a"
	fileID := "file-1"
	for i := 0; i < 3; i++ {
		if err := bs.PutWithHash(ownerID, fileID, i, []byte("shard-data")); err != nil {
			t.Fatal(err)
		}
	}

	mgr := scrub.New(bs, nil, nil)
	r, err := mgr.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Checked != 3 {
		t.Errorf("checked=%d want 3", r.Checked)
	}
	if r.Corrupted != 0 {
		t.Errorf("corrupted=%d want 0", r.Corrupted)
	}
	if r.OK != 3 {
		t.Errorf("ok=%d want 3", r.OK)
	}
}

// TestScrubDetectsCorruption: overwriting a shard without updating its hash
// causes scrub to flag it as corrupted.
func TestScrubDetectsCorruption(t *testing.T) {
	dir := t.TempDir()
	bs := buddy.NewStore(filepath.Join(dir, "store"))

	ownerID := "owner-peer-b"
	fileID := "file-2"
	for i := 0; i < 3; i++ {
		if err := bs.PutWithHash(ownerID, fileID, i, []byte("good data")); err != nil {
			t.Fatal(err)
		}
	}

	// Corrupt shard 1 by overwriting with bad bytes (bypasses PutWithHash).
	if err := bs.Put(ownerID, fileID, 1, []byte("corrupted!")); err != nil {
		t.Fatal(err)
	}

	mgr := scrub.New(bs, nil, nil)
	r, err := mgr.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Corrupted != 1 {
		t.Errorf("corrupted=%d want 1", r.Corrupted)
	}
	if r.Failed != 1 {
		// No host/registry provided, so revive fails.
		t.Errorf("failed=%d want 1", r.Failed)
	}
}

// TestSilentRevive: Alice stores a shard on Bob. Bob's copy is corrupted.
// Bob's scrub manager connects to Alice and re-fetches the shard, restoring it.
func TestSilentRevive(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// --- Two real libp2p hosts ---
	privA, _, _ := libp2pcrypto.GenerateEd25519Key(rand.Reader)
	privB, _, _ := libp2pcrypto.GenerateEd25519Key(rand.Reader)

	alice, err := p2p.NewHost(privA, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer alice.Close()

	bob, err := p2p.NewHost(privB, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer bob.Close()

	// Registries: mutual knowledge
	aliceReg, _ := buddy.NewRegistry(filepath.Join(dir, "alice_reg.enc"), testMasterKey)
	aliceAddrStrs := make([]string, 0, len(alice.Addrs()))
	for _, a := range alice.Addrs() {
		aliceAddrStrs = append(aliceAddrStrs, a.String())
	}
	_ = aliceReg.Add(&buddy.Entry{PeerID: bob.ID().String(), PubKey: []byte("pk")})

	bobReg, _ := buddy.NewRegistry(filepath.Join(dir, "bob_reg.enc"), testMasterKey)
	_ = bobReg.Add(&buddy.Entry{
		PeerID: alice.ID().String(),
		PubKey: []byte("pk"),
		Addrs:  aliceAddrStrs,
	})

	// Stores
	aliceStore := buddy.NewStore(filepath.Join(dir, "alice_shards"))
	bobStore := buddy.NewStore(filepath.Join(dir, "bob_shards"))

	// Register pull handler on Alice so Bob can fetch from her.
	p2p.RegisterHandlers(alice, aliceReg, aliceStore, invite.NewManager(filepath.Join(dir, "a_inv.json")))

	// Connect Bob to Alice
	if err := bob.Connect(ctx, peer.AddrInfo{ID: alice.ID(), Addrs: alice.Addrs()}); err != nil {
		t.Fatalf("connect bob->alice: %v", err)
	}

	// Alice stores the canonical shard (simulates her local store after backup).
	ownerID := alice.ID().String()
	fileID := "revive-test"
	shardIdx := 0
	shardData := []byte("canonical shard data from alice")
	if err := aliceStore.PutWithHash(ownerID, fileID, shardIdx, shardData); err != nil {
		t.Fatal(err)
	}

	// Bob stores a corrupted copy (the .hash file is correct, but data was overwritten).
	if err := bobStore.PutWithHash(ownerID, fileID, shardIdx, shardData); err != nil {
		t.Fatal(err)
	}
	// Corrupt data without updating hash.
	if err := bobStore.Put(ownerID, fileID, shardIdx, []byte("bitrot!")); err != nil {
		t.Fatal(err)
	}

	// Sanity check: Verify should return false now.
	if bobStore.Verify(ownerID, fileID, shardIdx) {
		t.Fatal("expected Verify to return false after corruption")
	}

	// Run scrub on Bob -- should detect corruption and revive from Alice.
	mgr := scrub.New(bobStore, bob, bobReg)
	r, err := mgr.RunOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if r.Corrupted != 1 {
		t.Errorf("corrupted=%d want 1", r.Corrupted)
	}
	if r.Revived != 1 {
		t.Errorf("revived=%d want 1 (got failed=%d)", r.Revived, r.Failed)
	}

	// Verify the shard is now healthy again.
	if !bobStore.Verify(ownerID, fileID, shardIdx) {
		t.Error("shard should pass Verify after revive")
	}
	got, err := bobStore.Get(ownerID, fileID, shardIdx)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(shardData) {
		t.Errorf("revived data %q, want %q", got, shardData)
	}
}

// TestScrubStart verifies the periodic scrub ticker fires and produces a
// report without errors.
func TestScrubStart(t *testing.T) {
	dir := t.TempDir()
	bs := buddy.NewStore(filepath.Join(dir, "store"))
	if err := bs.PutWithHash("owner", "file", 0, []byte("data")); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	mgr := scrub.New(bs, nil, nil)
	mgr.Start(ctx, 100*time.Millisecond) // fast interval for test

	// Wait for at least one pass to complete.
	time.Sleep(300 * time.Millisecond)
	// No assertion here -- if Start panics the test fails automatically.
}
