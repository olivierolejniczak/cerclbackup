package p2p

import (
	"context"
	"fmt"
	"log"
	"sync"

	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
)

// StartDHT creates a Kademlia DHT in auto mode and bootstraps it using the
// default public libp2p bootstrap peers.
//
// ModeAuto: acts as a server when the host is publicly reachable (AutoNAT
// confirms external connectivity), and as a client-only when behind NAT.
// The caller owns the returned DHT and must call Close when done.
func StartDHT(ctx context.Context, h host.Host) (*dht.IpfsDHT, error) {
	d, err := dht.New(ctx, h, dht.Mode(dht.ModeAuto))
	if err != nil {
		return nil, fmt.Errorf("p2p: create DHT: %w", err)
	}

	bootstrapPeers := dht.GetDefaultBootstrapPeerAddrInfos()
	var wg sync.WaitGroup
	for _, pi := range bootstrapPeers {
		wg.Add(1)
		go func(pi peer.AddrInfo) {
			defer wg.Done()
			if err := h.Connect(ctx, pi); err != nil {
				log.Printf("[dht] bootstrap %s: %v", pi.ID, err)
			}
		}(pi)
	}
	wg.Wait()

	if err := d.Bootstrap(ctx); err != nil {
		return nil, fmt.Errorf("p2p: DHT bootstrap: %w", err)
	}
	log.Printf("[dht] bootstrapped (%d public peers attempted)", len(bootstrapPeers))
	return d, nil
}
