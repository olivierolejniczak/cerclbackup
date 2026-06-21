package wire

// Protocol IDs
const (
	ProtoShard  = "/cerclbackup/shard/1.0.0"
	ProtoInvite = "/cerclbackup/invite/1.0.0"
)

// MsgType discriminates wire messages.
type MsgType string

const (
	TypeShardPush      MsgType = "shard_push"
	TypeShardAck       MsgType = "shard_ack"
	TypeInviteRequest  MsgType = "invite_request"
	TypeInviteResponse MsgType = "invite_response"
	TypeWantList       MsgType = "want_list"
	TypeHaveList       MsgType = "have_list"
)

// ShardPush is sent by the backup owner to a buddy.
// Data is the raw encrypted shard bytes (opaque to the buddy).
type ShardPush struct {
	Type       MsgType `json:"type"`
	OwnerID    string  `json:"owner_id"`   // sender's PeerID string
	FileID     string  `json:"file_id"`    // store file ID (hex)
	ShardIndex int     `json:"shard_index"`
	IsParity   bool    `json:"is_parity"`
	Data       []byte  `json:"data"`
}

// ShardAck is the buddy's reply to a ShardPush.
type ShardAck struct {
	Type       MsgType `json:"type"`
	FileID     string  `json:"file_id"`
	ShardIndex int     `json:"shard_index"`
	OK         bool    `json:"ok"`
	Error      string  `json:"error,omitempty"`
}

// WantEntry identifies a shard the requester needs.
type WantEntry struct {
	OwnerID    string `json:"owner_id"`
	FileID     string `json:"file_id"`
	ShardIndex int    `json:"shard_index"`
}

// WantList asks the peer to send specific shards.
type WantList struct {
	Type    MsgType     `json:"type"`
	Entries []WantEntry `json:"entries"`
}

// HaveEntry reports whether a peer holds a shard.
type HaveEntry struct {
	OwnerID    string `json:"owner_id"`
	FileID     string `json:"file_id"`
	ShardIndex int    `json:"shard_index"`
	Available  bool   `json:"available"`
}

// HaveList is the response to a WantList.
type HaveList struct {
	Type    MsgType     `json:"type"`
	Entries []HaveEntry `json:"entries"`
}

// InviteRequest is sent by the joiner (Bob) to the inviter (Alice).
type InviteRequest struct {
	Type      MsgType `json:"type"`
	Token     []byte  `json:"token"`       // 16-byte random token
	PeerID    string  `json:"peer_id"`     // Bob's PeerID string
	PubKey    []byte  `json:"pub_key"`     // Bob's serialised libp2p pubkey
	FriendlyName string `json:"friendly_name,omitempty"`
}

// InviteResponse is Alice's reply.
type InviteResponse struct {
	Type   MsgType `json:"type"`
	OK     bool    `json:"ok"`
	PeerID string  `json:"peer_id,omitempty"` // Alice's PeerID string
	PubKey []byte  `json:"pub_key,omitempty"` // Alice's serialised libp2p pubkey
	Error  string  `json:"error,omitempty"`
}
