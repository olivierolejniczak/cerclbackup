// Package rebalance redistributes locally-stored shards to all registered
// buddies. It is triggered after a buddy is revoked or a new buddy is added,
// ensuring every current buddy holds a complete replica of every shard.
package rebalance

import (
	"context"
	"fmt"
	"log"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/cerclbackup/cerclbackup/internal/buddy"
	"github.com/cerclbackup/cerclbackup/internal/p2p"
	"github.com/cerclbackup/cerclbackup/internal/storage"
)

// Result summarises a single rebalance pass.
type Result struct {
	FilesProcessed  int
	ShardsAttempted int
	ShardsOK        int
	Errors          []string
}

// Rebalancer pushes all locally-stored shards to all registered buddies.
type Rebalancer struct {
	ownerID    string
	localStore *storage.Store
	reg        *buddy.Registry
	h          host.Host
}

// New creates a Rebalancer.
//
// ownerID is the local peer's libp2p PeerID string (h.ID().String()).
// localStore holds the owner's own encrypted shards (written by runBackup).
// reg is the buddy registry; only currently registered buddies are targeted.
// h is used to open streams to buddies.
func New(ownerID string, localStore *storage.Store, reg *buddy.Registry, h host.Host) *Rebalancer {
	return &Rebalancer{
		ownerID:    ownerID,
		localStore: localStore,
		reg:        reg,
		h:          h,
	}
}

// Run pushes every shard for each fileID to every registered buddy.
// fileIDs come from the manifest (ManifestEntry.FileID) or from
// localStore.ListFiles().
// The operation is idempotent: a buddy that already holds the shard simply
// overwrites it with the same data.
func (r *Rebalancer) Run(ctx context.Context, fileIDs []string) (Result, error) {
	buddies := r.reg.List()
	if len(buddies) == 0 {
		return Result{}, nil
	}

	type buddyConn struct {
		entry  *buddy.Entry
		peerID peer.ID
	}
	conns := make([]buddyConn, 0, len(buddies))
	for _, e := range buddies {
		pid, err := peer.Decode(e.PeerID)
		if err != nil {
			log.Printf("[rebalance] skip buddy %s: invalid peer ID: %v", e.PeerID, err)
			continue
		}
		conns = append(conns, buddyConn{entry: e, peerID: pid})
	}

	var res Result

	for _, fileID := range fileIDs {
		if ctx.Err() != nil {
			break
		}

		metas, err := r.localStore.Meta(fileID)
		if err != nil || len(metas) == 0 {
			log.Printf("[rebalance] no local shards for file %s: %v", fileID, err)
			continue
		}

		res.FilesProcessed++

		for _, meta := range metas {
			data, err := r.localStore.Get(fileID, meta.ShardIndex)
			if err != nil {
				msg := fmt.Sprintf("read shard %s/%d: %v", fileID, meta.ShardIndex, err)
				log.Printf("[rebalance] %s", msg)
				res.Errors = append(res.Errors, msg)
				continue
			}

			for _, bc := range conns {
				res.ShardsAttempted++

				if r.h.Network().Connectedness(bc.peerID) != network.Connected {
					addrInfo := peer.AddrInfo{ID: bc.peerID}
					if err := r.h.Connect(ctx, addrInfo); err != nil {
						msg := fmt.Sprintf("connect to %s: %v", bc.entry.PeerID, err)
						log.Printf("[rebalance] %s", msg)
						res.Errors = append(res.Errors, msg)
						continue
					}
				}

				if err := p2p.PushShard(ctx, r.h, bc.peerID, r.ownerID, fileID, meta.ShardIndex, meta.IsParity, data); err != nil {
					msg := fmt.Sprintf("push shard %s/%d to %s: %v", fileID, meta.ShardIndex, bc.entry.PeerID, err)
					log.Printf("[rebalance] %s", msg)
					res.Errors = append(res.Errors, msg)
				} else {
					res.ShardsOK++
					log.Printf("[rebalance] pushed shard %s/%d to %s", fileID, meta.ShardIndex, bc.entry.PeerID)
				}
			}
		}
	}

	return res, nil
}
