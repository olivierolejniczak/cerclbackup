package emailinvite_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"
	"time"

	"github.com/cerclbackup/cerclbackup/internal/emailinvite"
)

func newTestKey(t *testing.T) (ed25519.PrivateKey, ed25519.PublicKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return priv, pub
}

// TestGenerateAndVerify: full happy path -- generate payload + 6 words,
// verify commitment and signature succeed.
func TestGenerateAndVerify(t *testing.T) {
	priv, _ := newTestKey(t)
	p, words, err := emailinvite.Generate(priv, "12D3KooWTest", "Famille", time.Hour)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if words == "" {
		t.Fatal("expected non-empty 6-word OOB code")
	}
	wordCount := len(strings.Fields(words))
	if wordCount != 12 {
		t.Errorf("expected 12 words, got %d: %q", wordCount, words)
	}
	if err := emailinvite.Verify(p, words); err != nil {
		t.Errorf("Verify: %v", err)
	}
}

// TestVerifyWrongWords: a syntactically valid 12-word mnemonic whose SHA-256
// does not match the payload commitment must be rejected.
func TestVerifyWrongWords(t *testing.T) {
	priv, _ := newTestKey(t)
	// Generate two independent payloads; each has a distinct secret.
	p1, _, err := emailinvite.Generate(priv, "12D3KooWTest", "Famille", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	_, words2, err := emailinvite.Generate(priv, "12D3KooWTest", "Famille", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	// words2 decodes fine but its secret doesn't match p1's commitment.
	if err := emailinvite.Verify(p1, words2); err == nil {
		t.Error("expected commitment mismatch error, got nil")
	}
}

// TestVerifyExpiredPayload: expired invites are rejected.
func TestVerifyExpiredPayload(t *testing.T) {
	priv, _ := newTestKey(t)
	p, words, err := emailinvite.Generate(priv, "12D3KooWTest", "Famille", -time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := emailinvite.Verify(p, words); err == nil {
		t.Error("expected error for expired payload, got nil")
	}
}

// TestVerifyTamperedPayload: modifying any payload field invalidates signature.
func TestVerifyTamperedPayload(t *testing.T) {
	priv, _ := newTestKey(t)
	p, words, err := emailinvite.Generate(priv, "12D3KooWTest", "Famille", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	p.Circle = "EvilCircle" // tamper
	if err := emailinvite.Verify(p, words); err == nil {
		t.Error("expected error for tampered payload, got nil")
	}
}

// TestRoundTripJSON: payload survives JSON serialisation.
func TestRoundTripJSON(t *testing.T) {
	priv, _ := newTestKey(t)
	p, words, err := emailinvite.Generate(priv, "12D3KooWTest", "Amis", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	data, err := emailinvite.ToJSON(p)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := emailinvite.FromJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := emailinvite.Verify(p2, words); err != nil {
		t.Errorf("Verify after JSON round-trip: %v", err)
	}
}

// TestSecretFromWords: decoded pre-image is consistent across calls.
func TestSecretFromWords(t *testing.T) {
	priv, _ := newTestKey(t)
	_, words, err := emailinvite.Generate(priv, "12D3KooWTest", "Test", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	s1, err := emailinvite.SecretFromWords(words)
	if err != nil {
		t.Fatalf("SecretFromWords: %v", err)
	}
	s2, err := emailinvite.SecretFromWords(words)
	if err != nil {
		t.Fatal(err)
	}
	if string(s1) != string(s2) {
		t.Error("SecretFromWords not deterministic")
	}
	if len(s1) != 16 {
		t.Errorf("expected 16-byte secret, got %d", len(s1))
	}
}
