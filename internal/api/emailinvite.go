package api

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/cerclbackup/cerclbackup/internal/emailinvite"
	p2pmod "github.com/cerclbackup/cerclbackup/internal/p2p"
	"github.com/libp2p/go-libp2p/core/peer"
)

// InviteEmailParams configures InviteEmail.
type InviteEmailParams struct {
	Password string
	Circle   string // circle name shown in the email, default "CerclBackup"
}

// InviteEmailResult is the generated out-of-band email invite. CerclBackup
// never sends this itself — the caller pastes Subject/Body into their own
// mail client and shares Words with the recipient through a different
// channel, so the recipient can confirm the invite's sender identity before
// trusting it.
type InviteEmailResult struct {
	PeerID      string
	Words       string // 12-word OOB code — must be shared via a different channel than the email
	PayloadJSON []byte // raw JSON payload, also embedded in Body
	Subject     string // ready-to-send email subject
	Body        string // ready-to-send email body
}

// InviteEmail generates a dual-channel (signed payload + OOB word) email
// invite and returns ready-to-send email content; it never sends mail
// itself.
func InviteEmail(params InviteEmailParams, to string) (*InviteEmailResult, error) {
	if to == "" || params.Password == "" {
		return nil, fmt.Errorf("to and password are required")
	}
	circle := params.Circle
	if circle == "" {
		circle = "CerclBackup"
	}

	ks, err := OpenKeystore(params.Password)
	if err != nil {
		return nil, err
	}
	privKey, err := p2pmod.EnsurePeerIdentity(ks, params.Password)
	if err != nil {
		return nil, err
	}
	h, err := p2pmod.NewHost(privKey, 0)
	if err != nil {
		return nil, err
	}
	defer h.Close()

	rawPriv, err := privKey.Raw()
	if err != nil {
		return nil, fmt.Errorf("raw private key: %w", err)
	}

	payload, words, err := emailinvite.Generate(rawPriv, h.ID().String(), circle, 48*time.Hour)
	if err != nil {
		return nil, fmt.Errorf("generate: %w", err)
	}

	invMgr := OpenInviteManager()
	secret, _ := emailinvite.SecretFromWords(words)
	expiry, _ := time.Parse(time.RFC3339, payload.Expiry)
	sum := sha256.Sum256(secret)
	if err := invMgr.AddCommitment(sum[:], expiry); err != nil {
		return nil, fmt.Errorf("register commitment: %w", err)
	}

	data, err := emailinvite.ToJSON(payload)
	if err != nil {
		return nil, err
	}
	subject, body := emailinvite.ComposeEmail(payload)

	result := &InviteEmailResult{
		PeerID:      h.ID().String(),
		Words:       words,
		PayloadJSON: data,
		Subject:     subject,
		Body:        body,
	}

	return result, nil
}

// JoinEmail verifies an email invite payload against the OOB words and, on
// success, performs the P2P handshake to register the inviter as a buddy.
// Returns the circle name and inviter peer ID from the verified payload.
func JoinEmail(password string, payloadJSON []byte, words string) (circleName, peerIDStr string, _ error) {
	if password == "" || len(payloadJSON) == 0 || words == "" {
		return "", "", fmt.Errorf("password, payload and words are required")
	}

	payload, err := emailinvite.FromJSON(payloadJSON)
	if err != nil {
		return "", "", fmt.Errorf("parse payload: %w", err)
	}
	if err := emailinvite.Verify(payload, words); err != nil {
		return "", "", fmt.Errorf("verification failed: %w", err)
	}

	ks, err := OpenKeystore(password)
	if err != nil {
		return "", "", err
	}
	privKey, err := p2pmod.EnsurePeerIdentity(ks, password)
	if err != nil {
		return "", "", err
	}
	h, err := p2pmod.NewHost(privKey, 0)
	if err != nil {
		return "", "", err
	}
	defer h.Close()

	reg, err := OpenRegistry(ks)
	if err != nil {
		return "", "", err
	}

	secret, err := emailinvite.SecretFromWords(words)
	if err != nil {
		return "", "", fmt.Errorf("decode words: %w", err)
	}

	inviterPeerID, err := peer.Decode(payload.PeerID)
	if err != nil {
		return "", "", fmt.Errorf("decode peer ID: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := p2pmod.SendInviteRequest(ctx, h, reg, inviterPeerID, secret, h.ID().String(), nil); err != nil {
		return "", "", fmt.Errorf("P2P handshake: %w", err)
	}

	return payload.Circle, payload.PeerID, nil
}
