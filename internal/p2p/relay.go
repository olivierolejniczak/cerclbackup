package p2p

import (
	"context"

	"github.com/libp2p/go-libp2p/core/peer"
	relayv2 "github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"
	"github.com/libp2p/go-libp2p/p2p/host/autorelay"
	ma "github.com/multiformats/go-multiaddr"

	"github.com/cerclbackup/cerclbackup/internal/buddy"
)

// buddyACL restricts circuit-v2 relay reservations and connections to peers
// already present in the local buddy registry, so a serve daemon only ever
// spends bandwidth relaying for people its owner already trusts.
type buddyACL struct {
	reg *buddy.Registry
}

func (a *buddyACL) AllowReserve(p peer.ID, _ ma.Multiaddr) bool {
	return a.reg.IsKnown(p.String())
}

func (a *buddyACL) AllowConnect(src peer.ID, _ ma.Multiaddr, dest peer.ID) bool {
	return a.reg.IsKnown(src.String()) && a.reg.IsKnown(dest.String())
}

func relayServiceACLOption(acl *buddyACL) relayv2.Option {
	return relayv2.WithACL(acl)
}

// buddyPeerSource offers this host's own buddies as AutoRelay candidates,
// using their last-seen addresses from the registry. A buddy is far more
// likely to accept a reservation than a stranger on the public DHT, since
// checkBuddyAuth / buddyACL only let the two of them relay for each other.
func buddyPeerSource(reg *buddy.Registry) autorelay.PeerSource {
	return func(ctx context.Context, numPeers int) <-chan peer.AddrInfo {
		out := make(chan peer.AddrInfo)
		go func() {
			defer close(out)
			for _, e := range reg.List() {
				if numPeers <= 0 {
					return
				}
				pid, err := peer.Decode(e.PeerID)
				if err != nil {
					continue
				}
				var addrs []ma.Multiaddr
				for _, s := range e.Addrs {
					a, err := ma.NewMultiaddr(s)
					if err != nil {
						continue
					}
					addrs = append(addrs, a)
				}
				if len(addrs) == 0 {
					continue
				}
				select {
				case out <- peer.AddrInfo{ID: pid, Addrs: addrs}:
					numPeers--
				case <-ctx.Done():
					return
				}
			}
		}()
		return out
	}
}
