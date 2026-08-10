package storage_test

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/cerclbackup/cerclbackup/internal/storage"
)

func TestStore_PutGet(t *testing.T) {
	dir := t.TempDir()
	s, err := storage.New(dir)
	if err != nil {
		t.Fatal(err)
	}

	data := []byte("shard bytes")
	if err := s.Put("file-1", 0, false, data); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get("file-1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("data mismatch: got %q want %q", got, data)
	}
}

func TestStore_MetaTracksShards(t *testing.T) {
	dir := t.TempDir()
	s, err := storage.New(dir)
	if err != nil {
		t.Fatal(err)
	}

	if err := s.Put("file-1", 0, false, []byte("a")); err != nil {
		t.Fatal(err)
	}
	if err := s.Put("file-1", 1, true, []byte("bb")); err != nil {
		t.Fatal(err)
	}

	meta, err := s.Meta("file-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(meta) != 2 {
		t.Fatalf("expected 2 shard meta entries, got %d", len(meta))
	}
	var sawParity bool
	for _, m := range meta {
		if m.ShardIndex == 1 {
			sawParity = m.IsParity
		}
	}
	if !sawParity {
		t.Fatal("shard 1 should be recorded as parity")
	}
}

func TestStore_PutOverwritesExistingShard(t *testing.T) {
	dir := t.TempDir()
	s, err := storage.New(dir)
	if err != nil {
		t.Fatal(err)
	}

	if err := s.Put("file-1", 0, false, []byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := s.Put("file-1", 0, false, []byte("second-longer")); err != nil {
		t.Fatal(err)
	}

	got, err := s.Get("file-1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "second-longer" {
		t.Fatalf("expected overwritten data, got %q", got)
	}
	meta, err := s.Meta("file-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(meta) != 1 {
		t.Fatalf("overwrite should not duplicate meta entries, got %d", len(meta))
	}
}

func TestStore_DeleteRemovesFile(t *testing.T) {
	dir := t.TempDir()
	s, err := storage.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Put("file-1", 0, false, []byte("data")); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("file-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("file-1", 0); err == nil {
		t.Fatal("Get after Delete should fail")
	}
}

func TestStore_ListFiles(t *testing.T) {
	dir := t.TempDir()
	s, err := storage.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Put("file-a", 0, false, []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := s.Put("file-b", 0, false, []byte("y")); err != nil {
		t.Fatal(err)
	}

	ids, err := s.ListFiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 {
		t.Fatalf("expected 2 file ids, got %d: %v", len(ids), ids)
	}
}

func TestStore_DiskUsageBytes(t *testing.T) {
	dir := t.TempDir()
	s, err := storage.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Put("file-1", 0, false, []byte("12345")); err != nil {
		t.Fatal(err)
	}
	usage, err := s.DiskUsageBytes()
	if err != nil {
		t.Fatal(err)
	}
	if usage <= 0 {
		t.Fatalf("expected positive disk usage, got %d", usage)
	}
}

func TestNew_CreatesRootDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "store")
	if _, err := storage.New(dir); err != nil {
		t.Fatal(err)
	}
}
