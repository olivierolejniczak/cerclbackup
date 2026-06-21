package crypto

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Keystore holds the master key and the Argon2 salt, persisted to disk
// in an encrypted JSON file.  The file is encrypted with AES-256-GCM
// using a key derived from the user's password via Argon2id.
//
// File format (after decryption):
//
//	{ "master_key": "<hex>", "salt": "<hex>" }
//
// The file on disk is: Encrypt(password-derived-key, JSON).
// The salt for the file-encryption key IS the same salt stored inside —
// this is safe because the salt is authenticated by GCM.
type Keystore struct {
	path      string
	masterKey []byte
	salt      []byte
	unlocked  bool
}

type keystoreJSON struct {
	MasterKey []byte `json:"master_key"`
	Salt      []byte `json:"salt"`
}

// DefaultKeystorePath returns the platform-appropriate default path.
// On Windows: %APPDATA%\CerclBackup\keystore.enc
// On Linux/WSL: ~/.config/cerclbackup/keystore.enc
func DefaultKeystorePath() string {
	if appdata := os.Getenv("APPDATA"); appdata != "" {
		return filepath.Join(appdata, "CerclBackup", "keystore.enc")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "cerclbackup", "keystore.enc")
}

// NewKeystore returns an empty Keystore bound to path.
// Call Create to initialise a new one, or Load+Unlock to open an existing one.
func NewKeystore(path string) *Keystore {
	return &Keystore{path: path}
}

// Create generates a new salt, derives a master key from password,
// persists the keystore to disk, and marks it as unlocked.
func (k *Keystore) Create(password string) error {
	salt, err := NewSalt()
	if err != nil {
		return err
	}
	masterKey := DeriveKey(password, salt)

	k.salt = salt
	k.masterKey = masterKey
	k.unlocked = true

	return k.save(password)
}

// Unlock loads and decrypts the keystore from disk.
func (k *Keystore) Unlock(password string) error {
	blob, err := os.ReadFile(k.path)
	if err != nil {
		return fmt.Errorf("keystore: read %q: %w", k.path, err)
	}

	// We need the salt to derive the decryption key, but the salt is inside
	// the encrypted blob.  To break this chicken-and-egg situation we prepend
	// the raw salt (SaltSize bytes) to the encrypted payload.
	if len(blob) < SaltSize {
		return fmt.Errorf("keystore: file too short")
	}
	salt := blob[:SaltSize]
	encrypted := blob[SaltSize:]

	fileKey := DeriveKey(password, salt)
	plaintext, err := Decrypt(fileKey, encrypted)
	if err != nil {
		return fmt.Errorf("keystore: wrong password or corrupted file")
	}

	var kj keystoreJSON
	if err := json.Unmarshal(plaintext, &kj); err != nil {
		return fmt.Errorf("keystore: parse: %w", err)
	}

	k.salt = kj.Salt
	k.masterKey = kj.MasterKey
	k.unlocked = true
	return nil
}

// MasterKey returns the 256-bit master key.  Panics if not unlocked.
func (k *Keystore) MasterKey() []byte {
	if !k.unlocked {
		panic("keystore: MasterKey called on locked keystore")
	}
	out := make([]byte, len(k.masterKey))
	copy(out, k.masterKey)
	return out
}

// Save re-encrypts and persists the keystore using the current password.
// Call after any mutation (e.g. password change).
func (k *Keystore) Save(password string) error {
	if !k.unlocked {
		return fmt.Errorf("keystore: cannot save locked keystore")
	}
	return k.save(password)
}

func (k *Keystore) save(password string) error {
	kj := keystoreJSON{
		MasterKey: k.masterKey,
		Salt:      k.salt,
	}
	plaintext, err := json.Marshal(kj)
	if err != nil {
		return fmt.Errorf("keystore: marshal: %w", err)
	}

	fileKey := DeriveKey(password, k.salt)
	encrypted, err := Encrypt(fileKey, plaintext)
	if err != nil {
		return err
	}

	// Prepend raw salt so Unlock can find it without a separate file.
	payload := append(k.salt, encrypted...)

	if err := os.MkdirAll(filepath.Dir(k.path), 0700); err != nil {
		return fmt.Errorf("keystore: mkdir: %w", err)
	}
	if err := os.WriteFile(k.path, payload, 0600); err != nil {
		return fmt.Errorf("keystore: write: %w", err)
	}
	return nil
}
