package p2p_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/cerclbackup/cerclbackup/internal/buddy"
	"github.com/cerclbackup/cerclbackup/internal/p2p"
	"github.com/cerclbackup/cerclbackup/internal/testutil"
)

// TestDHTBootstrapSmoke verifies StartDHT returns without error on a real host.
// It does NOT require Internet access — Bootstrap() is non-blocking and any
// failed outbound bootstrap connections are logged, not returned as errors.
func TestDHTBootstrapSmoke(t *testing.T) {
	priv, _, _ := crypto.GenerateEd25519Key(nil)
	h, err := p2p.NewHost(priv, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	d, err := p2p.StartDHT(ctx, h)
	if err != nil {
		t.Fatalf("StartDHT: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("DHT.Close: %v", err)
	}
}

// TestDialBuddyStoredAddrs verifies that DialBuddy connects via stored addresses
// without touching the DHT (nil dht path, local in-process peers).
func TestDialBuddyStoredAddrs(t *testing.T) {
	dir := t.TempDir()

	priv1, _, _ := crypto.GenerateEd25519Key(nil)
	priv2, _, _ := crypto.GenerateEd25519Key(nil)

	alice, err := p2p.NewHost(priv1, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer alice.Close()

	bob, err := p2p.NewHost(priv2, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer bob.Close()

	// Alice's registry knows Bob with his current listen addresses.
	aliceReg, _ := buddy.NewRegistry(filepath.Join(dir, "alice_reg.enc"), testMasterKey)
	bobAddrs := make([]string, 0, len(bob.Addrs()))
	for _, a := range bob.Addrs() {
		bobAddrs = append(bobAddrs, a.String())
	}
	bobPub := testutil.MarshaledPubKey(t, bob)
	if err := aliceReg.Add(&buddy.Entry{
		PeerID:       bob.ID().String(),
		PubKey:       bobPub,
		FriendlyName: "Bob",
		Addrs:        bobAddrs,
	}); err != nil {
		t.Fatalf("aliceReg.Add: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// nil DHT — must succeed with stored addrs alone.
	if err := p2p.DialBuddy(ctx, alice, nil, aliceReg, bob.ID()); err != nil {
		t.Fatalf("DialBuddy: %v", err)
	}

	if len(alice.Network().ConnsToPeer(bob.ID())) == 0 {
		t.Fatal("expected connection to Bob")
	}
}

// TestDialBuddyUnregisteredPeer verifies DialBuddy returns an error when the
// target peer is not in the registry.
func TestDialBuddyUnregisteredPeer(t *testing.T) {
	dir := t.TempDir()

	priv, _, _ := crypto.GenerateEd25519Key(nil)
	alice, err := p2p.NewHost(priv, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer alice.Close()

	aliceReg, _ := buddy.NewRegistry(filepath.Join(dir, "alice_reg.enc"), testMasterKey)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Fabricate a peer ID not in the registry.
	priv2, _, _ := crypto.GenerateEd25519Key(nil)
	pub2 := priv2.GetPublic()
	unknownID, _ := peer.IDFromPublicKey(pub2)

	if err := p2p.DialBuddy(ctx, alice, nil, aliceReg, unknownID); err == nil {
		t.Fatal("expected error for unregistered peer, got nil")
	}
}
