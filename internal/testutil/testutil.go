// Package testutil provides shared helpers for CerclBackup test packages.
package testutil

import (
	"crypto/rand"
	"testing"

	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
)

// RandKey returns a cryptographically random 32-byte key for use as a test
// master key.  Each call returns a fresh key so tests are isolated.
func RandKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("testutil.RandKey: %v", err)
	}
	return key
}

// RandMasterKey is like RandKey but panics instead of calling t.Fatal.
// Use in TestMain where *testing.T is not available.
func RandMasterKey() []byte {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic("testutil.RandMasterKey: " + err.Error())
	}
	return key
}

// MarshaledPubKey returns the libp2p-marshaled public key of h, suitable for
// storing in a buddy.Entry.PubKey field.  Fatals the test on error.
func MarshaledPubKey(t *testing.T, h host.Host) []byte {
	t.Helper()
	pub := h.Peerstore().PubKey(h.ID())
	if pub == nil {
		t.Fatalf("testutil.MarshaledPubKey: no pubkey in peerstore for %s", h.ID())
	}
	b, err := libp2pcrypto.MarshalPublicKey(pub)
	if err != nil {
		t.Fatalf("testutil.MarshaledPubKey: %v", err)
	}
	return b
}
