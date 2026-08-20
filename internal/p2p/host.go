package p2p

import (
	"fmt"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"

	"github.com/cerclbackup/cerclbackup/internal/buddy"
)

const DefaultPort = 7742

// baseOpts returns the transport, security, and NAT-traversal options shared
// by every host this package creates.
//
// Most home routers don't support UPnP/NAT-PMP (NATPortMap does nothing) and
// have no port forwarded, so two buddies behind separate NATs can't reach
// each other directly. AutoNATv2 lets this host learn it's unreachable;
// AutoRelay then tries to reserve a relay slot and advertise a relayed
// address so a buddy can dial in and attempt a direct hole-punch (DCUtR)
// afterwards. EnableNATService lets this host help other peers (including
// buddies) run the same reachability check.
func baseOpts(privKey libp2pcrypto.PrivKey, port int) []libp2p.Option {
	return []libp2p.Option{
		libp2p.Identity(privKey),
		libp2p.ListenAddrStrings(
			fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", port),
			fmt.Sprintf("/ip4/0.0.0.0/udp/%d/quic-v1", port),
			fmt.Sprintf("/ip6/::/tcp/%d", port),
			fmt.Sprintf("/ip6/::/udp/%d/quic-v1", port),
		),
		libp2p.NATPortMap(),
		libp2p.EnableAutoNATv2(),
		libp2p.EnableNATService(),
		libp2p.EnableRelay(),
		libp2p.EnableHolePunching(),
		libp2p.DefaultTransports,
	}
}

// NewHost creates a libp2p host for one-shot operations (invite, backup,
// restore, maintenance) that only needs the public bootstrap peers as
// best-effort relay candidates. In practice the public bootstrap peers often
// refuse reservations (they rate-limit/reject circuit relay for abuse
// prevention), so this alone is not a guaranteed NAT-traversal path — see
// NewServeHost, which additionally lets buddies relay for each other.
// port 0 picks a random available port.
func NewHost(privKey libp2pcrypto.PrivKey, port int) (host.Host, error) {
	opts := append(baseOpts(privKey, port),
		libp2p.EnableAutoRelayWithStaticRelays(dht.GetDefaultBootstrapPeerAddrInfos()),
	)
	h, err := libp2p.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("p2p: new host: %w", err)
	}
	return h, nil
}

// NewServeHost creates the host used by the long-running `serve` daemon. In
// addition to everything NewHost does, it:
//   - offers circuit-v2 relay service, gated by reg so this host only ever
//     relays traffic for peers already in its own buddy list, never for
//     arbitrary DHT peers (bandwidth this host's owner didn't agree to spend);
//   - prefers reachable buddies (from reg) as AutoRelay candidates over the
//     public bootstrap peers, since a buddy who already knows and trusts this
//     node is far more likely to accept a reservation than a stranger.
// This is what lets two buddies behind separate NATs actually reach each
// other once at least one of them is a `serve` daemon and can act as a relay
// (either because AutoNAT confirms it's publicly reachable, or because its
// router happens to have a port forwarded).
// port 0 picks a random available port.
func NewServeHost(privKey libp2pcrypto.PrivKey, port int, reg *buddy.Registry) (host.Host, error) {
	acl := &buddyACL{reg: reg}
	opts := append(baseOpts(privKey, port),
		libp2p.EnableRelayService(relayServiceACLOption(acl)),
		libp2p.EnableAutoRelayWithPeerSource(buddyPeerSource(reg)),
	)
	h, err := libp2p.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("p2p: new serve host: %w", err)
	}
	return h, nil
}
