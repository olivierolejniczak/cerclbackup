package p2p_test

import (
	"context"
	"crypto/rand"
	"path/filepath"
	"testing"
	"time"

	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/cerclbackup/cerclbackup/internal/buddy"
	"github.com/cerclbackup/cerclbackup/internal/invite"
	"github.com/cerclbackup/cerclbackup/internal/p2p"
	"github.com/cerclbackup/cerclbackup/pkg/wire"
)

// TestMDNSConnectsKnownPeer verifies that HandlePeerFound (the mDNS callback)
// connects to a known buddy and updates the stored addresses.
func TestMDNSConnectsKnownPeer(t *testing.T) {
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

	// Alice's registry knows Bob.
	aliceReg, _ := buddy.NewRegistry(filepath.Join(dir, "alice_reg.enc"), testMasterKey)
	_ = aliceReg.Add(&buddy.Entry{PeerID: bob.ID().String(), PubKey: []byte("pk")})

	bobStore := buddy.NewStore(filepath.Join(dir, "bob_shards"))
	bobReg, _ := buddy.NewRegistry(filepath.Join(dir, "bob_reg.enc"), testMasterKey)
	_ = bobReg.Add(&buddy.Entry{PeerID: alice.ID().String(), PubKey: []byte("pk")})
	p2p.RegisterHandlers(bob, bobReg, bobStore, invite.NewManager(filepath.Join(dir, "b_inv.json")))

	// Start mDNS on Alice with no queue (just testing connect behaviour).
	svc, err := p2p.StartMDNS(alice, aliceReg, nil)
	if err != nil {
		t.Fatalf("StartMDNS: %v", err)
	}
	defer svc.Close()

	// Simulate mDNS advertising Bob's addresses by calling the notifee directly
	// via PeerFound -- we expose a helper that fires the callback.
	p2p.SimulatePeerFound(alice, aliceReg, nil, peer.AddrInfo{
		ID:    bob.ID(),
		Addrs: bob.Addrs(),
	})

	// Give the async connect a moment to complete.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if alice.Network().Connectedness(bob.ID()) == network.Connected {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if alice.Network().Connectedness(bob.ID()) != network.Connected {
		t.Fatal("alice should be connected to bob after HandlePeerFound")
	}

	// UpdateAddrs should have persisted Bob's addresses.
	entry, ok := aliceReg.Get(bob.ID().String())
	if !ok {
		t.Fatal("Bob not in registry after connect")
	}
	if len(entry.Addrs) == 0 {
		t.Error("Bob's addresses not persisted by UpdateAddrs")
	}
}

// TestMDNSIgnoresUnknownPeer verifies that HandlePeerFound does NOT connect to
// a peer that is not in the buddy registry.
func TestMDNSIgnoresUnknownPeer(t *testing.T) {
	dir := t.TempDir()

	privA, _, _ := libp2pcrypto.GenerateEd25519Key(rand.Reader)
	privC, _, _ := libp2pcrypto.GenerateEd25519Key(rand.Reader)

	alice, err := p2p.NewHost(privA, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer alice.Close()

	carol, err := p2p.NewHost(privC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer carol.Close()

	// Empty registry -- Carol is not a buddy.
	aliceReg, _ := buddy.NewRegistry(filepath.Join(dir, "alice_reg.enc"), testMasterKey)

	p2p.SimulatePeerFound(alice, aliceReg, nil, peer.AddrInfo{
		ID:    carol.ID(),
		Addrs: carol.Addrs(),
	})

	time.Sleep(100 * time.Millisecond)

	if alice.Network().Connectedness(carol.ID()) == network.Connected {
		t.Error("alice should NOT connect to unknown peer Carol")
	}
}

// TestMDNSFlushesQueueOnConnect verifies that when a known buddy is found on
// the LAN, queued shards are pushed to them automatically.
func TestMDNSFlushesQueueOnConnect(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

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

	aliceReg, _ := buddy.NewRegistry(filepath.Join(dir, "alice_reg.enc"), testMasterKey)
	_ = aliceReg.Add(&buddy.Entry{PeerID: bob.ID().String(), PubKey: []byte("pk")})

	bobStore := buddy.NewStore(filepath.Join(dir, "bob_shards"))
	bobReg, _ := buddy.NewRegistry(filepath.Join(dir, "bob_reg.enc"), testMasterKey)
	_ = bobReg.Add(&buddy.Entry{PeerID: alice.ID().String(), PubKey: []byte("pk")})
	p2p.RegisterHandlers(bob, bobReg, bobStore, invite.NewManager(filepath.Join(dir, "b_inv.json")))

	// Enqueue a shard for Bob while Bob is "offline".
	qPath := filepath.Join(dir, "queue.json")
	q := p2p.NewQueue(qPath)
	push := wire.ShardPush{
		Type:       wire.TypeShardPush,
		OwnerID:    alice.ID().String(),
		FileID:     "mdns-queued-file",
		ShardIndex: 0,
		IsParity:   false,
		Data:       []byte("shard from offline queue"),
	}
	if err := q.Enqueue(bob.ID().String(), push); err != nil {
		t.Fatal(err)
	}

	// Simulate Bob appearing on the LAN (mDNS fires HandlePeerFound).
	p2p.SimulatePeerFound(alice, aliceReg, q, peer.AddrInfo{
		ID:    bob.ID(),
		Addrs: bob.Addrs(),
	})

	// Wait for the queue flush to deliver the shard.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if bobStore.Has(alice.ID().String(), "mdns-queued-file", 0) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if !bobStore.Has(alice.ID().String(), "mdns-queued-file", 0) {
		t.Error("queued shard was not delivered to Bob after mDNS discovery")
	}

	got, _ := bobStore.Get(alice.ID().String(), "mdns-queued-file", 0)
	if string(got) != string(push.Data) {
		t.Errorf("shard data %q, want %q", got, push.Data)
	}

	_ = ctx
}
