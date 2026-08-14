package api

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/cerclbackup/cerclbackup/internal/invite"
	p2pmod "github.com/cerclbackup/cerclbackup/internal/p2p"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
)

// OpenInviteManager opens the on-disk invite-code manager used by both
// Invite and the `serve` daemon's incoming-invite handler.
func OpenInviteManager() *invite.Manager {
	cfgDir, _ := ConfigDir()
	return invite.NewManager(filepath.Join(cfgDir, "invites.json"))
}

// InviteResult is a freshly generated invite code plus this host's known
// reachable addresses, for display/QR/email to the invitee.
type InviteResult struct {
	Words       string   // 12-word mnemonic
	VerbalWords string   // last 3 words, for out-of-band voice verification
	Addrs       []string // every known multiaddr for this host
	JoinAddr    string   // best-guess address the buddy should actually use
	PeerID      string
}

// Invite generates a new invite code and collects this host's addresses on
// servePort (the port the buddy's `serve` will actually listen on).
func Invite(password string, servePort int) (*InviteResult, error) {
	if password == "" {
		return nil, fmt.Errorf("password is required")
	}
	ks, err := OpenKeystore(password)
	if err != nil {
		return nil, err
	}
	privKey, err := p2pmod.EnsurePeerIdentity(ks, password)
	if err != nil {
		return nil, err
	}

	tmpHost, err := p2pmod.NewHost(privKey, 0)
	if err != nil {
		return nil, err
	}
	peerID := tmpHost.ID().String()
	var addrs []string
	for _, ma := range tmpHost.Addrs() {
		s := ma.String()
		if strings.Contains(s, "/udp/") {
			continue
		}
		parts := strings.Split(s, "/tcp/")
		if len(parts) == 2 {
			s = parts[0] + fmt.Sprintf("/tcp/%d", servePort)
		}
		addrs = append(addrs, s+"/p2p/"+peerID)
	}
	tmpHost.Close()

	invMgr := OpenInviteManager()
	code, err := invMgr.Generate()
	if err != nil {
		return nil, err
	}

	wlist := strings.Fields(code.Words)
	verbally := ""
	if len(wlist) >= 3 {
		verbally = strings.Join(wlist[len(wlist)-3:], " ")
	}

	joinAddr := ""
	for _, a := range addrs {
		if strings.Contains(a, "/169.254.") || strings.Contains(a, "/127.0.0.1/") || strings.Contains(a, "/::1/") {
			continue
		}
		joinAddr = a
		break
	}
	if joinAddr == "" {
		for _, a := range addrs {
			if strings.Contains(a, "/127.0.0.1/tcp/") {
				joinAddr = a
				break
			}
		}
	}
	if joinAddr == "" && len(addrs) > 0 {
		joinAddr = addrs[0]
	}

	return &InviteResult{
		Words:       code.Words,
		VerbalWords: verbally,
		Addrs:       addrs,
		JoinAddr:    joinAddr,
		PeerID:      peerID,
	}, nil
}

// Join connects to an inviter at addr using the given invite mnemonic,
// registering them as a buddy under the given friendly name.
func Join(password, addr, words, name string) (peerID string, _ error) {
	if password == "" || addr == "" || words == "" {
		return "", fmt.Errorf("password, addr and words are required")
	}
	ks, err := OpenKeystore(password)
	if err != nil {
		return "", err
	}
	privKey, err := p2pmod.EnsurePeerIdentity(ks, password)
	if err != nil {
		return "", err
	}
	h, err := p2pmod.NewHost(privKey, 0)
	if err != nil {
		return "", err
	}
	defer h.Close()

	token, err := invite.TokenFromMnemonic(words)
	if err != nil {
		return "", err
	}

	maddr, err := multiaddr.NewMultiaddr(addr)
	if err != nil {
		return "", fmt.Errorf("invalid addr: %w", err)
	}
	addrInfo, err := peer.AddrInfoFromP2pAddr(maddr)
	if err != nil {
		return "", fmt.Errorf("addr parse: %w", err)
	}

	if err := h.Connect(context.Background(), *addrInfo); err != nil {
		return "", fmt.Errorf("connect: %w", err)
	}

	reg, err := OpenRegistry(ks)
	if err != nil {
		return "", err
	}

	if err := p2pmod.SendInviteRequest(context.Background(), h, reg, addrInfo.ID, token, name); err != nil {
		return "", fmt.Errorf("invite: %w", err)
	}

	return addrInfo.ID.String(), nil
}
