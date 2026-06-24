package archive_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/cerclbackup/cerclbackup/internal/archive"
	"github.com/cerclbackup/cerclbackup/pkg/protocol"
)

func makeEntry() *protocol.ManifestEntry {
	return &protocol.ManifestEntry{
		FileID:   "test-file-id",
		Path:     "/home/user/docs/report.pdf",
		Hash:     "aabbccdd",
		Size:     1024,
		Scheme:   protocol.RSScheme{DataShards: 3, ParityShards: 2},
		Version:  2,
		BackedAt: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
	}
}

func TestRoundtrip(t *testing.T) {
	entry := makeEntry()
	shards := [][]byte{
		[]byte("shard0data"),
		[]byte("shard1data"),
		[]byte("shard2data"),
		[]byte("shard3parity"),
		[]byte("shard4parity"),
	}

	var buf bytes.Buffer
	if err := archive.Write(&buf, entry, shards); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, gotShards, err := archive.Read(&buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if got.FileID != entry.FileID {
		t.Errorf("FileID: got %q, want %q", got.FileID, entry.FileID)
	}
	if got.Version != entry.Version {
		t.Errorf("Version: got %d, want %d", got.Version, entry.Version)
	}
	if len(gotShards) != len(shards) {
		t.Fatalf("shard count: got %d, want %d", len(gotShards), len(shards))
	}
	for i, s := range shards {
		if !bytes.Equal(gotShards[i], s) {
			t.Errorf("shard %d mismatch", i)
		}
	}
}

func TestNilShardToleratedOnWrite(t *testing.T) {
	entry := makeEntry()
	shards := [][]byte{
		[]byte("shard0"),
		nil, // missing parity — should write as empty
		[]byte("shard2"),
		nil,
		[]byte("shard4"),
	}

	var buf bytes.Buffer
	if err := archive.Write(&buf, entry, shards); err != nil {
		t.Fatalf("Write with nil shard: %v", err)
	}

	_, gotShards, err := archive.Read(&buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !bytes.Equal(gotShards[0], shards[0]) {
		t.Error("shard 0 mismatch")
	}
	// nil shards come back as empty []byte
	if len(gotShards[1]) != 0 {
		t.Errorf("nil shard 1 expected empty, got %d bytes", len(gotShards[1]))
	}
}

func TestWrongMagicRejected(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString("not a cbk file")
	_, _, err := archive.Read(&buf)
	if err == nil {
		t.Error("expected error for non-archive input")
	}
}

func TestFilename(t *testing.T) {
	entry := makeEntry()
	name := archive.Filename(entry)
	if name == "" {
		t.Fatal("Filename returned empty string")
	}
	if len(name) < 5 || name[len(name)-4:] != ".cbk" {
		t.Errorf("Filename %q does not end with .cbk", name)
	}
}
