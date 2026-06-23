package rebalance_test

import (
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/cerclbackup/cerclbackup/internal/buddy"
	"github.com/cerclbackup/cerclbackup/internal/invite"
	"github.com/cerclbackup/cerclbackup/internal/p2p"
	"github.com/cerclbackup/cerclbackup/internal/rebalance"
	"github.com/cerclbackup/cerclbackup/internal/storage"
	"github.com/cerclbackup/cerclbackup/internal/testutil"
)

var testMasterKey []byte

func TestMain(m *testing.M) {
	testMasterKey = testutil.RandMasterKey()
	os.Exit(m.Run())
}

// TestRebalancePushesToNewBuddy: Alice has a local shard store and one buddy
// (Bob) that already has all shards. Carol is a newly-added buddy with zero
// shards. After Rebalancer.Run, Carol must have every shard.
func TestRebalancePushesToNewBuddy(t *testing.T) {
	dir := t.TempDir()

	// --- three libp2p hosts -------------------------------------------------
	privA, _, _ := libp2pcrypto.GenerateEd25519Key(rand.Reader)
	privB, _, _ := libp2pcrypto.GenerateEd25519Key(rand.Reader)
	privC, _, _ := libp2pcrypto.GenerateEd25519Key(rand.Reader)

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

	carol, err := p2p.NewHost(privC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer carol.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// --- Alice's registry: knows Bob and Carol ------------------------------
	aliceReg, _ := buddy.NewRegistry(filepath.Join(dir, "alice_reg.enc"), testMasterKey)
	carolAddrs := make([]string, 0, len(carol.Addrs()))
	for _, a := range carol.Addrs() {
		carolAddrs = append(carolAddrs, a.String())
	}
	bobAddrs := make([]string, 0, len(bob.Addrs()))
	for _, a := range bob.Addrs() {
		bobAddrs = append(bobAddrs, a.String())
	}
	if err := aliceReg.Add(&buddy.Entry{PeerID: bob.ID().String(), PubKey: testutil.MarshaledPubKey(t, bob), Addrs: bobAddrs}); err != nil {
		t.Fatalf("aliceReg.Add bob: %v", err)
	}
	if err := aliceReg.Add(&buddy.Entry{PeerID: carol.ID().String(), PubKey: testutil.MarshaledPubKey(t, carol), Addrs: carolAddrs}); err != nil {
		t.Fatalf("aliceReg.Add carol: %v", err)
	}

	// --- Bob and Carol each have a registry that knows Alice ----------------
	aliceAddrs := make([]string, 0, len(alice.Addrs()))
	for _, a := range alice.Addrs() {
		aliceAddrs = append(aliceAddrs, a.String())
	}
	bobReg, _ := buddy.NewRegistry(filepath.Join(dir, "bob_reg.enc"), testMasterKey)
	if err := bobReg.Add(&buddy.Entry{PeerID: alice.ID().String(), PubKey: testutil.MarshaledPubKey(t, alice), Addrs: aliceAddrs}); err != nil {
		t.Fatalf("bobReg.Add: %v", err)
	}

	carolReg, _ := buddy.NewRegistry(filepath.Join(dir, "carol_reg.enc"), testMasterKey)
	if err := carolReg.Add(&buddy.Entry{PeerID: alice.ID().String(), PubKey: testutil.MarshaledPubKey(t, alice), Addrs: aliceAddrs}); err != nil {
		t.Fatalf("carolReg.Add: %v", err)
	}

	// --- Buddy stores -------------------------------------------------------
	bobStore := buddy.NewStore(filepath.Join(dir, "bob_shards"))
	carolStore := buddy.NewStore(filepath.Join(dir, "carol_shards"))

	// Register handlers so Bob and Carol accept incoming push streams.
	p2p.RegisterHandlers(bob, bobReg, bobStore, invite.NewManager(filepath.Join(dir, "b_inv.json")))
	p2p.RegisterHandlers(carol, carolReg, carolStore, invite.NewManager(filepath.Join(dir, "c_inv.json")))

	// --- Alice's local shard store (simulates what backup wrote) ------------
	aliceLocalStore, err := storage.New(filepath.Join(dir, "alice_local"))
	if err != nil {
		t.Fatal(err)
	}
	ownerID := alice.ID().String()
	fileID := "rebalance-test-file"

	shards := []struct {
		idx      int
		isParity bool
		data     []byte
	}{
		{0, false, []byte("encrypted-data-shard-zero")},
		{1, false, []byte("encrypted-data-shard-one-")},
		{2, true, []byte("encrypted-parity-shard-xx")},
	}

	for _, s := range shards {
		if err := aliceLocalStore.Put(fileID, s.idx, s.isParity, s.data); err != nil {
			t.Fatalf("local Put shard %d: %v", s.idx, err)
		}
	}

	// Pre-connect Alice to both buddies (mimics what runBackup does).
	if err := alice.Connect(ctx, peer.AddrInfo{ID: bob.ID(), Addrs: bob.Addrs()}); err != nil {
		t.Fatalf("connect alice->bob: %v", err)
	}
	if err := alice.Connect(ctx, peer.AddrInfo{ID: carol.ID(), Addrs: carol.Addrs()}); err != nil {
		t.Fatalf("connect alice->carol: %v", err)
	}

	// --- Run rebalance -------------------------------------------------------
	rb := rebalance.New(ownerID, aliceLocalStore, aliceReg, alice)
	result, err := rb.Run(ctx, []string{fileID})
	if err != nil {
		t.Fatal(err)
	}

	if result.FilesProcessed != 1 {
		t.Errorf("FilesProcessed=%d want 1", result.FilesProcessed)
	}
	// 3 shards x 2 buddies = 6 attempts
	if result.ShardsAttempted != 6 {
		t.Errorf("ShardsAttempted=%d want 6", result.ShardsAttempted)
	}
	if result.ShardsOK != 6 {
		t.Errorf("ShardsOK=%d want 6 (errors: %v)", result.ShardsOK, result.Errors)
	}

	// --- Verify Carol has all shards ----------------------------------------
	for _, s := range shards {
		got, err := carolStore.Get(ownerID, fileID, s.idx)
		if err != nil {
			t.Errorf("carolStore shard %d missing: %v", s.idx, err)
			continue
		}
		if string(got) != string(s.data) {
			t.Errorf("shard %d: got %q want %q", s.idx, got, s.data)
		}
	}
}

// TestRebalanceIdempotent: running rebalance twice should not produce errors on
// the second pass (overwriting with same data is fine).
func TestRebalanceIdempotent(t *testing.T) {
	dir := t.TempDir()

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

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	aliceReg, _ := buddy.NewRegistry(filepath.Join(dir, "alice_reg.enc"), testMasterKey)
	if err := aliceReg.Add(&buddy.Entry{PeerID: bob.ID().String(), PubKey: testutil.MarshaledPubKey(t, bob)}); err != nil {
		t.Fatalf("aliceReg.Add: %v", err)
	}

	bobReg, _ := buddy.NewRegistry(filepath.Join(dir, "bob_reg.enc"), testMasterKey)
	if err := bobReg.Add(&buddy.Entry{PeerID: alice.ID().String(), PubKey: testutil.MarshaledPubKey(t, alice)}); err != nil {
		t.Fatalf("bobReg.Add: %v", err)
	}

	bobStore := buddy.NewStore(filepath.Join(dir, "bob_shards"))
	p2p.RegisterHandlers(bob, bobReg, bobStore, invite.NewManager(filepath.Join(dir, "b_inv.json")))

	aliceLocalStore, err := storage.New(filepath.Join(dir, "alice_local"))
	if err != nil {
		t.Fatal(err)
	}
	ownerID := alice.ID().String()
	fileID := "idempotent-test"

	if err := aliceLocalStore.Put(fileID, 0, false, []byte("shard-zero")); err != nil {
		t.Fatal(err)
	}

	if err := alice.Connect(ctx, peer.AddrInfo{ID: bob.ID(), Addrs: bob.Addrs()}); err != nil {
		t.Fatal(err)
	}

	rb := rebalance.New(ownerID, aliceLocalStore, aliceReg, alice)

	for pass := 1; pass <= 2; pass++ {
		res, err := rb.Run(ctx, []string{fileID})
		if err != nil {
			t.Fatalf("pass %d: %v", pass, err)
		}
		if len(res.Errors) > 0 {
			t.Errorf("pass %d: unexpected errors: %v", pass, res.Errors)
		}
		if res.ShardsOK != 1 {
			t.Errorf("pass %d: ShardsOK=%d want 1", pass, res.ShardsOK)
		}
	}
}

// TestRebalanceNoBuddies: with an empty registry, Run must return cleanly.
func TestRebalanceNoBuddies(t *testing.T) {
	dir := t.TempDir()

	privA, _, _ := libp2pcrypto.GenerateEd25519Key(rand.Reader)
	alice, err := p2p.NewHost(privA, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer alice.Close()

	aliceReg, _ := buddy.NewRegistry(filepath.Join(dir, "alice_reg.enc"), testMasterKey)
	aliceLocalStore, err := storage.New(filepath.Join(dir, "alice_local"))
	if err != nil {
		t.Fatal(err)
	}
	if err := aliceLocalStore.Put("file1", 0, false, []byte("data")); err != nil {
		t.Fatal(err)
	}

	rb := rebalance.New(alice.ID().String(), aliceLocalStore, aliceReg, alice)
	res, err := rb.Run(context.Background(), []string{"file1"})
	if err != nil {
		t.Fatal(err)
	}
	if res.ShardsAttempted != 0 {
		t.Errorf("ShardsAttempted=%d want 0", res.ShardsAttempted)
	}
}
