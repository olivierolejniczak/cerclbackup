// Package identity handles deterministic peer identity from a BIP39 recovery phrase.
//
// Flow:
//   - First boot: Generate() → 16-byte random seed → 12-word BIP39 mnemonic (shown to user)
//   - Subsequent boots: load seed from keystore extras, derive same key deterministically
//   - Recovery: SeedFromMnemonic(words) → DerivePrivKey(seed) → identical peer ID
//
// Key derivation: HKDF-SHA256(seed16, info="cerclbackup-identity-seed-v1") → 32-byte Ed25519 seed
package identity

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"

	bip39 "github.com/tyler-smith/go-bip39"
	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"golang.org/x/crypto/hkdf"
)

const (
	// SeedSize is the BIP39 entropy size in bytes (128-bit → 12 words).
	SeedSize = 16

	// KeyName is the keystore extras key under which the identity seed is stored.
	KeyName = "identity_seed"

	domain = "cerclbackup-identity-seed-v1"
)

// Generate creates a fresh random identity seed and returns it along with its
// BIP39 mnemonic. The caller must store seed in the keystore and display
// mnemonic to the user exactly once.
func Generate() (seed []byte, mnemonic string, err error) {
	seed, err = bip39.NewEntropy(128)
	if err != nil {
		return nil, "", fmt.Errorf("identity: entropy: %w", err)
	}
	mnemonic, err = bip39.NewMnemonic(seed)
	if err != nil {
		return nil, "", fmt.Errorf("identity: mnemonic: %w", err)
	}
	return seed, mnemonic, nil
}

// SeedFromMnemonic decodes a 12-word BIP39 recovery phrase back to its 16-byte seed.
func SeedFromMnemonic(mnemonic string) ([]byte, error) {
	if !bip39.IsMnemonicValid(mnemonic) {
		return nil, fmt.Errorf("identity: invalid recovery phrase")
	}
	seed, err := bip39.EntropyFromMnemonic(mnemonic)
	if err != nil {
		return nil, fmt.Errorf("identity: decode phrase: %w", err)
	}
	if len(seed) != SeedSize {
		return nil, fmt.Errorf("identity: expected %d-byte seed, got %d (wrong word count?)", SeedSize, len(seed))
	}
	return seed, nil
}

// MnemonicFromSeed encodes a 16-byte seed as its 12-word BIP39 mnemonic.
func MnemonicFromSeed(seed []byte) (string, error) {
	if len(seed) != SeedSize {
		return "", fmt.Errorf("identity: seed must be %d bytes", SeedSize)
	}
	m, err := bip39.NewMnemonic(seed)
	if err != nil {
		return "", fmt.Errorf("identity: encode seed: %w", err)
	}
	return m, nil
}

// DerivePrivKey expands a 16-byte seed to a 32-byte Ed25519 seed via HKDF-SHA256,
// then returns the libp2p-compatible private key.  Same seed always produces the
// same peer ID.
func DerivePrivKey(seed []byte) (libp2pcrypto.PrivKey, error) {
	if len(seed) != SeedSize {
		return nil, fmt.Errorf("identity: seed must be %d bytes", SeedSize)
	}
	r := hkdf.New(sha256.New, seed, nil, []byte(domain))
	expanded := make([]byte, 32)
	if _, err := io.ReadFull(r, expanded); err != nil {
		return nil, fmt.Errorf("identity: hkdf: %w", err)
	}
	priv, _, err := libp2pcrypto.GenerateEd25519Key(bytes.NewReader(expanded))
	if err != nil {
		return nil, fmt.Errorf("identity: ed25519: %w", err)
	}
	return priv, nil
}
