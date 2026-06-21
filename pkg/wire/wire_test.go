package wire_test

import (
	"bytes"
	"testing"

	"github.com/cerclbackup/cerclbackup/pkg/wire"
)

func TestFramingRoundtrip(t *testing.T) {
	orig := wire.ShardPush{
		Type:       wire.TypeShardPush,
		OwnerID:    "peer123",
		FileID:     "abc",
		ShardIndex: 2,
		IsParity:   true,
		Data:       []byte("hello shard"),
	}
	var buf bytes.Buffer
	if err := wire.WriteMsg(&buf, orig); err != nil {
		t.Fatal(err)
	}
	var got wire.ShardPush
	if err := wire.ReadMsg(&buf, &got); err != nil {
		t.Fatal(err)
	}
	if got.FileID != orig.FileID || got.ShardIndex != orig.ShardIndex {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
	if !bytes.Equal(got.Data, orig.Data) {
		t.Fatal("data mismatch")
	}
}

func TestFramingMultipleMessages(t *testing.T) {
	var buf bytes.Buffer
	for i := 0; i < 5; i++ {
		_ = wire.WriteMsg(&buf, wire.ShardAck{Type: wire.TypeShardAck, ShardIndex: i, OK: true})
	}
	for i := 0; i < 5; i++ {
		var ack wire.ShardAck
		if err := wire.ReadMsg(&buf, &ack); err != nil {
			t.Fatalf("msg %d: %v", i, err)
		}
		if ack.ShardIndex != i {
			t.Fatalf("msg %d: got shard_index %d", i, ack.ShardIndex)
		}
	}
}
