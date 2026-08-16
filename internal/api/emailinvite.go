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

// SMTPConfig mirrors emailinvite.SMTPConfig for callers that don't want to
// import that package directly.
type SMTPConfig = emailinvite.SMTPConfig

// InviteEmailParams configures InviteEmail.
type InviteEmailParams struct {
	Password string
	Circle   string      // circle name shown in the email, default "CerclBackup"
	SMTP     *SMTPConfig // nil = don't send, just return the payload for display
}

// InviteEmailResult is the generated out-of-band email invite.
type InviteEmailResult struct {
	PeerID      string
	Words       string // 12-word OOB code — must be shared via a different channel than the email
	PayloadJSON []byte // JSON to paste into an email body, when SMTP is nil
	Sent        bool
}

// InviteEmail generates a dual-channel (signed payload + OOB word) email
// invite and, if params.SMTP is set, sends it to `to`.
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

	result := &InviteEmailResult{PeerID: h.ID().String(), Words: words}

	if params.SMTP != nil {
		if err := emailinvite.Send(*params.SMTP, to, payload); err != nil {
			return nil, fmt.Errorf("send: %w", err)
		}
		result.Sent = true
	} else {
		data, err := emailinvite.ToJSON(payload)
		if err != nil {
			return nil, err
		}
		result.PayloadJSON = data
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
