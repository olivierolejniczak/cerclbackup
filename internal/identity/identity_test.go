package identity_test

import (
	"strings"
	"testing"

	"github.com/cerclbackup/cerclbackup/internal/identity"
)

func TestGenerateRoundtrip(t *testing.T) {
	seed, mnemonic, err := identity.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if len(seed) != identity.SeedSize {
		t.Fatalf("seed length: got %d want %d", len(seed), identity.SeedSize)
	}
	words := strings.Fields(mnemonic)
	if len(words) != 12 {
		t.Fatalf("mnemonic word count: got %d want 12", len(words))
	}

	// Decode must recover the original seed exactly.
	decoded, err := identity.SeedFromMnemonic(mnemonic)
	if err != nil {
		t.Fatal(err)
	}
	for i, b := range seed {
		if decoded[i] != b {
			t.Fatalf("seed mismatch at byte %d: got %02x want %02x", i, decoded[i], b)
		}
	}
}

func TestMnemonicFromSeedRoundtrip(t *testing.T) {
	seed, original, err := identity.Generate()
	if err != nil {
		t.Fatal(err)
	}
	mnemonic, err := identity.MnemonicFromSeed(seed)
	if err != nil {
		t.Fatal(err)
	}
	if mnemonic != original {
		t.Fatalf("mnemonic mismatch: got %q want %q", mnemonic, original)
	}
}

func TestDeriveConsistency(t *testing.T) {
	seed, _, err := identity.Generate()
	if err != nil {
		t.Fatal(err)
	}
	priv1, err := identity.DerivePrivKey(seed)
	if err != nil {
		t.Fatal(err)
	}
	priv2, err := identity.DerivePrivKey(seed)
	if err != nil {
		t.Fatal(err)
	}
	if !priv1.Equals(priv2) {
		t.Fatal("same seed produced different private keys")
	}
	if priv1.GetPublic().Equals(priv2.GetPublic()) {
		// sanity: if public keys are equal the private ones should be too
	}
}

func TestDifferentSeedsDifferentKeys(t *testing.T) {
	seed1, _, err := identity.Generate()
	if err != nil {
		t.Fatal(err)
	}
	seed2, _, err := identity.Generate()
	if err != nil {
		t.Fatal(err)
	}
	priv1, _ := identity.DerivePrivKey(seed1)
	priv2, _ := identity.DerivePrivKey(seed2)
	if priv1.Equals(priv2) {
		t.Fatal("different seeds produced the same private key")
	}
}

func TestPhraseRecovery(t *testing.T) {
	seed, mnemonic, err := identity.Generate()
	if err != nil {
		t.Fatal(err)
	}
	// Simulate recovery on a new machine from the phrase.
	recoveredSeed, err := identity.SeedFromMnemonic(mnemonic)
	if err != nil {
		t.Fatal(err)
	}
	origKey, err := identity.DerivePrivKey(seed)
	if err != nil {
		t.Fatal(err)
	}
	recoveredKey, err := identity.DerivePrivKey(recoveredSeed)
	if err != nil {
		t.Fatal(err)
	}
	if !origKey.Equals(recoveredKey) {
		t.Fatal("recovered key differs from original")
	}
}

func TestInvalidMnemonic(t *testing.T) {
	cases := []string{
		"",
		"abandon",
		"abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon",
		"zzzzz zzzzz zzzzz zzzzz zzzzz zzzzz zzzzz zzzzz zzzzz zzzzz zzzzz zzzzz",
		"not a valid bip39 phrase at all buddy",
	}
	for _, c := range cases {
		if _, err := identity.SeedFromMnemonic(c); err == nil {
			t.Errorf("expected error for phrase %q, got nil", c)
		}
	}
}

func TestInvalidSeedLength(t *testing.T) {
	if _, err := identity.DerivePrivKey(nil); err == nil {
		t.Error("nil seed: expected error")
	}
	if _, err := identity.DerivePrivKey(make([]byte, 10)); err == nil {
		t.Error("short seed: expected error")
	}
	if _, err := identity.MnemonicFromSeed(make([]byte, 8)); err == nil {
		t.Error("wrong length: expected error")
	}
}
