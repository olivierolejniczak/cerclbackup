package p2p

import (
	"fmt"

	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"

	bbcrypto "github.com/cerclbackup/cerclbackup/internal/crypto"
	"github.com/cerclbackup/cerclbackup/internal/identity"
)

const peerKeyName = "peer_privkey"

// EnsurePeerIdentity loads or creates the Ed25519 peer key from the keystore.
//
// On existing keystores (created before Phase 2g) the key is loaded verbatim.
// On fresh keystores a random 16-byte seed is generated, stored as
// identity.KeyName in the keystore extras, and the Ed25519 key is derived
// deterministically from it — enabling future recovery via show-phrase /
// recover commands.
func EnsurePeerIdentity(ks *bbcrypto.Keystore, password string) (libp2pcrypto.PrivKey, error) {
	raw := ks.LoadExtra(peerKeyName)
	if len(raw) > 0 {
		priv, err := libp2pcrypto.UnmarshalPrivateKey(raw)
		if err != nil {
			return nil, fmt.Errorf("p2p: unmarshal peer key: %w", err)
		}
		return priv, nil
	}

	// Fresh install: generate identity seed, derive key deterministically.
	seed, _, err := identity.Generate()
	if err != nil {
		return nil, fmt.Errorf("p2p: generate identity seed: %w", err)
	}
	if err := ks.StoreExtra(identity.KeyName, seed, password); err != nil {
		return nil, fmt.Errorf("p2p: store identity seed: %w", err)
	}

	priv, err := identity.DerivePrivKey(seed)
	if err != nil {
		return nil, err
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

// EnsurePeerIdentityFromSeed is used during recovery: it derives the Ed25519
// key from the provided 16-byte seed (decoded from a recovery phrase) and
// stores both the seed and derived key in the keystore.  Any previously stored
// identity is overwritten.
func EnsurePeerIdentityFromSeed(ks *bbcrypto.Keystore, seed []byte, password string) (libp2pcrypto.PrivKey, error) {
	priv, err := identity.DerivePrivKey(seed)
	if err != nil {
		return nil, err
	}
	raw, err := libp2pcrypto.MarshalPrivateKey(priv)
	if err != nil {
		return nil, fmt.Errorf("p2p: marshal peer key: %w", err)
	}
	if err := ks.StoreExtra(identity.KeyName, seed, password); err != nil {
		return nil, fmt.Errorf("p2p: store identity seed: %w", err)
	}
	if err := ks.StoreExtra(peerKeyName, raw, password); err != nil {
		return nil, fmt.Errorf("p2p: store peer key: %w", err)
	}
	return priv, nil
}
