// Package wire defines P2P message types and protocol IDs for CerclBackup.
package wire

// Protocol IDs for libp2p stream multiplexing.
const (
	ProtoPush     = "/cerclbackup/push/1.0.0"
	ProtoPull     = "/cerclbackup/pull/1.0.0"
	ProtoInvite   = "/cerclbackup/invite/1.0.0"
	ProtoManifest = "/cerclbackup/manifest/1.0.0"

	// ProtoShard is an alias for ProtoPush kept for compatibility.
	ProtoShard = ProtoPush
)

// Message type constants.
const (
	TypeShardPush     = "shard_push"
	TypeShardAck      = "shard_ack"
	TypeShardRequest  = "shard_request"
	TypeShardResponse = "shard_response"
	TypeInviteRequest    = "invite_request"
	TypeInviteResponse   = "invite_response"
	TypeManifestPush     = "manifest_push"
	TypeManifestAck      = "manifest_ack"
	TypeManifestRequest  = "manifest_request"
	TypeManifestResponse = "manifest_response"
)

// ShardPush is sent from a backup owner to a buddy to store one shard.
type ShardPush struct {
	Type       string `json:"type"`
	OwnerID    string `json:"owner_id"`
	FileID     string `json:"file_id"`
	ShardIndex int    `json:"shard_index"`
	IsParity   bool   `json:"is_parity"`
	Data       []byte `json:"data"`
}

// ShardAck is the buddy's response to a ShardPush.
type ShardAck struct {
	Type       string `json:"type"`
	FileID     string `json:"file_id"`
	ShardIndex int    `json:"shard_index"`
	OK         bool   `json:"ok"`
	Error      string `json:"error,omitempty"`
}

// ShardRequest is sent to fetch a shard from a buddy.
type ShardRequest struct {
	Type       string `json:"type"`
	OwnerID    string `json:"owner_id"`
	FileID     string `json:"file_id"`
	ShardIndex int    `json:"shard_index"`
}

// ShardResponse is the buddy's reply to a ShardRequest.
type ShardResponse struct {
	Type       string `json:"type"`
	FileID     string `json:"file_id"`
	ShardIndex int    `json:"shard_index"`
	Data       []byte `json:"data,omitempty"`
	Found      bool   `json:"found"`
}

// InviteRequest is sent by the joiner to the inviter.
type InviteRequest struct {
	Type         string `json:"type"`
	Token        []byte `json:"token"`
	PeerID       string `json:"peer_id"`
	PubKey       []byte `json:"pub_key"`
	FriendlyName string `json:"friendly_name,omitempty"`
}

// InviteResponse is the inviter's reply to an InviteRequest.
type InviteResponse struct {
	Type   string `json:"type"`
	OK     bool   `json:"ok"`
	PeerID string `json:"peer_id,omitempty"`
	PubKey []byte `json:"pub_key,omitempty"`
	Error  string `json:"error,omitempty"`
}

// ManifestPush delivers the owner's AES-256-GCM encrypted manifest blob to a buddy.
// The blob is opaque to the buddy — it cannot decrypt it without the master key.
type ManifestPush struct {
	Type    string `json:"type"`
	OwnerID string `json:"owner_id"`
	Data    []byte `json:"data"`
}

// ManifestAck is the buddy's confirmation of a received manifest blob.
type ManifestAck struct {
	Type  string `json:"type"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// ManifestRequest asks a buddy to return the stored manifest for OwnerID.
type ManifestRequest struct {
	Type    string `json:"type"`
	OwnerID string `json:"owner_id"`
}

// ManifestResponse carries the manifest blob (or Found=false if not stored).
type ManifestResponse struct {
	Type    string `json:"type"`
	OwnerID string `json:"owner_id"`
	Found   bool   `json:"found"`
	Data    []byte `json:"data,omitempty"`
}
