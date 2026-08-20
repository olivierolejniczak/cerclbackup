package p2p

import (
	"fmt"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
)

const DefaultPort = 7742

// NewHost creates a libp2p host with TCP + QUIC transports, TLS/Noise security,
// UPnP/NAT-PMP port mapping, AutoNAT reachability detection, circuit-relay
// fallback, and hole-punching support.
//
// Most home routers don't support UPnP/NAT-PMP (NATPortMap does nothing) and
// have no port forwarded, so two buddies behind separate NATs can't reach
// each other directly. AutoNATv2 lets this host learn it's unreachable;
// AutoRelay then tries to reserve a relay slot on the default public
// bootstrap peers and advertise a relayed address so a buddy can dial in and
// attempt a direct hole-punch (DCUtR) afterwards. In practice the public
// bootstrap peers often refuse reservations (they rate-limit/reject circuit
// relay for abuse prevention), so this is a best-effort fallback, not a
// guaranteed traversal path — a real fix needs either a relay the user's
// buddies can reserve on, or a manual port-forward. EnableNATService lets
// this host help other peers (including buddies) run the same reachability
// check. port 0 picks a random available port.
func NewHost(privKey libp2pcrypto.PrivKey, port int) (host.Host, error) {
	relayCandidates := dht.GetDefaultBootstrapPeerAddrInfos()

	h, err := libp2p.New(
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
		libp2p.EnableAutoRelayWithStaticRelays(relayCandidates),
		libp2p.EnableHolePunching(),
		libp2p.DefaultTransports,
	)
	if err != nil {
		return nil, fmt.Errorf("p2p: new host: %w", err)
	}
	return h, nil
}
