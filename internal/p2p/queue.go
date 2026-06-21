package p2p

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/cerclbackup/cerclbackup/pkg/wire"
)

// PendingItem is a shard push queued for an offline buddy.
type PendingItem struct {
	PeerID string         `json:"peer_id"`
	Push   wire.ShardPush `json:"push"`
}

type queueData struct {
	Items []*PendingItem `json:"items"`
}

// Queue persists pending shard pushes for offline buddies.
type Queue struct {
	path string
	mu   sync.Mutex
}

// NewQueue creates a Queue backed by the given file.
func NewQueue(path string) *Queue {
	return &Queue{path: path}
}

// Enqueue adds a shard push to the queue for peerID.
func (q *Queue) Enqueue(peerID string, push wire.ShardPush) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	d, err := q.loadLocked()
	if err != nil {
		return err
	}
	d.Items = append(d.Items, &PendingItem{PeerID: peerID, Push: push})
	return q.saveLocked(d)
}

// FlushToPeer sends all queued shards for peerID and removes them on success.
func (q *Queue) FlushToPeer(ctx context.Context, h host.Host, peerID peer.ID) {
	q.mu.Lock()
	d, err := q.loadLocked()
	q.mu.Unlock()
	if err != nil {
		log.Printf("[queue] load: %v", err)
		return
	}

	peerIDStr := peerID.String()
	var remaining []*PendingItem
	for _, item := range d.Items {
		if item.PeerID != peerIDStr {
			remaining = append(remaining, item)
			continue
		}
		push := item.Push
		if err := PushShard(ctx, h, peerID, push.OwnerID, push.FileID, push.ShardIndex, push.IsParity, push.Data); err != nil {
			log.Printf("[queue] push shard %s/%d to %s: %v — keeping in queue",
				item.Push.FileID, item.Push.ShardIndex, peerIDStr, err)
			remaining = append(remaining, item)
		}
	}

	q.mu.Lock()
	d.Items = remaining
	_ = q.saveLocked(d)
	q.mu.Unlock()
}

func (q *Queue) loadLocked() (*queueData, error) {
	data, err := os.ReadFile(q.path)
	if os.IsNotExist(err) {
		return &queueData{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("queue: read: %w", err)
	}
	var d queueData
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("queue: unmarshal: %w", err)
	}
	return &d, nil
}

func (q *Queue) saveLocked(d *queueData) error {
	raw, err := json.Marshal(d)
	if err != nil {
		return fmt.Errorf("queue: marshal: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(q.path), 0700); err != nil {
		return fmt.Errorf("queue: mkdir: %w", err)
	}
	return os.WriteFile(q.path, raw, 0600)
}
