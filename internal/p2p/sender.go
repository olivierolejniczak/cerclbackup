package p2p

import (
	"context"
	"fmt"
	"log"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/cerclbackup/cerclbackup/pkg/wire"
)

// PushShard opens a stream to peerID and sends a single shard push.
// Returns an error if the peer is unreachable or rejects the shard.
func PushShard(ctx context.Context, h host.Host, peerID peer.ID, push wire.ShardPush) error {
	s, err := h.NewStream(ctx, peerID, wire.ProtoShard)
	if err != nil {
		return fmt.Errorf("p2p: open stream to %s: %w", peerID, err)
	}
	defer s.Close()

	if err := wire.WriteMsg(s, push); err != nil {
		return fmt.Errorf("p2p: send shard: %w", err)
	}

	var ack wire.ShardAck
	if err := wire.ReadMsg(s, &ack); err != nil {
		return fmt.Errorf("p2p: read ack: %w", err)
	}
	if !ack.OK {
		return fmt.Errorf("p2p: shard rejected by peer: %s", ack.Error)
	}
	log.Printf("[p2p] shard %s/%d → %s: ack OK", push.FileID, push.ShardIndex, peerID)
	return nil
}

// PushAllShards sends multiple shards to peerID, stopping on first error.
func PushAllShards(ctx context.Context, h host.Host, peerID peer.ID, pushes []wire.ShardPush) error {
	for _, p := range pushes {
		if err := PushShard(ctx, h, peerID, p); err != nil {
			return err
		}
	}
	return nil
}
