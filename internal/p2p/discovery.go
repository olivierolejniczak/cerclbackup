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

// StartMDNS starts mDNS discovery. When a peer is found it is connected
// only if it is already in the buddy registry (TOFU — no auto-accept).
func StartMDNS(h host.Host, reg *buddy.Registry) (mdns.Service, error) {
	n := &buddyNotifee{host: h, reg: reg}
	svc := mdns.NewMdnsService(h, mdnsServiceTag, n)
	if err := svc.Start(); err != nil {
		return nil, err
	}
	return svc, nil
}

type buddyNotifee struct {
	host host.Host
	reg  *buddy.Registry
}

func (n *buddyNotifee) HandlePeerFound(info peer.AddrInfo) {
	if !n.reg.IsKnown(info.ID.String()) {
		return // unknown peer — ignore
	}
	// Update stored addresses
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
}
