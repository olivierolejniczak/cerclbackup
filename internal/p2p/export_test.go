package p2p

import (
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/cerclbackup/cerclbackup/internal/buddy"
)

// SimulatePeerFound calls the mDNS notifee callback directly, bypassing
// multicast. Used by tests to trigger the connect+flush logic without
// requiring a real LAN multicast environment.
func SimulatePeerFound(h host.Host, reg *buddy.Registry, q *Queue, info peer.AddrInfo) {
	n := &buddyNotifee{host: h, reg: reg, queue: q}
	n.HandlePeerFound(info)
}
