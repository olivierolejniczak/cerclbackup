// Package manifdist distributes the owner's encrypted manifest to buddy nodes.
//
// The manifest blob is already AES-256-GCM encrypted by the manifest package;
// buddies store it opaquely and cannot read it.
//
// Push is called after every backup.  Pull is the first step in a recovery
// flow when the owner's local manifest file is missing or corrupted.
package manifdist

import (
	"context"
	"fmt"
	"log"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/cerclbackup/cerclbackup/pkg/wire"
)

// PushToBuddy sends the encrypted manifest blob to a single buddy.
// The stream is opened on ProtoManifest; the buddy stores the blob under
// ownerID and replies with a ManifestAck.
func PushToBuddy(ctx context.Context, h host.Host, buddyID peer.ID, ownerID string, data []byte) error {
	s, err := h.NewStream(ctx, buddyID, wire.ProtoManifest)
	if err != nil {
		return fmt.Errorf("manifdist: open stream to %s: %w", buddyID, err)
	}
	defer s.Close()

	msg := wire.ManifestPush{
		Type:    wire.TypeManifestPush,
		OwnerID: ownerID,
		Data:    data,
	}
	if err := wire.WriteMsg(s, msg); err != nil {
		return fmt.Errorf("manifdist: write push to %s: %w", buddyID, err)
	}

	var ack wire.ManifestAck
	if err := wire.ReadMsg(s, &ack); err != nil {
		return fmt.Errorf("manifdist: read ack from %s: %w", buddyID, err)
	}
	if !ack.OK {
		return fmt.Errorf("manifdist: buddy %s rejected manifest: %s", buddyID, ack.Error)
	}
	return nil
}

// PushToAll distributes data to every peer currently connected to h that
// speaks ProtoManifest.  Failures per-buddy are logged and skipped;
// the count of successful pushes is returned.
func PushToAll(ctx context.Context, h host.Host, ownerID string, data []byte) int {
	ok := 0
	for _, pid := range connectedManifestPeers(h) {
		if err := PushToBuddy(ctx, h, pid, ownerID, data); err != nil {
			log.Printf("[manifdist] push to %s: %v", pid, err)
			continue
		}
		log.Printf("[manifdist] pushed manifest to %s (%d bytes)", pid, len(data))
		ok++
	}
	return ok
}

// PullFromBuddy requests the manifest for ownerID from a specific buddy.
// Returns the encrypted blob on success, or an error if the buddy does not
// have it.
func PullFromBuddy(ctx context.Context, h host.Host, buddyID peer.ID, ownerID string) ([]byte, error) {
	s, err := h.NewStream(ctx, buddyID, wire.ProtoManifest)
	if err != nil {
		return nil, fmt.Errorf("manifdist: open stream to %s: %w", buddyID, err)
	}
	defer s.Close()

	req := wire.ManifestRequest{
		Type:    wire.TypeManifestRequest,
		OwnerID: ownerID,
	}
	if err := wire.WriteMsg(s, req); err != nil {
		return nil, fmt.Errorf("manifdist: write request to %s: %w", buddyID, err)
	}

	var resp wire.ManifestResponse
	if err := wire.ReadMsg(s, &resp); err != nil {
		return nil, fmt.Errorf("manifdist: read response from %s: %w", buddyID, err)
	}
	if !resp.Found {
		return nil, fmt.Errorf("manifdist: buddy %s has no manifest for %s", buddyID, ownerID)
	}
	return resp.Data, nil
}

// connectedManifestPeers returns IDs of currently connected peers.
// We attempt to push to all of them; PushToBuddy handles stream failures
// gracefully (non-CerclBackup peers simply refuse the protocol).
func connectedManifestPeers(h host.Host) []peer.ID {
	return h.Network().Peers()
}
