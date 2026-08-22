package p2p

import (
	"context"
	"fmt"
	"log"
	"time"

	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"

	"github.com/cerclbackup/cerclbackup/internal/buddy"
)

// DefaultRedialInterval is how often PeriodicDialAllBuddies retries buddies
// it isn't currently connected to. h.Connect is a no-op for peers already
// connected, so this is safe to run indefinitely without disrupting healthy
// connections.
const DefaultRedialInterval = 10 * time.Minute

// DialBuddy connects to a registered buddy using a two-step strategy:
//
//  1. Stored addresses: if the registry has a non-empty Addrs list, try those first
//     (fast path for recently-seen peers on the same LAN or with stable public IPs).
//  2. DHT lookup: if stored addresses fail or are absent, query the DHT for current
//     addresses, connect, and persist discovered addresses back to the registry.
//
// A nil dht skips step 2 (useful in tests or offline mode).
func DialBuddy(ctx context.Context, h host.Host, d *dht.IpfsDHT, reg *buddy.Registry, buddyID peer.ID) error {
	// Fast path: try stored addresses.
	entry, ok := reg.Get(buddyID.String())
	if !ok {
		return fmt.Errorf("dialer: buddy %s not in registry", buddyID)
	}

	if len(entry.Addrs) > 0 {
		addrs := parseAddrs(entry.Addrs)
		if err := h.Connect(ctx, peer.AddrInfo{ID: buddyID, Addrs: addrs}); err == nil {
			log.Printf("[dialer] connected to %s via stored addrs", buddyID)
			return nil
		}
		log.Printf("[dialer] stored addrs failed for %s, trying DHT", buddyID)
	}

	// DHT lookup.
	if d == nil {
		return fmt.Errorf("dialer: no stored addrs and DHT is unavailable for %s", buddyID)
	}
	pi, err := d.FindPeer(ctx, buddyID)
	if err != nil {
		return fmt.Errorf("dialer: DHT lookup for %s: %w", buddyID, err)
	}
	if err := h.Connect(ctx, pi); err != nil {
		return fmt.Errorf("dialer: connect to %s: %w", buddyID, err)
	}
	log.Printf("[dialer] connected to %s via DHT discovery", buddyID)

	// Persist discovered addresses for future fast-path hits.
	newAddrs := make([]string, 0, len(pi.Addrs))
	for _, a := range pi.Addrs {
		newAddrs = append(newAddrs, a.String())
	}
	reg.UpdateAddrs(buddyID.String(), newAddrs)
	return nil
}

// DialAllBuddies attempts DialBuddy for every entry in the registry.
// Errors are logged and skipped; the function never returns an error.
func DialAllBuddies(ctx context.Context, h host.Host, d *dht.IpfsDHT, reg *buddy.Registry) {
	entries := reg.List()
	for _, entry := range entries {
		pid, err := peer.Decode(entry.PeerID)
		if err != nil {
			log.Printf("[dialer] invalid peer ID %q: %v", entry.PeerID, err)
			continue
		}
		if err := DialBuddy(ctx, h, d, reg, pid); err != nil {
			log.Printf("[dialer] could not reach %s (%s): %v", entry.FriendlyName, pid, err)
		}
	}
}

// PeriodicDialAllBuddies runs DialAllBuddies immediately and then every
// interval until ctx is cancelled. This is what lets a long-running serve
// daemon recover from a buddy's address changing (stale stored address,
// buddy's WAN IP changed, etc.) without requiring a manual daemon restart —
// previously DialAllBuddies only ran once, at startup.
func PeriodicDialAllBuddies(ctx context.Context, h host.Host, d *dht.IpfsDHT, reg *buddy.Registry, interval time.Duration) {
	DialAllBuddies(ctx, h, d, reg)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			DialAllBuddies(ctx, h, d, reg)
		}
	}
}

func parseAddrs(raw []string) []multiaddr.Multiaddr {
	out := make([]multiaddr.Multiaddr, 0, len(raw))
	for _, s := range raw {
		ma, err := multiaddr.NewMultiaddr(s)
		if err == nil {
			out = append(out, ma)
		}
	}
	return out
}
