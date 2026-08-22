package api

import (
	"context"
	"fmt"
	"net"
	"path/filepath"
	"strings"

	"github.com/cerclbackup/cerclbackup/internal/invite"
	p2pmod "github.com/cerclbackup/cerclbackup/internal/p2p"
	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
)

// preferredOutboundIP returns the local address the OS routing table would
// pick for outbound traffic (e.g. the real LAN IP on eth0), so Invite can
// avoid offering an unreachable address from a virtual interface such as
// Docker's docker0 bridge — those addresses sort earlier in the interface
// list on hosts that have Docker installed, silently breaking invites.
// The UDP "connection" below never sends a packet; it only performs a local
// routing lookup for the given destination.
func preferredOutboundIP() string {
	conn, err := net.Dial("udp4", "203.0.113.1:1")
	if err != nil {
		return ""
	}
	defer conn.Close()
	host, _, err := net.SplitHostPort(conn.LocalAddr().String())
	if err != nil {
		return ""
	}
	return host
}

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

// selfAddrs starts a throwaway host to enumerate this machine's own
// interface addresses, then rewrites each one's port to servePort (the port
// the caller's `serve` daemon actually listens on).
func selfAddrs(privKey libp2pcrypto.PrivKey, servePort int) (peerID string, addrs []string, _ error) {
	tmpHost, err := p2pmod.NewHost(privKey, 0)
	if err != nil {
		return "", nil, err
	}
	defer tmpHost.Close()

	peerID = tmpHost.ID().String()
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
	return peerID, addrs, nil
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

	peerID, addrs, err := selfAddrs(privKey, servePort)
	if err != nil {
		return nil, err
	}

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
	if preferred := preferredOutboundIP(); preferred != "" && preferred != "127.0.0.1" && preferred != "::1" && !strings.HasPrefix(preferred, "169.254.") {
		for _, a := range addrs {
			if strings.Contains(a, "/"+preferred+"/") {
				joinAddr = a
				break
			}
		}
	}
	if joinAddr == "" {
		for _, a := range addrs {
			if strings.Contains(a, "/169.254.") || strings.Contains(a, "/127.0.0.1/") || strings.Contains(a, "/::1/") {
				continue
			}
			joinAddr = a
			break
		}
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
// registering them as a buddy under the given friendly name. servePort is
// the port this machine's own `serve` daemon listens on, self-reported to
// the inviter so it can dial back immediately without waiting on mDNS/DHT.
func Join(password, addr, words, name string, servePort int) (peerID string, _ error) {
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

	_, myAddrs, err := selfAddrs(privKey, servePort)
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

	if err := p2pmod.SendInviteRequest(context.Background(), h, reg, addrInfo.ID, token, name, myAddrs); err != nil {
		return "", fmt.Errorf("invite: %w", err)
	}

	return addrInfo.ID.String(), nil
}
