package crypto_test

import (
	"path/filepath"
	"testing"

	"github.com/cerclbackup/cerclbackup/internal/crypto"
)

func TestKeystore_CreateUnlockRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keystore.enc")

	ks := crypto.NewKeystore(path)
	if err := ks.Create("correct horse"); err != nil {
		t.Fatal(err)
	}
	masterKey := ks.MasterKey()

	loaded := crypto.NewKeystore(path)
	if err := loaded.Unlock("correct horse"); err != nil {
		t.Fatal(err)
	}
	if string(loaded.MasterKey()) != string(masterKey) {
		t.Fatal("master key mismatch after unlock")
	}
}

func TestKeystore_WrongPasswordFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keystore.enc")

	ks := crypto.NewKeystore(path)
	if err := ks.Create("correct horse"); err != nil {
		t.Fatal(err)
	}

	loaded := crypto.NewKeystore(path)
	if err := loaded.Unlock("wrong password"); err == nil {
		t.Fatal("unlocking with the wrong password should fail")
	}
}

func TestKeystore_MasterKeyPanicsWhenLocked(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("MasterKey on a locked keystore should panic")
		}
	}()
	ks := crypto.NewKeystore(filepath.Join(t.TempDir(), "keystore.enc"))
	_ = ks.MasterKey()
}

func TestKeystore_StoreAndLoadExtra(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keystore.enc")

	ks := crypto.NewKeystore(path)
	if err := ks.Create("pw"); err != nil {
		t.Fatal(err)
	}
	if err := ks.StoreExtra("recovery-phrase", []byte("twelve words here"), "pw"); err != nil {
		t.Fatal(err)
	}

	loaded := crypto.NewKeystore(path)
	if err := loaded.Unlock("pw"); err != nil {
		t.Fatal(err)
	}
	got := loaded.LoadExtra("recovery-phrase")
	if string(got) != "twelve words here" {
		t.Fatalf("extra mismatch: got %q", got)
	}
	if loaded.LoadExtra("missing-key") != nil {
		t.Fatal("LoadExtra for a missing key should return nil")
	}
}

func TestKeystore_UnlockMissingFile(t *testing.T) {
	ks := crypto.NewKeystore(filepath.Join(t.TempDir(), "does-not-exist.enc"))
	if err := ks.Unlock("pw"); err == nil {
		t.Fatal("unlocking a nonexistent keystore file should fail")
	}
}
