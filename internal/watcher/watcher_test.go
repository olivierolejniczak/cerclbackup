package watcher_test

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cerclbackup/cerclbackup/internal/watcher"
)

func TestDebounceCollapsesBurst(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "test.txt")

	var mu sync.Mutex
	var calls []string

	debounce := 80 * time.Millisecond
	w, err := watcher.NewWithDebounce(dir, debounce, func(p string) {
		mu.Lock()
		calls = append(calls, p)
		mu.Unlock()
	})
	if err != nil {
		t.Fatalf("NewWithDebounce: %v", err)
	}

	go func() { w.Start() }()
	t.Cleanup(w.Stop)

	// Write the file 5 times rapidly — should collapse to 1 callback.
	for i := 0; i < 5; i++ {
		os.WriteFile(file, []byte("burst"), 0o600)
		time.Sleep(10 * time.Millisecond)
	}

	time.Sleep(debounce + 100*time.Millisecond)

	mu.Lock()
	n := len(calls)
	mu.Unlock()

	if n != 1 {
		t.Errorf("expected 1 callback after burst, got %d", n)
	}
	if n > 0 && calls[0] != file {
		t.Errorf("callback path = %q, want %q", calls[0], file)
	}
}

func TestTwoDistinctFilesFireTwice(t *testing.T) {
	dir := t.TempDir()

	var mu sync.Mutex
	seen := make(map[string]bool)

	debounce := 80 * time.Millisecond
	w, err := watcher.NewWithDebounce(dir, debounce, func(p string) {
		mu.Lock()
		seen[p] = true
		mu.Unlock()
	})
	if err != nil {
		t.Fatalf("NewWithDebounce: %v", err)
	}
	go func() { w.Start() }()
	t.Cleanup(w.Stop)

	// Give the watcher a moment to register the directory before writing.
	time.Sleep(20 * time.Millisecond)

	fileA := filepath.Join(dir, "a.txt")
	fileB := filepath.Join(dir, "b.txt")
	os.WriteFile(fileA, []byte("a"), 0o600)
	os.WriteFile(fileB, []byte("b"), 0o600)

	// Wait well past the debounce window.
	time.Sleep(debounce + 200*time.Millisecond)

	mu.Lock()
	gotA, gotB := seen[fileA], seen[fileB]
	mu.Unlock()

	if !gotA {
		t.Errorf("expected callback for %q", fileA)
	}
	if !gotB {
		t.Errorf("expected callback for %q", fileB)
	}
}

func TestStopCancelsInflightTimers(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f.txt")

	var mu sync.Mutex
	var calls int

	debounce := 200 * time.Millisecond
	w, err := watcher.NewWithDebounce(dir, debounce, func(_ string) {
		mu.Lock()
		calls++
		mu.Unlock()
	})
	if err != nil {
		t.Fatalf("NewWithDebounce: %v", err)
	}
	go func() { w.Start() }()

	os.WriteFile(file, []byte("x"), 0o600)
	time.Sleep(20 * time.Millisecond)
	w.Stop()
	time.Sleep(debounce + 50*time.Millisecond)

	mu.Lock()
	n := calls
	mu.Unlock()

	if n != 0 {
		t.Errorf("expected 0 callbacks after Stop, got %d", n)
	}
}
