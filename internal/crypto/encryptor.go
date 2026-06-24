// Package crypto handles all cryptographic operations:
//   - Master key derivation from password (Argon2id)
//   - Per-file and per-shard key derivation (HKDF)
//   - Shard encryption / decryption (AES-256-GCM)
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/hkdf"
)

// Argon2 parameters — conservative defaults suitable for a desktop app.
// Time=3, Memory=64MB, Threads=4 → ~300 ms on a modern CPU.
const (
	argon2Time    = 3
	argon2Memory  = 64 * 1024 // 64 MB
	argon2Threads = 4
	argon2KeyLen  = 32 // 256-bit master key
)

// SaltSize is the length of the Argon2 salt in bytes.
const SaltSize = 16

// DeriveKey derives a 256-bit master key from a user password and a random salt.
// The salt must be stored in the keystore (it is NOT secret).
func DeriveKey(password string, salt []byte) []byte {
	return argon2.IDKey(
		[]byte(password),
		salt,
		argon2Time,
		argon2Memory,
		argon2Threads,
		argon2KeyLen,
	)
}

// DeriveCircleKey derives a circle-specific master key.  The circleID is mixed
// into the password with a null-byte separator so each circle has a unique key
// even when two circles share the same Argon2 salt.
func DeriveCircleKey(password, circleID string, salt []byte) []byte {
	combined := password + "\x00" + circleID
	return DeriveKey(combined, salt)
}

// NewSalt generates a cryptographically random salt for Argon2.
func NewSalt() ([]byte, error) {
	salt := make([]byte, SaltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("crypto: generate salt: %w", err)
	}
	return salt, nil
}

// DeriveFileKey derives a per-file sub-key from the master key and the
// SHA-256 hash of the file content.  Using the file hash as info means
// the key is deterministic for a given file version and master key.
func DeriveFileKey(masterKey []byte, fileHash [32]byte) ([]byte, error) {
	return deriveSubKey(masterKey, fileHash[:], "cerclbackup-file-key-v1")
}

// DeriveShardKey derives a per-shard sub-key from a file key and the shard index.
func DeriveShardKey(fileKey []byte, shardIndex int) ([]byte, error) {
	info := fmt.Sprintf("cerclbackup-shard-key-v1-%d", shardIndex)
	return deriveSubKey(fileKey, nil, info)
}

// deriveSubKey runs HKDF-SHA256 to produce a 32-byte sub-key.
func deriveSubKey(secret, salt []byte, info string) ([]byte, error) {
	r := hkdf.New(sha256.New, secret, salt, []byte(info))
	key := make([]byte, 32)
	if _, err := io.ReadFull(r, key); err != nil {
		return nil, fmt.Errorf("crypto: HKDF derive (%q): %w", info, err)
	}
	return key, nil
}

// Encrypt encrypts plaintext with AES-256-GCM using key.
// Returns (nonce || ciphertext) where nonce is 12 bytes.
// The nonce is randomly generated for each call.
func Encrypt(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: new AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: new GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("crypto: generate nonce: %w", err)
	}

	// Seal appends ciphertext+tag after nonce.
	out := gcm.Seal(nonce, nonce, plaintext, nil)
	return out, nil
}

// Decrypt decrypts a blob produced by Encrypt.
// The input must start with the 12-byte nonce followed by ciphertext+tag.
func Decrypt(key, blob []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: new AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: new GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(blob) < nonceSize {
		return nil, fmt.Errorf("crypto: blob too short (%d bytes)", len(blob))
	}

	nonce, ciphertext := blob[:nonceSize], blob[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("crypto: AES-GCM decrypt: %w", err)
	}
	return plaintext, nil
}

// EncryptShard encrypts a single RS shard using a key derived from the file key
// and the shard index.  This is the canonical function called by the pipeline.
func EncryptShard(fileKey []byte, shardIndex int, plaintext []byte) ([]byte, error) {
	shardKey, err := DeriveShardKey(fileKey, shardIndex)
	if err != nil {
		return nil, err
	}
	return Encrypt(shardKey, plaintext)
}

// DecryptShard is the inverse of EncryptShard.
func DecryptShard(fileKey []byte, shardIndex int, blob []byte) ([]byte, error) {
	shardKey, err := DeriveShardKey(fileKey, shardIndex)
	if err != nil {
		return nil, err
	}
	return Decrypt(shardKey, blob)
}
