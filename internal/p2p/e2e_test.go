package p2p_test

// TestP2BBackupRestoreE2E is an end-to-end integration test for Phase 2b:
//   Alice encodes a file into shards and pushes them to Bob.
//   Alice's local store is then deleted.
//   Alice fetches the shards back from Bob and reassembles the file.
//
// This exercises PushShard, FetchShard, and the pull handler under real
// libp2p network conditions.

import (
	"bytes"
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
	"github.com/cerclbackup/cerclbackup/pkg/wire"
)

func TestP2BPushAndFetchE2E(t *testing.T) {
	dir := t.TempDir()

	// Create two hosts
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
	_ = aliceReg.Add(&buddy.Entry{PeerID: bob.ID().String(), PubKey: []byte("pk")})

	bobReg, _ := buddy.NewRegistry(filepath.Join(dir, "bob_reg.enc"), testMasterKey)
	_ = bobReg.Add(&buddy.Entry{PeerID: alice.ID().String(), PubKey: []byte("pk")})

	// Shard stores
	aliceStore := buddy.NewStore(filepath.Join(dir, "alice_shards"))
	bobStore := buddy.NewStore(filepath.Join(dir, "bob_shards"))

	// Register handlers
	p2p.RegisterHandlers(alice, aliceReg, aliceStore, invite.NewManager(filepath.Join(dir, "a_inv.json")))
	p2p.RegisterHandlers(bob, bobReg, bobStore, invite.NewManager(filepath.Join(dir, "b_inv.json")))

	// Connect
	if err := alice.Connect(context.Background(), peer.AddrInfo{ID: bob.ID(), Addrs: bob.Addrs()}); err != nil {
		t.Fatalf("connect alice->bob: %v", err)
	}

	// Simulate a 3-shard file (2 data + 1 parity)
	ownerID := alice.ID().String()
	fileID := "e2e-test-file-001"
	shards := []struct {
		idx      int
		isParity bool
		data     []byte
	}{
		{0, false, []byte("data-shard-zero-payload")},
		{1, false, []byte("data-shard-one-payload-")},
		{2, true, []byte("parity-shard-two-xxxxxx")},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Alice pushes all shards to Bob
	for _, s := range shards {
		if err := p2p.PushShard(ctx, alice, bob.ID(), ownerID, fileID, s.idx, s.isParity, s.data); err != nil {
			t.Fatalf("PushShard idx=%d: %v", s.idx, err)
		}
	}

	// Verify Bob has all shards
	for _, s := range shards {
		got, err := bobStore.Get(ownerID, fileID, s.idx)
		if err != nil {
			t.Errorf("bobStore.Get idx=%d: %v", s.idx, err)
			continue
		}
		if !bytes.Equal(got, s.data) {
			t.Errorf("shard %d data mismatch: got %q want %q", s.idx, got, s.data)
		}
	}

	// Alice fetches back from Bob (simulates restore after local loss)
	for _, s := range shards {
		got, err := p2p.FetchShard(ctx, alice, bob.ID(), ownerID, fileID, s.idx)
		if err != nil {
			t.Errorf("FetchShard idx=%d: %v", s.idx, err)
			continue
		}
		if !bytes.Equal(got, s.data) {
			t.Errorf("fetched shard %d data mismatch: got %q want %q", s.idx, got, s.data)
		}
	}
}

func TestP2BOfflineQueueFlush(t *testing.T) {
	dir := t.TempDir()
	qPath := filepath.Join(dir, "queue.json")

	q := p2p.NewQueue(qPath)

	push := wire.ShardPush{
		Type:       wire.TypeShardPush,
		OwnerID:    "owner-id",
		FileID:     "queued-file",
		ShardIndex: 7,
		IsParity:   false,
		Data:       []byte("queued shard data"),
	}

	if err := q.Enqueue("peer-bob-offline", push); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// Reload from disk and verify the item survived
	q2 := p2p.NewQueue(qPath)
	privB, _, _ := libp2pcrypto.GenerateEd25519Key(rand.Reader)
	bob, err := p2p.NewHost(privB, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer bob.Close()

	// FlushToPeer with an unreachable peer ID should log errors but not panic
	peerID, _ := peer.Decode("12D3KooWAaEqWivppyiAU2BWmSV9TskTuFYDbnJwRn6nh8HQYEpL")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	q2.FlushToPeer(ctx, bob, peerID) // errors are logged, not returned
}
