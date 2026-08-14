package api

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/cerclbackup/cerclbackup/internal/buddy"
	p2pmod "github.com/cerclbackup/cerclbackup/internal/p2p"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
)

// BuddyList returns every registered buddy, without probing connectivity.
func BuddyList(password string) ([]*buddy.Entry, error) {
	if password == "" {
		return nil, fmt.Errorf("password is required")
	}
	ks, err := OpenKeystore(password)
	if err != nil {
		return nil, err
	}
	reg, err := OpenRegistry(ks)
	if err != nil {
		return nil, err
	}
	return reg.List(), nil
}

// BuddyStatusEntry is one buddy's connectivity probe result.
type BuddyStatusEntry struct {
	Entry   *buddy.Entry
	Online  bool
	Latency time.Duration
}

// BuddyStatus probes every registered buddy concurrently and reports whether
// each one is currently reachable, along with connect latency.
func BuddyStatus(password string, timeout time.Duration) ([]BuddyStatusEntry, error) {
	if password == "" {
		return nil, fmt.Errorf("password is required")
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	ks, err := OpenKeystore(password)
	if err != nil {
		return nil, err
	}
	reg, err := OpenRegistry(ks)
	if err != nil {
		return nil, err
	}
	buddies := reg.List()
	if len(buddies) == 0 {
		return nil, nil
	}

	privKey, err := p2pmod.EnsurePeerIdentity(ks, password)
	if err != nil {
		return nil, fmt.Errorf("peer identity: %w", err)
	}
	h, err := p2pmod.NewHost(privKey, 0)
	if err != nil {
		return nil, fmt.Errorf("host: %w", err)
	}
	defer h.Close()

	results := make([]BuddyStatusEntry, len(buddies))
	var wg sync.WaitGroup
	for i, e := range buddies {
		wg.Add(1)
		go func(idx int, entry *buddy.Entry) {
			defer wg.Done()
			pid, err := peer.Decode(entry.PeerID)
			if err != nil {
				results[idx] = BuddyStatusEntry{Entry: entry}
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			addrs := make([]multiaddr.Multiaddr, 0, len(entry.Addrs))
			for _, a := range entry.Addrs {
				if ma, err := multiaddr.NewMultiaddr(a); err == nil {
					addrs = append(addrs, ma)
				}
			}
			start := time.Now()
			err = h.Connect(ctx, peer.AddrInfo{ID: pid, Addrs: addrs})
			lat := time.Since(start)
			results[idx] = BuddyStatusEntry{Entry: entry, Online: err == nil, Latency: lat}
		}(i, e)
	}
	wg.Wait()
	return results, nil
}

// BuddyRemove removes a buddy by peer ID and, unless skipRebalance is set,
// redistributes shards across the remaining buddies.
func BuddyRemove(password, peerID string, skipRebalance bool) error {
	if password == "" || peerID == "" {
		return fmt.Errorf("password and peerID are required")
	}
	ks, err := OpenKeystore(password)
	if err != nil {
		return err
	}
	reg, err := OpenRegistry(ks)
	if err != nil {
		return fmt.Errorf("registry: %w", err)
	}
	if err := reg.Remove(peerID); err != nil {
		return err
	}
	if !skipRebalance {
		_, _ = Rebalance(password) // best-effort; rebalance failures don't undo the removal
	}
	return nil
}
