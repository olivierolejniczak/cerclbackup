package p2p

import (
	"context"
	"encoding/json"
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
	h.SetStreamHandler(wire.ProtoPush, func(s network.Stream) {
		handleShard(s, h, reg, bs)
	})
	h.SetStreamHandler(wire.ProtoPull, func(s network.Stream) {
		handlePull(s, h, reg, bs)
	})
	h.SetStreamHandler(wire.ProtoInvite, func(s network.Stream) {
		handleInvite(s, h, reg, invMgr)
	})
	h.SetStreamHandler(wire.ProtoManifest, func(s network.Stream) {
		handleManifest(s, reg, bs)
	})
}

// checkBuddyAuth verifies two things:
//  1. The remote peer is in the buddy registry (IsKnown).
//  2. The stored public key in the registry matches the key the remote peer
//     proved ownership of at the libp2p transport layer.
//
// For Ed25519 peers the public key is embedded in the peer ID, so
// peer.ID.ExtractPublicKey() recovers it without a peerstore lookup.
// Returns false (and logs) if either check fails.
func checkBuddyAuth(reg *buddy.Registry, remotePeer peer.ID) bool {
	peerIDStr := remotePeer.String()
	entry, ok := reg.Get(peerIDStr)
	if !ok {
		return false
	}

	if len(entry.PubKey) == 0 {
		// No key stored yet — allow but warn.  This covers entries added
		// before key verification was introduced; they should be re-added.
		log.Printf("[auth] warning: no pubkey stored for buddy %s, skipping verification", peerIDStr)
		return true
	}

	stored, err := libp2pcrypto.UnmarshalPublicKey(entry.PubKey)
	if err != nil {
		log.Printf("[auth] stored pubkey for %s is not a valid key: %v — rejecting", peerIDStr, err)
		return false
	}

	// Ed25519 peer IDs embed the public key; extract it from the ID itself.
	connPub, err := remotePeer.ExtractPublicKey()
	if err != nil || connPub == nil {
		log.Printf("[auth] cannot extract pubkey from peer ID %s: %v — rejecting", peerIDStr, err)
		return false
	}

	if !stored.Equals(connPub) {
		log.Printf("[auth] pubkey mismatch for peer %s — registry key differs from connection key — rejecting", peerIDStr)
		return false
	}
	return true
}

// handlePull serves a ShardRequest from a known, authenticated buddy.
func handlePull(s network.Stream, h host.Host, reg *buddy.Registry, bs *buddy.Store) {
	defer s.Close()
	_ = h // reserved for future peerstore lookups

	remotePeer := s.Conn().RemotePeer()
	remotePeerID := remotePeer.String()
	if !checkBuddyAuth(reg, remotePeer) {
		log.Printf("[handler] pull from unauthenticated peer %s — rejected", remotePeerID)
		_ = wire.WriteMsg(s, wire.ShardResponse{
			Type:  wire.TypeShardResponse,
			Found: false,
		})
		return
	}

	var req wire.ShardRequest
	if err := wire.ReadMsg(s, &req); err != nil {
		log.Printf("[handler] pull read from %s: %v", remotePeerID, err)
		return
	}

	data, err := bs.Get(req.OwnerID, req.FileID, req.ShardIndex)
	resp := wire.ShardResponse{
		Type:       wire.TypeShardResponse,
		FileID:     req.FileID,
		ShardIndex: req.ShardIndex,
	}
	if err == nil {
		resp.Data = data
		resp.Found = true
		log.Printf("[handler] served shard %s/%d to %s", req.FileID, req.ShardIndex, remotePeerID)
	}
	_ = wire.WriteMsg(s, resp)
}

// handleShard receives a ShardPush from a buddy, verifies the sender is
// known and authentic, stores the shard, and sends back a ShardAck.
func handleShard(s network.Stream, h host.Host, reg *buddy.Registry, bs *buddy.Store) {
	defer s.Close()
	_ = h // reserved for future peerstore lookups

	remotePeer := s.Conn().RemotePeer()
	remotePeerID := remotePeer.String()

	if !checkBuddyAuth(reg, remotePeer) {
		log.Printf("[handler] shard from unauthenticated peer %s — rejected", remotePeerID)
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

	if err := bs.PutWithHash(push.OwnerID, push.FileID, push.ShardIndex, push.Data); err != nil {
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

	// Accept either a 12-word BIP39 token (classic invite) or an 8-byte
	// pre-image whose SHA-256 matches a registered email-invite commitment.
	if err := invMgr.Consume(req.Token); err != nil {
		if err2 := invMgr.ConsumeCommitment(req.Token); err2 != nil {
			log.Printf("[handler] invite token invalid (token: %v, commitment: %v)", err, err2)
			_ = wire.WriteMsg(s, wire.InviteResponse{Type: wire.TypeInviteResponse, OK: false, Error: err.Error()})
			return
		}
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

// handleManifest serves both ManifestPush (store) and ManifestRequest (retrieve)
// on ProtoManifest.  The first message type field determines the operation.
func handleManifest(s network.Stream, reg *buddy.Registry, bs *buddy.Store) {
	defer s.Close()

	remotePeer := s.Conn().RemotePeer()
	if !checkBuddyAuth(reg, remotePeer) {
		log.Printf("[manifest] unauthenticated peer %s — rejected", remotePeer)
		return
	}

	// Read raw bytes once so we can inspect Type and then re-unmarshal.
	raw, err := wire.ReadRawMsg(s)
	if err != nil {
		log.Printf("[manifest] read from %s: %v", remotePeer, err)
		return
	}

	var env struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		log.Printf("[manifest] unmarshal envelope from %s: %v", remotePeer, err)
		return
	}

	switch env.Type {
	case wire.TypeManifestPush:
		var push wire.ManifestPush
		if err := json.Unmarshal(raw, &push); err != nil {
			log.Printf("[manifest] unmarshal push from %s: %v", remotePeer, err)
			_ = wire.WriteMsg(s, wire.ManifestAck{Type: wire.TypeManifestAck, OK: false, Error: "decode error"})
			return
		}
		if err := bs.PutManifest(push.OwnerID, push.Data); err != nil {
			log.Printf("[manifest] store manifest for %s: %v", push.OwnerID, err)
			_ = wire.WriteMsg(s, wire.ManifestAck{Type: wire.TypeManifestAck, OK: false, Error: err.Error()})
			return
		}
		log.Printf("[manifest] stored manifest for owner %s from %s (%d bytes)", push.OwnerID, remotePeer, len(push.Data))
		_ = wire.WriteMsg(s, wire.ManifestAck{Type: wire.TypeManifestAck, OK: true})

	case wire.TypeManifestRequest:
		var req wire.ManifestRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			log.Printf("[manifest] unmarshal request from %s: %v", remotePeer, err)
			return
		}
		data, err := bs.GetManifest(req.OwnerID)
		if err != nil {
			_ = wire.WriteMsg(s, wire.ManifestResponse{
				Type:    wire.TypeManifestResponse,
				OwnerID: req.OwnerID,
				Found:   false,
			})
			return
		}
		log.Printf("[manifest] served manifest for owner %s to %s", req.OwnerID, remotePeer)
		_ = wire.WriteMsg(s, wire.ManifestResponse{
			Type:    wire.TypeManifestResponse,
			OwnerID: req.OwnerID,
			Found:   true,
			Data:    data,
		})

	default:
		log.Printf("[manifest] unknown message type %q from %s", env.Type, remotePeer)
	}
}

