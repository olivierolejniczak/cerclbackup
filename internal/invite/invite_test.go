package invite_test

import (
	"path/filepath"
	"testing"

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
