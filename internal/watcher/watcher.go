// Package watcher monitors a directory tree for file changes and fires a
// callback with a debounce window so that rapid saves (editor temp files,
// autosave bursts) collapse into a single backup trigger per file.
package watcher

import (
	"io/fs"
	"log"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const defaultDebounce = 3 * time.Second

// Handler is called with the canonical path of a changed file.
// It is invoked from a single goroutine; concurrent calls never happen.
type Handler func(path string)

// Watcher monitors srcDir recursively and calls h for each settled change.
type Watcher struct {
	srcDir   string
	debounce time.Duration
	handler  Handler
	fw       *fsnotify.Watcher
	stopCh   chan struct{}
}

// New creates a Watcher but does not start it.
// Call Start to begin monitoring.
func New(srcDir string, h Handler) (*Watcher, error) {
	return NewWithDebounce(srcDir, defaultDebounce, h)
}

// NewWithDebounce creates a Watcher with a custom debounce window.
func NewWithDebounce(srcDir string, debounce time.Duration, h Handler) (*Watcher, error) {
	abs, err := filepath.Abs(srcDir)
	if err != nil {
		return nil, err
	}
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	return &Watcher{
		srcDir:   abs,
		debounce: debounce,
		handler:  h,
		fw:       fw,
		stopCh:   make(chan struct{}),
	}, nil
}

// Start adds srcDir (and its subdirectories) to the OS watch list and
// begins dispatching events.  Blocks until Stop is called.
func (w *Watcher) Start() error {
	if err := addRecursive(w.fw, w.srcDir); err != nil {
		w.fw.Close()
		return err
	}
	w.run()
	return nil
}

// Stop shuts down the watcher.
func (w *Watcher) Stop() {
	close(w.stopCh)
	w.fw.Close()
}

// run is the event loop: it merges events per path, debounces, then fires h.
func (w *Watcher) run() {
	timers := make(map[string]*time.Timer)
	var mu sync.Mutex

	fire := func(path string) {
		mu.Lock()
		delete(timers, path)
		mu.Unlock()
		w.handler(path)
	}

	for {
		select {
		case <-w.stopCh:
			mu.Lock()
			for _, t := range timers {
				t.Stop()
			}
			mu.Unlock()
			return

		case ev, ok := <-w.fw.Events:
			if !ok {
				return
			}
			if ev.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Rename) == 0 {
				continue
			}
			abs := filepath.Clean(ev.Name)

			// If a new directory appeared, watch it too.
			if ev.Op&fsnotify.Create != 0 {
				w.fw.Add(abs)
			}

			mu.Lock()
			if t, exists := timers[abs]; exists {
				t.Reset(w.debounce)
			} else {
				p := abs
				timers[abs] = time.AfterFunc(w.debounce, func() { fire(p) })
			}
			mu.Unlock()

		case err, ok := <-w.fw.Errors:
			if !ok {
				return
			}
			log.Printf("[watcher] %v", err)
		}
	}
}

// addRecursive adds dir and every subdirectory to fw.
func addRecursive(fw *fsnotify.Watcher, dir string) error {
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return err
		}
		return fw.Add(path)
	})
}
