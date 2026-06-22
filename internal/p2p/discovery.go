package p2p

import (
	"context"
	"log"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"

	"github.com/cerclbackup/cerclbackup/internal/buddy"
)

const mdnsServiceTag = "_cerclbackup._tcp"

// StartMDNS starts mDNS LAN discovery. When a buddy is found it is connected
// and any queued shards for that peer are flushed immediately.
// q may be nil if offline queue is not used (e.g. in tests).
func StartMDNS(h host.Host, reg *buddy.Registry, q *Queue) (mdns.Service, error) {
	n := &buddyNotifee{host: h, reg: reg, queue: q}
	svc := mdns.NewMdnsService(h, mdnsServiceTag, n)
	if err := svc.Start(); err != nil {
		return nil, err
	}
	return svc, nil
}

type buddyNotifee struct {
	host  host.Host
	reg   *buddy.Registry
	queue *Queue
}

func (n *buddyNotifee) HandlePeerFound(info peer.AddrInfo) {
	if !n.reg.IsKnown(info.ID.String()) {
		return // unknown peer -- ignore (TOFU)
	}

	// Persist the freshly-discovered LAN addresses.
	addrStrs := make([]string, len(info.Addrs))
	for i, a := range info.Addrs {
		addrStrs[i] = a.String()
	}
	n.reg.UpdateAddrs(info.ID.String(), addrStrs)

	if err := n.host.Connect(context.Background(), info); err != nil {
		log.Printf("[mdns] connect to %s: %v", info.ID, err)
		return
	}
	log.Printf("[mdns] connected to buddy %s", info.ID)

	// Flush any shards queued while this buddy was offline.
	if n.queue != nil {
		go n.queue.FlushToPeer(context.Background(), n.host, info.ID)
	}
}
