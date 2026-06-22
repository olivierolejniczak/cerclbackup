// Package emailinvite implements the dual-channel MFA invite flow described
// in ARCHITECTURE.md Section 8.
//
// Alice generates a secret S (8 bytes) and a public commitment C = SHA-256(S).
// The payload (PeerID, pubkey, commitment, expiry, Ed25519 signature) is sent
// by email.  The 6 BIP39 words encoding S are shared out-of-band (SMS, voice).
// Bob verifies the signature and SHA-256(words→S) == C before connecting.
package emailinvite

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	bip39 "github.com/tyler-smith/go-bip39"
)

const (
	secretSize = 16 // bytes of OOB secret  =>  12 BIP39 words (128-bit entropy, BIP39 minimum)
	// BIP39: 128-bit entropy + 4-bit checksum = 132 bits = 12 × 11
	checksumBits = 4 // bits for 128-bit entropy
)

// Payload is the JSON structure sent by email.  It carries no secret.
type Payload struct {
	Version    int    `json:"version"`
	PeerID     string `json:"peer_id"`
	PubKey     string `json:"pubkey"`        // base64-encoded Ed25519 public key
	Commitment string `json:"commitment"`    // "sha256:<hex>" of the OOB secret
	Circle     string `json:"circle"`
	Expiry     string `json:"expiry"`        // RFC3339
	Nonce      string `json:"nonce"`         // hex, prevents replay
	Signature  string `json:"signature"`     // base64 Ed25519 sig over canonical fields
}

// Generate creates a new email invite payload and returns the 6-word OOB code.
// privKey is the inviter's raw Ed25519 private key (64 bytes).
// peerID is the inviter's libp2p peer ID string.
// circle is a human-readable label (e.g. "Famille").
// ttl is how long the invite stays valid.
func Generate(privKey ed25519.PrivateKey, peerID, circle string, ttl time.Duration) (Payload, string, error) {
	// 1. Random OOB secret S.
	secret := make([]byte, secretSize)
	if _, err := rand.Read(secret); err != nil {
		return Payload{}, "", fmt.Errorf("emailinvite: generate secret: %w", err)
	}

	// 2. Commitment C = SHA-256(S).
	commitment := sha256.Sum256(secret)

	// 3. Random nonce.
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return Payload{}, "", fmt.Errorf("emailinvite: generate nonce: %w", err)
	}

	expiry := time.Now().Add(ttl)
	pub := privKey.Public().(ed25519.PublicKey)

	p := Payload{
		Version:    1,
		PeerID:     peerID,
		PubKey:     base64.StdEncoding.EncodeToString(pub),
		Commitment: "sha256:" + hex.EncodeToString(commitment[:]),
		Circle:     circle,
		Expiry:     expiry.UTC().Format(time.RFC3339),
		Nonce:      hex.EncodeToString(nonce),
	}

	// 4. Sign the unsigned payload (Signature field empty).
	sig, err := signPayload(privKey, p)
	if err != nil {
		return Payload{}, "", fmt.Errorf("emailinvite: sign: %w", err)
	}
	p.Signature = base64.StdEncoding.EncodeToString(sig)

	// 5. Encode secret to 6 BIP39 words.
	words, err := secretToWords(secret)
	if err != nil {
		return Payload{}, "", fmt.Errorf("emailinvite: encode words: %w", err)
	}

	return p, words, nil
}

// Verify checks that:
//  1. The 6-word secret pre-images the commitment (SHA-256 match).
//  2. The payload signature is valid.
//  3. The payload has not expired.
//
// Returns nil on success.
func Verify(p Payload, sixWords string) error {
	// 1. Check expiry.
	expiry, err := time.Parse(time.RFC3339, p.Expiry)
	if err != nil {
		return fmt.Errorf("emailinvite: bad expiry: %w", err)
	}
	if time.Now().After(expiry) {
		return fmt.Errorf("emailinvite: invite expired at %s", p.Expiry)
	}

	// 2. Verify signature.
	pubBytes, err := base64.StdEncoding.DecodeString(p.PubKey)
	if err != nil {
		return fmt.Errorf("emailinvite: decode pubkey: %w", err)
	}
	sig, err := base64.StdEncoding.DecodeString(p.Signature)
	if err != nil {
		return fmt.Errorf("emailinvite: decode signature: %w", err)
	}
	pNoSig := p
	pNoSig.Signature = ""
	canonical, err := canonicalBytes(pNoSig)
	if err != nil {
		return fmt.Errorf("emailinvite: canonical: %w", err)
	}
	if !ed25519.Verify(pubBytes, canonical, sig) {
		return fmt.Errorf("emailinvite: invalid signature")
	}

	// 3. Decode 6 words → secret, verify commitment.
	secret, err := wordsToSecret(sixWords)
	if err != nil {
		return fmt.Errorf("emailinvite: decode words: %w", err)
	}
	sum := sha256.Sum256(secret)
	expected := "sha256:" + hex.EncodeToString(sum[:])
	if expected != p.Commitment {
		return fmt.Errorf("emailinvite: commitment mismatch (wrong OOB code?)")
	}

	return nil
}

// SecretFromWords decodes the 6-word OOB code to the raw 8-byte secret.
// Call this after Verify to obtain the pre-image to present to the inviter.
func SecretFromWords(sixWords string) ([]byte, error) {
	return wordsToSecret(sixWords)
}

// ToJSON serialises a Payload to indented JSON.
func ToJSON(p Payload) ([]byte, error) {
	return json.MarshalIndent(p, "", "  ")
}

// FromJSON parses a Payload from JSON.
func FromJSON(data []byte) (Payload, error) {
	var p Payload
	return p, json.Unmarshal(data, &p)
}

// canonicalBytes serialises the payload deterministically for signing.
// Signature field must be empty before calling.
func canonicalBytes(p Payload) ([]byte, error) {
	return json.Marshal(p)
}

func signPayload(privKey ed25519.PrivateKey, p Payload) ([]byte, error) {
	data, err := canonicalBytes(p)
	if err != nil {
		return nil, err
	}
	return ed25519.Sign(privKey, data), nil
}

// secretToWords encodes 8 bytes of entropy as 6 BIP39 words.
func secretToWords(secret []byte) (string, error) {
	mnemonic, err := bip39.NewMnemonic(secret)
	if err != nil {
		return "", err
	}
	return mnemonic, nil
}

// wordsToSecret decodes a 12-word BIP39 mnemonic back to 16 bytes.
//
// go-bip39 MnemonicToByteArray for 128-bit entropy returns 17 bytes where the
// high 4 bits of byte[0] are the BIP39 checksum nibble.  The actual entropy
// occupies bits 4..131 (shifted right by 4).  Same shift used in invite pkg.
func wordsToSecret(words string) ([]byte, error) {
	mnemonic := strings.TrimSpace(words)
	raw, err := bip39.MnemonicToByteArray(mnemonic)
	if err != nil {
		return nil, fmt.Errorf("decode mnemonic: %w", err)
	}
	// raw: 17 bytes = checksum nibble (4 bits) | entropy (128 bits)
	if len(raw) < secretSize+1 {
		if len(raw) < secretSize {
			return nil, fmt.Errorf("mnemonic too short (%d bytes)", len(raw))
		}
		return raw[:secretSize], nil
	}
	out := make([]byte, secretSize)
	for i := 0; i < secretSize; i++ {
		out[i] = (raw[i] << checksumBits) | (raw[i+1] >> (8 - checksumBits))
	}
	return out, nil
}
