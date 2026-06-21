package p2p

import (
	"crypto/rand"
	"fmt"

	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"

	bbcrypto "github.com/cerclbackup/cerclbackup/internal/crypto"
)

const peerKeyName = "peer_privkey"

// EnsurePeerIdentity loads or creates the Ed25519 peer key from the keystore.
// The serialised key is stored as an extra entry in the Argon2id-protected
// keystore alongside the master key.
func EnsurePeerIdentity(ks *bbcrypto.Keystore, password string) (libp2pcrypto.PrivKey, error) {
	raw := ks.LoadExtra(peerKeyName)
	if len(raw) > 0 {
		priv, err := libp2pcrypto.UnmarshalPrivateKey(raw)
		if err != nil {
			return nil, fmt.Errorf("p2p: unmarshal peer key: %w", err)
		}
		return priv, nil
	}

	// Generate a fresh Ed25519 key
	priv, _, err := libp2pcrypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("p2p: generate peer key: %w", err)
	}
	raw, err = libp2pcrypto.MarshalPrivateKey(priv)
	if err != nil {
		return nil, fmt.Errorf("p2p: marshal peer key: %w", err)
	}
	if err := ks.StoreExtra(peerKeyName, raw, password); err != nil {
		return nil, fmt.Errorf("p2p: store peer key: %w", err)
	}
	return priv, nil
}
