package manifdist_test

import (
	"bytes"
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
	"github.com/cerclbackup/cerclbackup/internal/manifdist"
	"github.com/cerclbackup/cerclbackup/internal/p2p"
	"github.com/cerclbackup/cerclbackup/internal/testutil"
)

var testMasterKey []byte

func TestMain(m *testing.M) {
	testMasterKey = testutil.RandMasterKey()
	os.Exit(m.Run())
}

func makeHost(t *testing.T) interface {
	Close() error
	ID() peer.ID
} {
	t.Helper()
	priv, _, err := libp2pcrypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	h, err := p2p.NewHost(priv, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.Close() })
	return h
}

func TestPushAndPull(t *testing.T) {
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

	// Bob's registry knows Alice (for checkBuddyAuth).
	alicePub := testutil.MarshaledPubKey(t, alice)
	bobReg, _ := buddy.NewRegistry(filepath.Join(dir, "bob_reg.enc"), testMasterKey)
	if err := bobReg.Add(&buddy.Entry{PeerID: alice.ID().String(), PubKey: alicePub}); err != nil {
		t.Fatalf("bobReg.Add: %v", err)
	}
	bobStore := buddy.NewStore(filepath.Join(dir, "bob_store"))

	p2p.RegisterHandlers(bob, bobReg, bobStore, invite.NewManager(filepath.Join(dir, "bob_inv.json")))

	// Connect Alice → Bob.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := alice.Connect(ctx, peer.AddrInfo{ID: bob.ID(), Addrs: bob.Addrs()}); err != nil {
		t.Fatalf("connect: %v", err)
	}

	// Alice pushes her encrypted manifest blob.
	blob := make([]byte, 512)
	if _, err := rand.Read(blob); err != nil {
		t.Fatal(err)
	}
	ownerID := alice.ID().String()

	if err := manifdist.PushToBuddy(ctx, alice, bob.ID(), ownerID, blob); err != nil {
		t.Fatalf("PushToBuddy: %v", err)
	}

	// Alice retrieves the blob from Bob (simulating recovery).
	got, err := manifdist.PullFromBuddy(ctx, alice, bob.ID(), ownerID)
	if err != nil {
		t.Fatalf("PullFromBuddy: %v", err)
	}
	if !bytes.Equal(got, blob) {
		t.Error("pulled manifest blob differs from pushed blob")
	}
}

func TestPullNotFound(t *testing.T) {
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

	alicePub := testutil.MarshaledPubKey(t, alice)
	bobReg, _ := buddy.NewRegistry(filepath.Join(dir, "bob_reg.enc"), testMasterKey)
	if err := bobReg.Add(&buddy.Entry{PeerID: alice.ID().String(), PubKey: alicePub}); err != nil {
		t.Fatalf("bobReg.Add: %v", err)
	}
	p2p.RegisterHandlers(bob, bobReg, buddy.NewStore(filepath.Join(dir, "bob_store")), invite.NewManager(filepath.Join(dir, "bob_inv.json")))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := alice.Connect(ctx, peer.AddrInfo{ID: bob.ID(), Addrs: bob.Addrs()}); err != nil {
		t.Fatalf("connect: %v", err)
	}

	_, err = manifdist.PullFromBuddy(ctx, alice, bob.ID(), "unknown-owner")
	if err == nil {
		t.Fatal("expected error when buddy has no manifest, got nil")
	}
}

func TestPushUnauthorizedRejected(t *testing.T) {
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

	// Bob's registry is empty — does not know Alice.
	bobReg, _ := buddy.NewRegistry(filepath.Join(dir, "bob_reg.enc"), testMasterKey)
	p2p.RegisterHandlers(bob, bobReg, buddy.NewStore(filepath.Join(dir, "bob_store")), invite.NewManager(filepath.Join(dir, "bob_inv.json")))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := alice.Connect(ctx, peer.AddrInfo{ID: bob.ID(), Addrs: bob.Addrs()}); err != nil {
		t.Fatalf("connect: %v", err)
	}

	blob := []byte("fake manifest")
	err = manifdist.PushToBuddy(ctx, alice, bob.ID(), alice.ID().String(), blob)
	if err == nil {
		t.Fatal("expected push rejection for unknown peer, got nil")
	}
}
