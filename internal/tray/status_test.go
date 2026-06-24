package tray_test

import (
	"testing"
	"time"

	"github.com/cerclbackup/cerclbackup/internal/tray"
)

func TestWriteRead(t *testing.T) {
	dir := t.TempDir()
	want := tray.Status{
		LastBackupAt: time.Now().UTC().Truncate(time.Millisecond),
		LastFile:     "/home/user/documents/report.pdf",
	}
	if err := tray.Write(dir, want); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := tray.Read(dir)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !got.LastBackupAt.Equal(want.LastBackupAt) {
		t.Errorf("LastBackupAt: got %v, want %v", got.LastBackupAt, want.LastBackupAt)
	}
	if got.LastFile != want.LastFile {
		t.Errorf("LastFile: got %q, want %q", got.LastFile, want.LastFile)
	}
}

func TestReadMissing(t *testing.T) {
	dir := t.TempDir()
	s, err := tray.Read(dir)
	if err != nil {
		t.Fatalf("Read on empty dir: %v", err)
	}
	if !s.LastBackupAt.IsZero() {
		t.Error("expected zero LastBackupAt for missing status file")
	}
}

func TestWriteWithError(t *testing.T) {
	dir := t.TempDir()
	want := tray.Status{
		LastBackupAt: time.Now().UTC().Truncate(time.Millisecond),
		LastFile:     "/tmp/file.txt",
		Error:        "connection refused",
	}
	if err := tray.Write(dir, want); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := tray.Read(dir)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.Error != want.Error {
		t.Errorf("Error: got %q, want %q", got.Error, want.Error)
	}
}

func TestWriteIsAtomic(t *testing.T) {
	dir := t.TempDir()
	s1 := tray.Status{LastBackupAt: time.Now().UTC().Truncate(time.Millisecond), LastFile: "a"}
	s2 := tray.Status{LastBackupAt: time.Now().UTC().Truncate(time.Millisecond), LastFile: "b"}
	if err := tray.Write(dir, s1); err != nil {
		t.Fatal(err)
	}
	if err := tray.Write(dir, s2); err != nil {
		t.Fatal(err)
	}
	got, _ := tray.Read(dir)
	if got.LastFile != "b" {
		t.Errorf("second write did not win: got %q", got.LastFile)
	}
}
