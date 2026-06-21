// Package watcher monitors file system paths and emits FileEvents.
// It wraps fsnotify with debouncing and temporary-file filtering.
package watcher

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cerclbackup/cerclbackup/pkg/protocol"
	"github.com/fsnotify/fsnotify"
)

const debounceDuration = 2 * time.Second

// ignoredPatterns lists filename patterns that should never trigger a backup.
var ignoredPatterns = []string{
	"~$",          // MS Office temp files
	".tmp",        // Generic temp files
	".swp",        // Vim swap
	".DS_Store",   // macOS metadata
	"desktop.ini", // Windows folder metadata
	"thumbs.db",
}

// Watcher monitors one or more directory trees and emits FileEvents.
type Watcher struct {
	fsw    *fsnotify.Watcher
	events chan protocol.FileEvent
	done   chan struct{}

	mu      sync.Mutex
	pending map[string]*time.Timer // debounce timers keyed by path
}

// New creates a Watcher and starts its internal goroutine.
func New() (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	w := &Watcher{
		fsw:     fsw,
		events:  make(chan protocol.FileEvent, 64),
		done:    make(chan struct{}),
		pending: make(map[string]*time.Timer),
	}
	go w.loop()
	return w, nil
}

// Watch adds path (and all sub-directories) to the watch list.
func (w *Watcher) Watch(path string) error {
	return filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return w.fsw.Add(p)
		}
		return nil
	})
}

// Events returns the channel on which FileEvents are delivered.
func (w *Watcher) Events() <-chan protocol.FileEvent {
	return w.events
}

// Stop shuts down the watcher and closes the Events channel.
func (w *Watcher) Stop() {
	close(w.done)
	w.fsw.Close()
}

// loop is the main goroutine: reads raw fsnotify events, debounces, and emits.
func (w *Watcher) loop() {
	defer close(w.events)
	for {
		select {
		case <-w.done:
			return

		case ev, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			if isIgnored(ev.Name) {
				continue
			}
			op := mapOp(ev.Op)
			if op < 0 {
				continue
			}
			w.debounce(ev.Name, protocol.Op(op))

		case err, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
			log.Printf("[watcher] error: %v", err)
		}
	}
}

// debounce resets the timer for path so that rapid consecutive writes
// produce only one FileEvent after debounceDuration of silence.
func (w *Watcher) debounce(path string, op protocol.Op) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if t, ok := w.pending[path]; ok {
		t.Stop()
	}
	w.pending[path] = time.AfterFunc(debounceDuration, func() {
		w.mu.Lock()
		delete(w.pending, path)
		w.mu.Unlock()

		select {
		case w.events <- protocol.FileEvent{
			Path:      path,
			Operation: op,
			Timestamp: time.Now(),
		}:
		case <-w.done:
		}
	})
}

// isIgnored returns true if the file should never trigger a backup.
func isIgnored(path string) bool {
	base := filepath.Base(path)
	for _, pat := range ignoredPatterns {
		if strings.HasPrefix(base, pat) || strings.HasSuffix(base, pat) {
			return true
		}
	}
	return false
}

// mapOp converts an fsnotify.Op to the protocol.Op equivalent.
// Returns -1 for operations we don't care about (Chmod).
func mapOp(op fsnotify.Op) int {
	switch {
	case op&fsnotify.Create != 0:
		return int(protocol.OpCreate)
	case op&fsnotify.Write != 0:
		return int(protocol.OpWrite)
	case op&fsnotify.Remove != 0:
		return int(protocol.OpDelete)
	case op&fsnotify.Rename != 0:
		return int(protocol.OpRename)
	default:
		return -1
	}
}
