//go:build integration

// Package main_test contains integration tests that require a running environment.
//
// Run with:
//
//	go test -tags integration -v -run TestPeerConnectivity ./cmd/cerclbackup/
package main_test

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/cerclbackup/cerclbackup/internal/buddy"
	bbcrypto "github.com/cerclbackup/cerclbackup/internal/crypto"
	p2pmod "github.com/cerclbackup/cerclbackup/internal/p2p"
)

const peerTestPassword = "integration-test-pass-x7"

// peerNode describes one isolated CerclBackup instance.
type peerNode struct {
	dir        string // CERCLBACKUP_CONFIG_DIR
	p2pPort    int
	healthPort int
}

// TestPeerConnectivity proves the full P2P backup/restore cycle end-to-end:
//
//  1. nodeA backs up a file; shards are pushed to three peer serve daemons.
//  2. nodeA's local shard store is wiped (simulates disk loss).
//  3. nodeA restores the file, fetching every shard from peers over P2P.
//  4. Restored bytes match the original exactly.
//
// The minimum RS scheme requires 3 buddies; the test therefore starts three
// independent serve daemons (B, C, D) and cross-registers all of them with
// nodeA before running backup and restore.
func TestPeerConnectivity(t *testing.T) {
	bin := buildTestBinary(t)

	// Create four isolated config environments.
	nodeA := &peerNode{dir: t.TempDir()} // client only (no serve needed)
	nodeB := &peerNode{dir: t.TempDir(), p2pPort: 17743, healthPort: 17783}
	nodeC := &peerNode{dir: t.TempDir(), p2pPort: 17744, healthPort: 17784}
	nodeD := &peerNode{dir: t.TempDir(), p2pPort: 17745, healthPort: 17785}
	peers := []*peerNode{nodeB, nodeC, nodeD}

	// Init all four keystores.
	t.Log("initialising keystores")
	for _, n := range append([]*peerNode{nodeA}, peers...) {
		cliRun(t, bin, n.dir, "init", "--force", "--password", peerTestPassword)
	}

	// Cross-register: nodeA learns all three peers; each peer learns nodeA.
	t.Log("cross-registering buddies")
	crossRegisterN(t, nodeA, peers, peerTestPassword)

	// Start serve daemons for B, C and D.
	t.Log("starting peer serve daemons (B, C, D)")
	for _, n := range peers {
		startServe(t, bin, n)
	}
	for _, n := range peers {
		waitHealth(t, fmt.Sprintf("http://127.0.0.1:%d/health", n.healthPort), 15*time.Second)
	}
	t.Log("all peer serve daemons ready")

	// Create the test payload and back it up from nodeA.
	// --buddies 3 matches the number of registered peers and selects an RS
	// scheme that is exactly recoverable from any one peer (all shards are
	// pushed to every buddy).
	srcDir := t.TempDir()
	srcFile := filepath.Join(srcDir, "secret.dat")
	payload := []byte("peer-connectivity-test-payload-cerclbackup-1234567890-abcdef")
	if err := os.WriteFile(srcFile, payload, 0o600); err != nil {
		t.Fatal(err)
	}

	t.Log("backing up from nodeA (--buddies 3)")
	cliRun(t, bin, nodeA.dir, "backup",
		"--src", srcDir,
		"--password", peerTestPassword,
		"--buddies", "3",
	)

	// Confirm that nodeB received at least one shard from the push.
	t.Log("waiting for shards in nodeB store (push confirmation)")
	waitShards(t, filepath.Join(nodeB.dir, "shards"), 15*time.Second)

	// Simulate complete disk loss on nodeA.
	t.Log("wiping nodeA local shard store")
	if err := os.RemoveAll(filepath.Join(nodeA.dir, "shards")); err != nil {
		t.Fatal(err)
	}

	// Restore on nodeA: every shard must come from a peer over P2P.
	restoreDir := t.TempDir()
	restoreFile := filepath.Join(restoreDir, "secret.dat")
	t.Log("restoring on nodeA (all shards fetched from peers)")
	cliRun(t, bin, nodeA.dir, "restore",
		"--file", srcFile,
		"--password", peerTestPassword,
		"--out", restoreFile,
	)

	// Byte-exact verification.
	got, err := os.ReadFile(restoreFile)
	if err != nil {
		t.Fatalf("restored file missing: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("content mismatch:\n  want %q\n   got %q", payload, got)
	}
	t.Log("PASS: file restored byte-exactly via P2P after local store wipe")
}

// ── setup helpers ─────────────────────────────────────────────────────────────

// crossRegisterN registers every peer in nodeA's buddy registry and registers
// nodeA in each peer's buddy registry.
func crossRegisterN(t *testing.T, nodeA *peerNode, peers []*peerNode, password string) {
	t.Helper()

	ksA := openKS(t, nodeA.dir, password)
	privA, err := p2pmod.EnsurePeerIdentity(ksA, password)
	if err != nil {
		t.Fatalf("peer identity A: %v", err)
	}
	idA, pubA := marshalPeer(t, privA)
	t.Logf("nodeA peer ID: %s", idA)

	for _, p := range peers {
		ksPeer := openKS(t, p.dir, password)
		privPeer, err := p2pmod.EnsurePeerIdentity(ksPeer, password)
		if err != nil {
			t.Fatalf("peer identity %s: %v", p.dir, err)
		}
		idPeer, pubPeer := marshalPeer(t, privPeer)
		t.Logf("peer %d ID: %s", p.p2pPort, idPeer)

		// Register this peer in A's registry.
		addBuddy(t, nodeA.dir, ksA.MasterKey(), &buddy.Entry{
			PeerID: idPeer,
			PubKey: pubPeer,
			Addrs:  []string{fmt.Sprintf("/ip4/127.0.0.1/tcp/%d/p2p/%s", p.p2pPort, idPeer)},
		})

		// Register A in this peer's registry.
		addBuddy(t, p.dir, ksPeer.MasterKey(), &buddy.Entry{
			PeerID: idA,
			PubKey: pubA,
		})
	}
}

func openKS(t *testing.T, cfgDir, password string) *bbcrypto.Keystore {
	t.Helper()
	ks := bbcrypto.NewKeystore(filepath.Join(cfgDir, "keystore.enc"))
	if err := ks.Unlock(password); err != nil {
		t.Fatalf("open keystore in %s: %v", cfgDir, err)
	}
	return ks
}

func marshalPeer(t *testing.T, priv libp2pcrypto.PrivKey) (peerID string, pubBytes []byte) {
	t.Helper()
	pub := priv.GetPublic()
	b, err := libp2pcrypto.MarshalPublicKey(pub)
	if err != nil {
		t.Fatalf("marshal pubkey: %v", err)
	}
	id, err := peer.IDFromPublicKey(pub)
	if err != nil {
		t.Fatalf("peer ID from pubkey: %v", err)
	}
	return id.String(), b
}

func addBuddy(t *testing.T, cfgDir string, masterKey []byte, entry *buddy.Entry) {
	t.Helper()
	reg, err := buddy.NewRegistry(filepath.Join(cfgDir, "buddies.enc"), masterKey)
	if err != nil {
		t.Fatalf("open registry in %s: %v", cfgDir, err)
	}
	if err := reg.Add(entry); err != nil {
		t.Fatalf("registry.Add %s: %v", entry.PeerID, err)
	}
}

// ── process helpers ───────────────────────────────────────────────────────────

// startServe launches cerclbackup serve for the given node and registers a
// cleanup hook to kill it when the test ends.
func startServe(t *testing.T, bin string, n *peerNode) {
	t.Helper()
	cmd := exec.Command(bin,
		"serve",
		"--password", peerTestPassword,
		"--port", fmt.Sprintf("%d", n.p2pPort),
		"--health-addr", fmt.Sprintf("127.0.0.1:%d", n.healthPort),
	)
	cmd.Env = append(os.Environ(), "CERCLBACKUP_CONFIG_DIR="+n.dir)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start serve (port %d): %v", n.p2pPort, err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
	})
}

// waitHealth polls /health until HTTP 200 or deadline.
func waitHealth(t *testing.T, url string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url) //nolint:noctx
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("serve did not become healthy at %s within %s", url, timeout)
}

// waitShards polls until at least one shard file appears in the store dir.
func waitShards(t *testing.T, dir string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		entries, _ := filepath.Glob(filepath.Join(dir, "*", "*"))
		if len(entries) > 0 {
			t.Logf("shard store %s: %d file(s)", dir, len(entries))
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("no shards appeared in %s within %s — push may have failed", dir, timeout)
}

// cliRun runs a cerclbackup command with the given config dir and fatals on
// non-zero exit.
func cliRun(t *testing.T, bin, cfgDir string, args ...string) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), "CERCLBACKUP_CONFIG_DIR="+cfgDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("cerclbackup %v failed: %v\n%s", args, err, out)
	}
	if len(out) > 0 {
		t.Logf("$ cerclbackup %v\n%s", args, out)
	}
}

// buildTestBinary compiles the cerclbackup CLI into a temp dir.
func buildTestBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "cerclbackup")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("build cerclbackup: %v\n%s", err, out)
	}
	return bin
}
