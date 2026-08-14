package api

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/cerclbackup/cerclbackup/internal/circle"
	cerclConfig "github.com/cerclbackup/cerclbackup/internal/config"
	bbcrypto "github.com/cerclbackup/cerclbackup/internal/crypto"
	"github.com/cerclbackup/cerclbackup/internal/identity"
	"github.com/cerclbackup/cerclbackup/internal/keyring"
	"github.com/cerclbackup/cerclbackup/internal/manifest"
	p2pmod "github.com/cerclbackup/cerclbackup/internal/p2p"
	"github.com/cerclbackup/cerclbackup/internal/storage"
	"github.com/libp2p/go-libp2p/core/peer"
)

// InitParams configures Init.
type InitParams struct {
	Password string // required
	StoreDir string
	Force    bool // overwrite an existing keystore/manifest/store
}

// InitResult is the outcome of a successful Init call.
type InitResult struct {
	PeerID         string
	RecoveryPhrase string // empty if the keystore has no identity seed
	KeystorePath   string
	StoreDir       string
}

// Init creates a fresh keystore, peer identity and default circle, and
// creates the shard store directory. If a keystore already exists at the
// default location, Force must be set to overwrite it (which deletes the
// existing keystore, manifest and shard store).
func Init(params InitParams) (*InitResult, error) {
	if params.Password == "" {
		return nil, fmt.Errorf("password is required")
	}
	storeDir := params.StoreDir
	if storeDir == "" {
		storeDir = storage.DefaultStorePath()
	}

	cfgDir, err := ConfigDir()
	if err != nil {
		return nil, fmt.Errorf("config dir: %w", err)
	}
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		return nil, fmt.Errorf("mkdir: %w", err)
	}
	ksPath := filepath.Join(cfgDir, "keystore.enc")

	if _, err := os.Stat(ksPath); err == nil {
		if !params.Force {
			return nil, fmt.Errorf("keystore already exists at %s (use Force to reinitialize, which deletes existing backup metadata)", ksPath)
		}
		os.Remove(ksPath)
		os.Remove(manifest.DefaultManifestPath())
		os.RemoveAll(storage.DefaultStorePath())
	}

	ks := bbcrypto.NewKeystore(ksPath)
	if err := ks.Create(params.Password); err != nil {
		return nil, fmt.Errorf("create keystore: %w", err)
	}

	privKey, err := p2pmod.EnsurePeerIdentity(ks, params.Password)
	if err != nil {
		return nil, fmt.Errorf("peer identity: %w", err)
	}
	peerID, err := peer.IDFromPrivateKey(privKey)
	if err != nil {
		return nil, fmt.Errorf("peer id: %w", err)
	}

	phrase := ""
	if seed := ks.LoadExtra(identity.KeyName); len(seed) > 0 {
		phrase, err = identity.MnemonicFromSeed(seed)
		if err != nil {
			return nil, fmt.Errorf("mnemonic: %w", err)
		}
	}

	mgr := circle.NewManager(ks, params.Password)
	if _, err := mgr.GetOrDefault("", params.Password); err != nil {
		return nil, fmt.Errorf("create default circle: %w", err)
	}

	if err := os.MkdirAll(storeDir, 0o700); err != nil {
		return nil, fmt.Errorf("store dir: %w", err)
	}

	return &InitResult{
		PeerID:         peerID.String(),
		RecoveryPhrase: phrase,
		KeystorePath:   ksPath,
		StoreDir:       storeDir,
	}, nil
}

// ShowPhrase returns the 12-word recovery phrase for the keystore's identity
// seed. Returns an error if the keystore has no seed (created before phrase
// recovery existed) — in that case the raw keystore file must be backed up.
func ShowPhrase(password string) (string, error) {
	if password == "" {
		return "", fmt.Errorf("password is required")
	}
	ks, err := OpenKeystore(password)
	if err != nil {
		return "", err
	}
	seed := ks.LoadExtra(identity.KeyName)
	if len(seed) == 0 {
		return "", fmt.Errorf("this keystore has no identity seed; back up the keystore file directly")
	}
	return identity.MnemonicFromSeed(seed)
}

// RecoverResult is the outcome of a successful Recover call.
type RecoverResult struct {
	PeerID string
}

// Recover creates a fresh keystore at the default location and re-derives
// the peer identity from a 12-word recovery phrase.
func Recover(phrase, password string) (*RecoverResult, error) {
	if phrase == "" || password == "" {
		return nil, fmt.Errorf("phrase and password are required")
	}
	seed, err := identity.SeedFromMnemonic(phrase)
	if err != nil {
		return nil, err
	}

	ksPath := bbcrypto.DefaultKeystorePath()
	ks := bbcrypto.NewKeystore(ksPath)
	if err := ks.Create(password); err != nil {
		return nil, fmt.Errorf("create keystore: %w", err)
	}

	priv, err := p2pmod.EnsurePeerIdentityFromSeed(ks, seed, password)
	if err != nil {
		return nil, fmt.Errorf("derive identity: %w", err)
	}
	peerID, err := peer.IDFromPrivateKey(priv)
	if err != nil {
		return nil, fmt.Errorf("peer ID: %w", err)
	}

	return &RecoverResult{PeerID: peerID.String()}, nil
}

// SetPassword stores password in the OS credential store (Windows Credential
// Manager, macOS Keychain, Linux Secret Service) so it doesn't need to be
// typed or stored in a plain-text file.
func SetPassword(password string) error {
	if password == "" {
		return fmt.Errorf("password cannot be empty")
	}
	return keyring.Set(password)
}

// DeletePassword removes the stored password from the OS credential store.
func DeletePassword() error {
	return keyring.Delete()
}

// Passwd changes the keystore password from oldPassword to newPassword.
func Passwd(oldPassword, newPassword string) error {
	if newPassword == "" {
		return fmt.Errorf("new password cannot be empty")
	}
	ks, err := OpenKeystore(oldPassword)
	if err != nil {
		return fmt.Errorf("wrong password or corrupted keystore: %w", err)
	}
	return ks.Save(newPassword)
}

// ConfigShow loads and returns the on-disk config.yaml (or defaults if it
// doesn't exist) plus the path it was loaded from.
func ConfigShow() (cerclConfig.Config, string) {
	path := cerclConfig.DefaultPath()
	return cerclConfig.LoadFrom(path), path
}

// ConfigInit writes a commented sample config.yaml to the default path.
// Fails if a config file already exists there.
func ConfigInit() (string, error) {
	path := cerclConfig.DefaultPath()
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("%s already exists (delete it first to regenerate)", path)
	}
	if err := cerclConfig.WriteTemplate(path); err != nil {
		return "", err
	}
	return path, nil
}
