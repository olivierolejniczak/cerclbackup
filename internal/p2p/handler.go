package p2p

import (
	"context"
	"fmt"
	"log"

	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/cerclbackup/cerclbackup/internal/buddy"
	"github.com/cerclbackup/cerclbackup/internal/invite"
	"github.com/cerclbackup/cerclbackup/pkg/wire"
)

// RegisterHandlers attaches all CerclBackup protocol handlers to h.
func RegisterHandlers(h host.Host, reg *buddy.Registry, bs *buddy.Store, invMgr *invite.Manager) {
	h.SetStreamHandler(wire.ProtoShard, func(s network.Stream) {
		handleShard(s, h, reg, bs)
	})
	h.SetStreamHandler(wire.ProtoInvite, func(s network.Stream) {
		handleInvite(s, h, reg, invMgr)
	})
}

// handleShard receives a ShardPush from a buddy, verifies the sender is
// known, stores the shard, and sends back a ShardAck.
func handleShard(s network.Stream, h host.Host, reg *buddy.Registry, bs *buddy.Store) {
	defer s.Close()

	remotePeerID := s.Conn().RemotePeer().String()

	if !reg.IsKnown(remotePeerID) {
		log.Printf("[handler] shard from unknown peer %s — rejected", remotePeerID)
		_ = wire.WriteMsg(s, wire.ShardAck{
			Type:  wire.TypeShardAck,
			OK:    false,
			Error: "peer not in buddy registry",
		})
		return
	}

	var push wire.ShardPush
	if err := wire.ReadMsg(s, &push); err != nil {
		log.Printf("[handler] read shard from %s: %v", remotePeerID, err)
		return
	}

	if err := bs.Put(push.OwnerID, push.FileID, push.ShardIndex, push.Data); err != nil {
		log.Printf("[handler] store shard %s/%d: %v", push.FileID, push.ShardIndex, err)
		_ = wire.WriteMsg(s, wire.ShardAck{
			Type:       wire.TypeShardAck,
			FileID:     push.FileID,
			ShardIndex: push.ShardIndex,
			OK:         false,
			Error:      err.Error(),
		})
		return
	}

	log.Printf("[handler] stored shard %s/%d from %s", push.FileID, push.ShardIndex, remotePeerID)
	_ = wire.WriteMsg(s, wire.ShardAck{
		Type:       wire.TypeShardAck,
		FileID:     push.FileID,
		ShardIndex: push.ShardIndex,
		OK:         true,
	})
}

// handleInvite processes an InviteRequest: validates the token, then
// exchanges public keys and adds the joiner to the buddy registry.
func handleInvite(s network.Stream, h host.Host, reg *buddy.Registry, invMgr *invite.Manager) {
	defer s.Close()

	var req wire.InviteRequest
	if err := wire.ReadMsg(s, &req); err != nil {
		log.Printf("[handler] invite read: %v", err)
		return
	}

	if err := invMgr.Consume(req.Token); err != nil {
		log.Printf("[handler] invite token invalid: %v", err)
		_ = wire.WriteMsg(s, wire.InviteResponse{Type: wire.TypeInviteResponse, OK: false, Error: err.Error()})
		return
	}

	// Serialise own public key for the response
	ownPubBytes, err := libp2pcrypto.MarshalPublicKey(h.Peerstore().PubKey(h.ID()))
	if err != nil {
		log.Printf("[handler] marshal own pubkey: %v", err)
		_ = wire.WriteMsg(s, wire.InviteResponse{Type: wire.TypeInviteResponse, OK: false, Error: "internal error"})
		return
	}

	// Add joiner to registry
	if err := reg.Add(&buddy.Entry{
		PeerID:       req.PeerID,
		PubKey:       req.PubKey,
		FriendlyName: req.FriendlyName,
	}); err != nil {
		log.Printf("[handler] add buddy: %v", err)
	}

	_ = wire.WriteMsg(s, wire.InviteResponse{
		Type:   wire.TypeInviteResponse,
		OK:     true,
		PeerID: h.ID().String(),
		PubKey: ownPubBytes,
	})
	log.Printf("[handler] invite accepted for peer %s", req.PeerID)
}

// SendInviteRequest connects to targetAddr and presents the invite token.
// On success, adds the inviter to the buddy registry and returns.
func SendInviteRequest(ctx context.Context, h host.Host, reg *buddy.Registry,
	targetPeerID peer.ID, token []byte, friendlyName string) error {

	s, err := h.NewStream(ctx, targetPeerID, wire.ProtoInvite)
	if err != nil {
		return err
	}
	defer s.Close()

	ownPubBytes, err := libp2pcrypto.MarshalPublicKey(h.Peerstore().PubKey(h.ID()))
	if err != nil {
		return err
	}

	req := wire.InviteRequest{
		Type:         wire.TypeInviteRequest,
		Token:        token,
		PeerID:       h.ID().String(),
		PubKey:       ownPubBytes,
		FriendlyName: friendlyName,
	}
	if err := wire.WriteMsg(s, req); err != nil {
		return err
	}

	var resp wire.InviteResponse
	if err := wire.ReadMsg(s, &resp); err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("invite rejected: %s", resp.Error)
	}

	return reg.Add(&buddy.Entry{
		PeerID: resp.PeerID,
		PubKey: resp.PubKey,
	})
}

