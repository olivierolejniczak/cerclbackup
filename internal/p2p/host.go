package p2p

import (
	"fmt"

	"github.com/libp2p/go-libp2p"
	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
)

const DefaultPort = 7742

// NewHost creates a libp2p host with TCP + QUIC transports, TLS/Noise security,
// UPnP/NAT-PMP port mapping, and hole-punching support.
// port 0 picks a random available port.
func NewHost(privKey libp2pcrypto.PrivKey, port int) (host.Host, error) {
	h, err := libp2p.New(
		libp2p.Identity(privKey),
		libp2p.ListenAddrStrings(
			fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", port),
			fmt.Sprintf("/ip4/0.0.0.0/udp/%d/quic-v1", port),
			fmt.Sprintf("/ip6/::/tcp/%d", port),
			fmt.Sprintf("/ip6/::/udp/%d/quic-v1", port),
		),
		libp2p.NATPortMap(),
		libp2p.EnableHolePunching(),
		libp2p.DefaultTransports,
	)
	if err != nil {
		return nil, fmt.Errorf("p2p: new host: %w", err)
	}
	return h, nil
}
