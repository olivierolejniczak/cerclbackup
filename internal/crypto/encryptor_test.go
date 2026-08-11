package crypto_test

import (
	"bytes"
	"testing"

	"github.com/cerclbackup/cerclbackup/internal/crypto"
)

func TestDeriveKey_Deterministic(t *testing.T) {
	salt, err := crypto.NewSalt()
	if err != nil {
		t.Fatal(err)
	}
	k1 := crypto.DeriveKey("hunter2", salt)
	k2 := crypto.DeriveKey("hunter2", salt)
	if !bytes.Equal(k1, k2) {
		t.Fatal("DeriveKey should be deterministic for the same password+salt")
	}
	if len(k1) != 32 {
		t.Fatalf("expected 32-byte key, got %d", len(k1))
	}
}

func TestDeriveKey_DifferentSaltsDifferentKeys(t *testing.T) {
	salt1, _ := crypto.NewSalt()
	salt2, _ := crypto.NewSalt()
	k1 := crypto.DeriveKey("hunter2", salt1)
	k2 := crypto.DeriveKey("hunter2", salt2)
	if bytes.Equal(k1, k2) {
		t.Fatal("different salts should produce different keys")
	}
}

func TestDeriveCircleKey_IsolatesCircles(t *testing.T) {
	salt, _ := crypto.NewSalt()
	kFamily := crypto.DeriveCircleKey("password", "circle-family", salt)
	kWork := crypto.DeriveCircleKey("password", "circle-work", salt)
	if bytes.Equal(kFamily, kWork) {
		t.Fatal("distinct circles sharing a salt must derive distinct keys")
	}
}

func TestEncryptDecrypt_Roundtrip(t *testing.T) {
	key := crypto.DeriveKey("pw", []byte("0123456789abcdef"))
	plaintext := []byte("the quick brown fox jumps over the lazy dog")

	ct, err := crypto.Encrypt(key, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(ct, plaintext) {
		t.Fatal("ciphertext must not equal plaintext")
	}

	pt, err := crypto.Decrypt(key, ct)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(pt, plaintext) {
		t.Fatalf("roundtrip mismatch: got %q want %q", pt, plaintext)
	}
}

func TestEncrypt_NonceIsRandomPerCall(t *testing.T) {
	key := crypto.DeriveKey("pw", []byte("0123456789abcdef"))
	plaintext := []byte("same plaintext both times")

	ct1, _ := crypto.Encrypt(key, plaintext)
	ct2, _ := crypto.Encrypt(key, plaintext)
	if bytes.Equal(ct1, ct2) {
		t.Fatal("two encryptions of identical plaintext must not produce identical ciphertext (nonce reuse)")
	}
}

func TestDecrypt_WrongKeyFails(t *testing.T) {
	key1 := crypto.DeriveKey("pw1", []byte("0123456789abcdef"))
	key2 := crypto.DeriveKey("pw2", []byte("0123456789abcdef"))

	ct, err := crypto.Encrypt(key1, []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := crypto.Decrypt(key2, ct); err == nil {
		t.Fatal("decrypting with the wrong key must fail")
	}
}

func TestDecrypt_TamperedCiphertextFails(t *testing.T) {
	key := crypto.DeriveKey("pw", []byte("0123456789abcdef"))
	ct, err := crypto.Encrypt(key, []byte("secret payload"))
	if err != nil {
		t.Fatal(err)
	}
	tampered := append([]byte(nil), ct...)
	tampered[len(tampered)-1] ^= 0xFF // flip a bit in the GCM tag
	if _, err := crypto.Decrypt(key, tampered); err == nil {
		t.Fatal("decrypting tampered ciphertext must fail (GCM authentication)")
	}
}

func TestDecrypt_TooShortFails(t *testing.T) {
	key := crypto.DeriveKey("pw", []byte("0123456789abcdef"))
	if _, err := crypto.Decrypt(key, []byte("short")); err == nil {
		t.Fatal("decrypting a too-short blob must fail")
	}
}

func TestShardKeys_DifferByIndex(t *testing.T) {
	fileKey := crypto.DeriveKey("pw", []byte("0123456789abcdef"))
	k0, err := crypto.DeriveShardKey(fileKey, 0)
	if err != nil {
		t.Fatal(err)
	}
	k1, err := crypto.DeriveShardKey(fileKey, 1)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(k0, k1) {
		t.Fatal("shard keys for different indices must differ")
	}
}

func TestEncryptDecryptShard_Roundtrip(t *testing.T) {
	fileKey := crypto.DeriveKey("pw", []byte("0123456789abcdef"))
	plaintext := []byte("shard payload bytes")

	blob, err := crypto.EncryptShard(fileKey, 2, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	got, err := crypto.DecryptShard(fileKey, 2, blob)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("shard roundtrip mismatch: got %q want %q", got, plaintext)
	}

	// Decrypting with the wrong shard index must fail (different derived key).
	if _, err := crypto.DecryptShard(fileKey, 3, blob); err == nil {
		t.Fatal("decrypting shard with wrong index should fail")
	}
}

func TestDeriveFileKey_DiffersByHash(t *testing.T) {
	masterKey := crypto.DeriveKey("pw", []byte("0123456789abcdef"))
	var h1, h2 [32]byte
	h1[0] = 0x01
	h2[0] = 0x02

	k1, err := crypto.DeriveFileKey(masterKey, h1)
	if err != nil {
		t.Fatal(err)
	}
	k2, err := crypto.DeriveFileKey(masterKey, h2)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(k1, k2) {
		t.Fatal("file keys for different file hashes must differ")
	}
}
