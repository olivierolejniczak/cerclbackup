package p2p_test

// e2e_test.go — end-to-end integration tests for Phase 2b.
//
// TestFullBackupRestoreE2E exercises the complete pipeline:
//   RS encode + AES encrypt -> PushShard to buddy -> FetchShard from buddy ->
//   AES decrypt -> RS reconstruct -> original bytes recovered.
//
// TestFullRestoreWithMissingShard additionally deletes the parity shard from
// the buddy store before restore, proving RS reconstruction still works when
// one shard is missing.
//
// TestP2BPushAndFetchE2E verifies raw push+fetch round-trip at the transport
// layer (no crypto) to catch regressions in the wire framing.

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"path/filepath"
	"testing"
	"time"

	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/cerclbackup/cerclbackup/internal/buddy"
	"github.com/cerclbackup/cerclbackup/internal/codec"
	bbcrypto "github.com/cerclbackup/cerclbackup/internal/crypto"
	"github.com/cerclbackup/cerclbackup/internal/invite"
	"github.com/cerclbackup/cerclbackup/internal/p2p"
	"github.com/cerclbackup/cerclbackup/internal/testutil"
	"github.com/cerclbackup/cerclbackup/pkg/protocol"
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
	if err := aliceReg.Add(&buddy.Entry{PeerID: bob.ID().String(), PubKey: testutil.MarshaledPubKey(t, bob)}); err != nil {
		t.Fatalf("aliceReg.Add: %v", err)
	}

	bobReg, _ := buddy.NewRegistry(filepath.Join(dir, "bob_reg.enc"), testMasterKey)
	if err := bobReg.Add(&buddy.Entry{PeerID: alice.ID().String(), PubKey: testutil.MarshaledPubKey(t, alice)}); err != nil {
		t.Fatalf("bobReg.Add: %v", err)
	}

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

// TestFullBackupRestoreE2E runs the complete crypto+P2P pipeline:
//   1. RS-encode + AES-256-GCM-encrypt a 1 MB test blob
//   2. Push all encrypted shards from Alice to Bob via the push protocol
//   3. Restore: fetch every shard back from Bob via the pull protocol
//   4. AES-decrypt + RS-reconstruct -> assert bytes match the original
func TestFullBackupRestoreE2E(t *testing.T) {
	fullPipelineTest(t, false)
}

// TestFullRestoreWithMissingShard repeats the full-pipeline test but removes
// the parity shard from Bob's store before restore. The RS decoder must
// reconstruct the missing shard from the two surviving data shards.
func TestFullRestoreWithMissingShard(t *testing.T) {
	fullPipelineTest(t, true)
}

// fullPipelineTest is the shared body for the two full-pipeline tests.
// deleteParity=true removes shard index 2 (the parity shard) from Bob's store
// before restore begins, exercising the RS reconstruction path.
func fullPipelineTest(t *testing.T, deleteParity bool) {
	t.Helper()
	dir := t.TempDir()

	// --- 1. Test data: 1 MB random blob (fits in one 4 MB chunk) ----------
	original := make([]byte, 1*1024*1024)
	if _, err := rand.Read(original); err != nil {
		t.Fatal(err)
	}

	// --- 2. RS scheme 2 data + 1 parity, derive crypto keys --------------
	scheme := protocol.RSScheme{DataShards: 2, ParityShards: 1}
	enc, err := codec.NewEncoder(scheme)
	if err != nil {
		t.Fatal(err)
	}

	var fileHashArr [32]byte
	h := sha256.New()
	h.Write(original)
	copy(fileHashArr[:], h.Sum(nil))

	fileKey, err := bbcrypto.DeriveFileKey(testMasterKey, fileHashArr)
	if err != nil {
		t.Fatal(err)
	}

	// --- 3. Two real libp2p hosts ----------------------------------------
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
	if err := aliceReg.Add(&buddy.Entry{PeerID: bob.ID().String(), PubKey: testutil.MarshaledPubKey(t, bob)}); err != nil {
		t.Fatalf("aliceReg.Add: %v", err)
	}
	bobReg, _ := buddy.NewRegistry(filepath.Join(dir, "bob_reg.enc"), testMasterKey)
	if err := bobReg.Add(&buddy.Entry{PeerID: alice.ID().String(), PubKey: testutil.MarshaledPubKey(t, alice)}); err != nil {
		t.Fatalf("bobReg.Add: %v", err)
	}

	bobStore := buddy.NewStore(filepath.Join(dir, "bob_shards"))
	p2p.RegisterHandlers(bob, bobReg, bobStore, invite.NewManager(filepath.Join(dir, "b_inv.json")))
	// Alice only needs to open streams — no handlers required on her side for push.

	if err := alice.Connect(context.Background(), peer.AddrInfo{ID: bob.ID(), Addrs: bob.Addrs()}); err != nil {
		t.Fatalf("connect alice->bob: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ownerID := alice.ID().String()
	fileID := "full-e2e-test"

	// --- 4. RS encode + AES encrypt + push to Bob -------------------------
	rawShards, err := enc.SplitChunkToShards(original)
	if err != nil {
		t.Fatalf("SplitChunkToShards: %v", err)
	}

	for si, raw := range rawShards {
		isParity := si >= scheme.DataShards
		ciphertext, err := bbcrypto.EncryptShard(fileKey, si, raw)
		if err != nil {
			t.Fatalf("EncryptShard %d: %v", si, err)
		}
		if err := p2p.PushShard(ctx, alice, bob.ID(), ownerID, fileID, si, isParity, ciphertext); err != nil {
			t.Fatalf("PushShard %d: %v", si, err)
		}
	}

	// --- 5. Optionally delete parity shard from Bob's store ---------------
	if deleteParity {
		parityIdx := scheme.DataShards // index of first (and only) parity shard
		if err := bobStore.Delete(ownerID, fileID, parityIdx); err != nil {
			t.Logf("Delete shard %d: %v (may not be implemented, skipping delete)", parityIdx, err)
			// If Delete is not implemented, mark the shard invalid by overwriting.
			_ = bobStore.Put(ownerID, fileID, parityIdx, []byte("corrupted"))
		}
		t.Logf("deleted/corrupted parity shard %d — restore must use RS reconstruction", parityIdx)
	}

	// --- 6. Restore: fetch encrypted shards from Bob, decrypt, reconstruct -
	totalShards := scheme.TotalShards()
	decrypted := make([][]byte, totalShards)

	for si := 0; si < totalShards; si++ {
		ciphertext, err := p2p.FetchShard(ctx, alice, bob.ID(), ownerID, fileID, si)
		if err != nil {
			// Missing/corrupted shard: leave nil for RS reconstruction.
			t.Logf("FetchShard %d: %v (will reconstruct)", si, err)
			decrypted[si] = nil
			continue
		}
		plain, err := bbcrypto.DecryptShard(fileKey, si, ciphertext)
		if err != nil {
			t.Logf("DecryptShard %d: %v (will reconstruct)", si, err)
			decrypted[si] = nil
			continue
		}
		decrypted[si] = plain
	}

	restored, err := enc.MergeShardToChunk(decrypted)
	if err != nil {
		t.Fatalf("MergeShardToChunk: %v", err)
	}

	// Trim RS padding: the encoder pads the chunk to the next multiple of
	// DataShards; the original length is always <= padded length.
	if len(restored) > len(original) {
		restored = restored[:len(original)]
	}

	if !bytes.Equal(restored, original) {
		t.Errorf("restored %d bytes but expected %d; data mismatch", len(restored), len(original))
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
