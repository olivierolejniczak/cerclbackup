package invite_test

import (
	"crypto/sha256"
	"path/filepath"
	"testing"
	"time"

	"github.com/cerclbackup/cerclbackup/internal/invite"
)

func TestInviteGenerateConsume(t *testing.T) {
	dir := t.TempDir()
	mgr := invite.NewManager(filepath.Join(dir, "pending.json"))

	code, err := mgr.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if code.Words == "" {
		t.Fatal("empty mnemonic")
	}

	// Decode the mnemonic back to the token
	token, err := invite.TokenFromMnemonic(code.Words)
	if err != nil {
		t.Fatal(err)
	}

	// Consume the token
	if err := mgr.Consume(token); err != nil {
		t.Fatal(err)
	}
}

func TestInviteConsumeReplay(t *testing.T) {
	dir := t.TempDir()
	mgr := invite.NewManager(filepath.Join(dir, "pending.json"))

	code, _ := mgr.Generate()
	token, _ := invite.TokenFromMnemonic(code.Words)

	_ = mgr.Consume(token)

	// Second consume must fail
	if err := mgr.Consume(token); err == nil {
		t.Fatal("expected error on replay, got nil")
	}
}

func TestInviteUnknownToken(t *testing.T) {
	dir := t.TempDir()
	mgr := invite.NewManager(filepath.Join(dir, "pending.json"))

	fakeToken := make([]byte, 16)
	if err := mgr.Consume(fakeToken); err == nil {
		t.Fatal("expected error for unknown token")
	}
}

func TestTokenFromMnemonicRoundtrip(t *testing.T) {
	dir := t.TempDir()
	mgr := invite.NewManager(filepath.Join(dir, "pending.json"))

	code, err := mgr.Generate()
	if err != nil {
		t.Fatal(err)
	}
	token, err := invite.TokenFromMnemonic(code.Words)
	if err != nil {
		t.Fatal(err)
	}
	if string(token) != string(code.Token) {
		t.Fatalf("token mismatch: got %x, want %x", token, code.Token)
	}
}

func TestAddAndConsumeCommitment(t *testing.T) {
	dir := t.TempDir()
	m := invite.NewManager(filepath.Join(dir, "invites.json"))

	preimage := []byte("test-secret-preimage-16b")
	h := sha256.Sum256(preimage)
	expiry := time.Now().Add(time.Hour)

	if err := m.AddCommitment(h[:], expiry); err != nil {
		t.Fatalf("AddCommitment: %v", err)
	}

	// Wrong pre-image must fail.
	if err := m.ConsumeCommitment([]byte("wrong")); err == nil {
		t.Error("expected error for wrong pre-image")
	}

	// Correct pre-image must succeed.
	if err := m.ConsumeCommitment(preimage); err != nil {
		t.Errorf("ConsumeCommitment: %v", err)
	}

	// Replay must fail (one-time use).
	if err := m.ConsumeCommitment(preimage); err == nil {
		t.Error("expected error on commitment replay")
	}
}

func TestExpiredCommitmentRejected(t *testing.T) {
	dir := t.TempDir()
	m := invite.NewManager(filepath.Join(dir, "invites.json"))

	preimage := []byte("another-secret-value-ok")
	h := sha256.Sum256(preimage)

	if err := m.AddCommitment(h[:], time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := m.ConsumeCommitment(preimage); err == nil {
		t.Error("expected error for expired commitment")
	}
}
