package api

import (
	"fmt"
	"sync/atomic"
	"time"

	bbexclude "github.com/cerclbackup/cerclbackup/internal/exclude"
	"github.com/cerclbackup/cerclbackup/internal/watcher"
)

// WatchParams configures Watch.
type WatchParams struct {
	SrcDir     string
	StoreDir   string
	Password   string
	Buddies    int
	Debounce   time.Duration // 0 = 3s
	Exclude    string        // comma-separated glob patterns; "" = sensible default
	UploadKbps int
	AutoPrune  bool

	// Event, if set, is called once per file settled + backed up (or with a
	// non-empty Err on failure), and once per underlying Backup progress line.
	Event func(WatchEvent)
}

// WatchEvent describes one occurrence during a Watch session.
type WatchEvent struct {
	Path     string // triggering file, empty for plain progress lines
	Progress string // human-readable progress line
	Err      string // non-empty if backing up Path failed
}

// WatchHandle controls a running Watch session.
type WatchHandle struct {
	w *watcher.Watcher
}

// Stop halts the watcher.
func (h *WatchHandle) Stop() { h.w.Stop() }

// Watch monitors params.SrcDir for file changes and, after each file settles
// (per Debounce), runs Backup on it. It returns immediately with a handle to
// stop the watcher; call Stop when done.
func Watch(params WatchParams) (*WatchHandle, error) {
	if params.SrcDir == "" || params.Password == "" {
		return nil, fmt.Errorf("src dir and password are required")
	}
	debounce := params.Debounce
	if debounce == 0 {
		debounce = 3 * time.Second
	}
	exclude := params.Exclude
	if exclude == "" {
		exclude = ".git,node_modules,*.tmp,*.swp"
	}
	ef, err := bbexclude.Parse(exclude)
	if err != nil {
		return nil, fmt.Errorf("exclude: %w", err)
	}

	// Pre-flight: open keystore once so bad passwords fail fast.
	if _, err := OpenOrCreateKeystore(params.Password); err != nil {
		return nil, err
	}

	emit := func(ev WatchEvent) {
		if params.Event != nil {
			params.Event(ev)
		}
	}

	var watchedCount int64
	w, err := watcher.NewWithDebounce(params.SrcDir, debounce, func(path string) {
		if ef.Match(path) {
			return
		}
		n := atomic.AddInt64(&watchedCount, 1)
		emit(WatchEvent{Path: path, Progress: fmt.Sprintf("file %d: %s", n, path)})

		result, err := Backup(BackupParams{
			Src:        path,
			StoreDir:   params.StoreDir,
			Password:   params.Password,
			Buddies:    params.Buddies,
			Exclude:    exclude,
			UploadKbps: params.UploadKbps,
			AutoPrune:  params.AutoPrune,
			Progress: func(line string) {
				emit(WatchEvent{Path: path, Progress: line})
			},
		})
		if err != nil {
			emit(WatchEvent{Path: path, Err: err.Error()})
			return
		}
		for _, f := range result.Files {
			if f.Err != "" {
				emit(WatchEvent{Path: f.Path, Err: f.Err})
			}
		}
	})
	if err != nil {
		return nil, fmt.Errorf("init: %w", err)
	}

	if err := w.Start(); err != nil {
		return nil, err
	}

	return &WatchHandle{w: w}, nil
}
