package p2p

import (
	"context"
	"fmt"

	"github.com/cerclbackup/cerclbackup/pkg/wire"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
)

// PushShard sends one encrypted shard to the remote peer via the push protocol.
func PushShard(ctx context.Context, h host.Host, peerID peer.ID, ownerID, fileID string, shardIndex int, isParity bool, data []byte) error {
	s, err := h.NewStream(ctx, peerID, wire.ProtoPush)
	if err != nil {
		return fmt.Errorf("p2p: open push stream: %w", err)
	}
	defer s.Close()

	req := wire.ShardPush{
		Type:       wire.TypeShardPush,
		OwnerID:    ownerID,
		FileID:     fileID,
		ShardIndex: shardIndex,
		IsParity:   isParity,
		Data:       data,
	}
	if err := wire.WriteMsg(s, req); err != nil {
		return fmt.Errorf("p2p: write push: %w", err)
	}

	var ack wire.ShardAck
	if err := wire.ReadMsg(s, &ack); err != nil {
		return fmt.Errorf("p2p: read ack: %w", err)
	}
	if !ack.OK {
		return fmt.Errorf("p2p: buddy rejected shard %s/%d: %s", fileID, shardIndex, ack.Error)
	}
	return nil
}

// FetchShard requests one encrypted shard from the remote peer via the pull protocol.
func FetchShard(ctx context.Context, h host.Host, peerID peer.ID, ownerID, fileID string, idx int) ([]byte, error) {
	s, err := h.NewStream(ctx, peerID, wire.ProtoPull)
	if err != nil {
		return nil, fmt.Errorf("p2p: open pull stream: %w", err)
	}
	defer s.Close()

	req := wire.ShardRequest{
		Type:       wire.TypeShardRequest,
		OwnerID:    ownerID,
		FileID:     fileID,
		ShardIndex: idx,
	}
	if err := wire.WriteMsg(s, req); err != nil {
		return nil, err
	}
	var resp wire.ShardResponse
	if err := wire.ReadMsg(s, &resp); err != nil {
		return nil, err
	}
	if !resp.Found {
		return nil, fmt.Errorf("p2p: shard %s/%d not found at peer %s", fileID, idx, peerID)
	}
	return resp.Data, nil
}

