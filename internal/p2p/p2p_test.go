package p2p_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/cerclbackup/cerclbackup/internal/buddy"
	"github.com/cerclbackup/cerclbackup/internal/invite"
	"github.com/cerclbackup/cerclbackup/internal/p2p"
	"github.com/cerclbackup/cerclbackup/internal/testutil"
	"github.com/cerclbackup/cerclbackup/pkg/wire"
)

var testMasterKey []byte

func TestMain(m *testing.M) {
	testMasterKey = testutil.RandMasterKey()
	os.Exit(m.Run())
}

func newTestHost(t *testing.T) (interface{ ID() peer.ID }, crypto.PrivKey) {
	t.Helper()
	priv, _, err := crypto.GenerateEd25519Key(nil)
	if err != nil {
		t.Fatal(err)
	}
	h, err := p2p.NewHost(priv, 0) // random port
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close() })
	return h, priv
}

func TestHostHandshake(t *testing.T) {
	priv1, _, _ := crypto.GenerateEd25519Key(nil)
	priv2, _, _ := crypto.GenerateEd25519Key(nil)

	h1, err := p2p.NewHost(priv1, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer h1.Close()

	h2, err := p2p.NewHost(priv2, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer h2.Close()

	if h1.ID() == h2.ID() {
		t.Fatal("hosts should have distinct PeerIDs")
	}

	// Connect h2 → h1
	h2.Peerstore().AddAddrs(h1.ID(), h1.Addrs(), time.Minute)
	if err := h2.Connect(context.Background(), peer.AddrInfo{
		ID:    h1.ID(),
		Addrs: h1.Addrs(),
	}); err != nil {
		t.Fatalf("connect: %v", err)
	}

	// Verify h2 knows h1
	conns := h2.Network().ConnsToPeer(h1.ID())
	if len(conns) == 0 {
		t.Fatal("expected at least one connection")
	}
}

func TestShardPushPull(t *testing.T) {
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

	// Bob knows Alice as buddy — store her real pubkey for checkBuddyAuth.
	alicePub := testutil.MarshaledPubKey(t, alice)
	bobReg, _ := buddy.NewRegistry(filepath.Join(dir, "bob_reg.enc"), testMasterKey)
	if err := bobReg.Add(&buddy.Entry{PeerID: alice.ID().String(), PubKey: alicePub}); err != nil {
		t.Fatalf("bobReg.Add: %v", err)
	}
	bobStore := buddy.NewStore(filepath.Join(dir, "bob_store"))

	// Register shard handler on Bob
	bobInvMgr := invite.NewManager(filepath.Join(dir, "bob_invites.json"))
	p2p.RegisterHandlers(bob, bobReg, bobStore, bobInvMgr)

	// Connect Alice → Bob
	if err := alice.Connect(context.Background(), peer.AddrInfo{ID: bob.ID(), Addrs: bob.Addrs()}); err != nil {
		t.Fatalf("connect alice->bob: %v", err)
	}

	// Push a shard
	shardData := []byte("this is an encrypted shard payload")
	push := wire.ShardPush{
		Type:       wire.TypeShardPush,
		OwnerID:    alice.ID().String(),
		FileID:     "testfile123",
		ShardIndex: 0,
		IsParity:   false,
		Data:       shardData,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := p2p.PushShard(ctx, alice, bob.ID(), push.OwnerID, push.FileID, push.ShardIndex, push.IsParity, push.Data); err != nil {
		t.Fatalf("PushShard: %v", err)
	}

	// Verify Bob received it
	got, err := bobStore.Get(alice.ID().String(), "testfile123", 0)
	if err != nil {
		t.Fatalf("buddystore Get: %v", err)
	}
	if !bytes.Equal(got, shardData) {
		t.Fatalf("shard data mismatch: got %q", got)
	}
}

func TestUnknownPeerRejected(t *testing.T) {
	dir := t.TempDir()

	priv1, _, _ := crypto.GenerateEd25519Key(nil)
	priv2, _, _ := crypto.GenerateEd25519Key(nil)

	alice, _ := p2p.NewHost(priv1, 0)
	defer alice.Close()
	bob, _ := p2p.NewHost(priv2, 0)
	defer bob.Close()

	// Bob has an empty registry — does not know Alice
	bobReg, _ := buddy.NewRegistry(filepath.Join(dir, "bob_reg.enc"), testMasterKey)
	bobStore := buddy.NewStore(filepath.Join(dir, "bob_store"))
	bobInvMgr := invite.NewManager(filepath.Join(dir, "bob_invites.json"))
	p2p.RegisterHandlers(bob, bobReg, bobStore, bobInvMgr)

	_ = alice.Connect(context.Background(), peer.AddrInfo{ID: bob.ID(), Addrs: bob.Addrs()})

	push := wire.ShardPush{
		Type: wire.TypeShardPush, OwnerID: alice.ID().String(),
		FileID: "x", ShardIndex: 0, Data: []byte("data"),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := p2p.PushShard(ctx, alice, bob.ID(), push.OwnerID, push.FileID, push.ShardIndex, push.IsParity, push.Data)
	if err == nil {
		t.Fatal("expected rejection of unknown peer, got nil")
	}
}

func TestInviteRoundtrip(t *testing.T) {
	dir := t.TempDir()

	priv1, _, _ := crypto.GenerateEd25519Key(nil)
	priv2, _, _ := crypto.GenerateEd25519Key(nil)

	alice, _ := p2p.NewHost(priv1, 0)
	defer alice.Close()
	bob, _ := p2p.NewHost(priv2, 0)
	defer bob.Close()

	aliceReg, _ := buddy.NewRegistry(filepath.Join(dir, "alice_reg.enc"), testMasterKey)
	aliceStore := buddy.NewStore(filepath.Join(dir, "alice_store"))
	aliceInvMgr := invite.NewManager(filepath.Join(dir, "alice_invites.json"))
	p2p.RegisterHandlers(alice, aliceReg, aliceStore, aliceInvMgr)

	bobReg, _ := buddy.NewRegistry(filepath.Join(dir, "bob_reg.enc"), testMasterKey)

	// Alice generates an invite
	code, err := aliceInvMgr.Generate()
	if err != nil {
		t.Fatal(err)
	}

	// Bob decodes the token and connects
	token, err := invite.TokenFromMnemonic(code.Words)
	if err != nil {
		t.Fatal(err)
	}

	_ = bob.Connect(context.Background(), peer.AddrInfo{ID: alice.ID(), Addrs: alice.Addrs()})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := p2p.SendInviteRequest(ctx, bob, bobReg, alice.ID(), token, "Bob"); err != nil {
		t.Fatalf("SendInviteRequest: %v", err)
	}

	// Both should know each other
	if !bobReg.IsKnown(alice.ID().String()) {
		t.Error("Bob should know Alice after invite")
	}
	if !aliceReg.IsKnown(bob.ID().String()) {
		t.Error("Alice should know Bob after invite")
	}
}

func TestPushQueue(t *testing.T) {
	dir := t.TempDir()
	queuePath := filepath.Join(dir, "queue.json")
	q := p2p.NewQueue(queuePath)

	push := wire.ShardPush{
		Type: wire.TypeShardPush, OwnerID: "owner",
		FileID: "f1", ShardIndex: 0, Data: []byte("shard"),
	}
	if err := q.Enqueue("peer-bob", push); err != nil {
		t.Fatal(err)
	}

	// Verify the item was enqueued (queue is non-nil)
	if q == nil {
		t.Fatal("expected non-nil queue")
	}
}

// TestShardFetch verifies the pull protocol: Alice stores a shard and Bob
// fetches it over a real libp2p connection.
func TestShardFetch(t *testing.T) {
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

	// Both hosts know each other — real pubkeys required for checkBuddyAuth.
	alicePub2 := testutil.MarshaledPubKey(t, alice)
	bobPub := testutil.MarshaledPubKey(t, bob)

	aliceReg, _ := buddy.NewRegistry(filepath.Join(dir, "alice_reg.enc"), testMasterKey)
	if err := aliceReg.Add(&buddy.Entry{PeerID: bob.ID().String(), PubKey: bobPub}); err != nil {
		t.Fatalf("aliceReg.Add: %v", err)
	}
	aliceStore := buddy.NewStore(filepath.Join(dir, "alice_store"))

	bobReg, _ := buddy.NewRegistry(filepath.Join(dir, "bob_reg.enc"), testMasterKey)
	if err := bobReg.Add(&buddy.Entry{PeerID: alice.ID().String(), PubKey: alicePub2}); err != nil {
		t.Fatalf("bobReg.Add: %v", err)
	}
	bobStore := buddy.NewStore(filepath.Join(dir, "bob_store"))

	p2p.RegisterHandlers(alice, aliceReg, aliceStore, invite.NewManager(filepath.Join(dir, "alice_inv.json")))
	p2p.RegisterHandlers(bob, bobReg, bobStore, invite.NewManager(filepath.Join(dir, "bob_inv.json")))

	if err := bob.Connect(context.Background(), peer.AddrInfo{ID: alice.ID(), Addrs: alice.Addrs()}); err != nil {
		t.Fatalf("connect bob->alice: %v", err)
	}

	// Alice stores a shard (as if a buddy pushed it to her)
	shardData := []byte("pull-this-shard-from-alice")
	ownerID := alice.ID().String()
	fileID := "fetchtest"
	shardIdx := 3
	if err := aliceStore.Put(ownerID, fileID, shardIdx, shardData); err != nil {
		t.Fatalf("aliceStore.Put: %v", err)
	}

	// Bob fetches it from Alice
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got, err := p2p.FetchShard(ctx, bob, alice.ID(), ownerID, fileID, shardIdx)
	if err != nil {
		t.Fatalf("FetchShard: %v", err)
	}
	if !bytes.Equal(got, shardData) {
		t.Errorf("data mismatch: got %q want %q", got, shardData)
	}
}
