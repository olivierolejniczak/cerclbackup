// Package scrub periodically verifies the integrity of shards stored on
// behalf of buddies and silently re-fetches any that are corrupted or missing.
package scrub

import (
	"context"
	"log"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/cerclbackup/cerclbackup/internal/buddy"
	"github.com/cerclbackup/cerclbackup/internal/p2p"
)

// Report summarises a single scrub pass.
type Report struct {
	Checked   int // shards examined
	OK        int // shards that passed the hash check
	Corrupted int // shards with a bad or missing hash
	Revived   int // corrupted shards successfully re-fetched from the owner
	Failed    int // corrupted shards that could not be revived
}

// Manager runs periodic scrub passes over the shards stored in a buddy store.
type Manager struct {
	bs  *buddy.Store
	h   host.Host
	reg *buddy.Registry
}

// New creates a Manager. h and reg are used for Silent Revive: when a shard
// fails its hash check, the manager attempts to fetch a fresh copy from the
// shard owner via the pull protocol.
func New(bs *buddy.Store, h host.Host, reg *buddy.Registry) *Manager {
	return &Manager{bs: bs, h: h, reg: reg}
}

// Start launches a background goroutine that calls RunOnce every interval.
// It stops when ctx is cancelled.
func (m *Manager) Start(ctx context.Context, interval time.Duration) {
	go func() {
		// Run once immediately at startup, then on the ticker.
		m.runAndLog(ctx)
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				m.runAndLog(ctx)
			}
		}
	}()
}

func (m *Manager) runAndLog(ctx context.Context) {
	r, err := m.RunOnce(ctx)
	if err != nil {
		log.Printf("[scrub] error: %v", err)
		return
	}
	if r.Corrupted == 0 {
		log.Printf("[scrub] ok: %d shards checked, all healthy", r.Checked)
	} else {
		log.Printf("[scrub] %d/%d shards corrupted -- revived %d, failed %d",
			r.Corrupted, r.Checked, r.Revived, r.Failed)
	}
}

// RunOnce performs a single scrub pass and returns the report.
func (m *Manager) RunOnce(ctx context.Context) (Report, error) {
	refs, err := m.bs.ListAll()
	if err != nil {
		return Report{}, err
	}

	var r Report
	r.Checked = len(refs)

	for _, ref := range refs {
		if ctx.Err() != nil {
			break
		}
		if m.bs.Verify(ref.OwnerPeerID, ref.FileID, ref.ShardIndex) {
			r.OK++
			continue
		}

		r.Corrupted++
		log.Printf("[scrub] corrupted shard %s/%s/%d -- attempting revive",
			ref.OwnerPeerID, ref.FileID, ref.ShardIndex)

		if m.revive(ctx, ref) {
			r.Revived++
		} else {
			r.Failed++
			log.Printf("[scrub] revive failed for %s/%s/%d", ref.OwnerPeerID, ref.FileID, ref.ShardIndex)
		}
	}

	return r, nil
}

// revive tries to fetch a fresh copy of the shard from the owner or any
// known buddy that might have it, then re-stores it.
func (m *Manager) revive(ctx context.Context, ref buddy.ShardRef) bool {
	// ownerPeerID is also the peer to ask -- the owner keeps local copies.
	ownerPeerID, err := peer.Decode(ref.OwnerPeerID)
	if err != nil {
		return false
	}

	// Ensure we are connected to the owner.
	if m.h.Network().Connectedness(ownerPeerID) != network.Connected {
		entry, ok := m.reg.Get(ref.OwnerPeerID)
		if !ok {
			return false
		}
		addrInfo := peer.AddrInfo{ID: ownerPeerID}
		for _, a := range entry.Addrs {
			_ = a // address parsing deferred -- Connect will use peerstore
		}
		if err := m.h.Connect(ctx, addrInfo); err != nil {
			return false
		}
	}

	data, err := p2p.FetchShard(ctx, m.h, ownerPeerID, ref.OwnerPeerID, ref.FileID, ref.ShardIndex)
	if err != nil {
		return false
	}

	if err := m.bs.PutWithHash(ref.OwnerPeerID, ref.FileID, ref.ShardIndex, data); err != nil {
		log.Printf("[scrub] re-store shard %s/%d: %v", ref.FileID, ref.ShardIndex, err)
		return false
	}

	log.Printf("[scrub] revived shard %s/%s/%d", ref.OwnerPeerID, ref.FileID, ref.ShardIndex)
	return true
}
