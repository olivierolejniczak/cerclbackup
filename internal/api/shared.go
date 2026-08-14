// Package api exposes CerclBackup's operations (backup, restore, circle
// management, buddy status, maintenance, ...) as plain Go functions that
// take structs and return structs/errors — no flag parsing, no os.Exit, no
// log.Fatal, no printing. Both the CLI (cmd/cerclbackup) and the Wails GUI
// (cmd/cerclbackup-gui) call into this package so the two front ends never
// diverge in behavior.
package api

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/cerclbackup/cerclbackup/internal/buddy"
	bbcrypto "github.com/cerclbackup/cerclbackup/internal/crypto"
	"github.com/cerclbackup/cerclbackup/internal/manifest"
	"github.com/cerclbackup/cerclbackup/internal/storage"
)

// ConfigDir returns the root directory for all CerclBackup data files.
// Override with CERCLBACKUP_CONFIG_DIR for testing or multi-instance setups.
func ConfigDir() (string, error) {
	if d := os.Getenv("CERCLBACKUP_CONFIG_DIR"); d != "" {
		return d, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "cerclbackup"), nil
}

// OpenStore opens (creating if necessary) the shard store at dir.
func OpenStore(dir string) (*storage.Store, error) {
	return storage.New(dir)
}

// OpenOrCreateKeystore unlocks the keystore with password, creating it on
// first use.
func OpenOrCreateKeystore(password string) (*bbcrypto.Keystore, error) {
	cfgDir, err := ConfigDir()
	if err != nil {
		return nil, err
	}
	ksPath := filepath.Join(cfgDir, "keystore.enc")
	ks := bbcrypto.NewKeystore(ksPath)
	if _, err := os.Stat(ksPath); os.IsNotExist(err) {
		if err := ks.Create(password); err != nil {
			return nil, fmt.Errorf("keystore create: %w", err)
		}
	} else if err := ks.Unlock(password); err != nil {
		return nil, fmt.Errorf("keystore unlock: %w", err)
	}
	return ks, nil
}

// OpenKeystore unlocks an existing keystore. It fails if none exists yet.
func OpenKeystore(password string) (*bbcrypto.Keystore, error) {
	cfgDir, err := ConfigDir()
	if err != nil {
		return nil, err
	}
	ksPath := filepath.Join(cfgDir, "keystore.enc")
	ks := bbcrypto.NewKeystore(ksPath)
	if err := ks.Unlock(password); err != nil {
		return nil, fmt.Errorf("keystore unlock: %w", err)
	}
	return ks, nil
}

// OpenManifest loads the encrypted file manifest using masterKey.
func OpenManifest(masterKey []byte) (*manifest.Manifest, error) {
	mf := manifest.New(manifest.DefaultManifestPath(), masterKey)
	if err := mf.Load(); err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	return mf, nil
}

// OpenRegistry loads the encrypted buddy registry for ks.
func OpenRegistry(ks *bbcrypto.Keystore) (*buddy.Registry, error) {
	cfgDir, err := ConfigDir()
	if err != nil {
		return nil, err
	}
	regPath := filepath.Join(cfgDir, "buddies.enc")
	return buddy.NewRegistry(regPath, ks.MasterKey())
}
