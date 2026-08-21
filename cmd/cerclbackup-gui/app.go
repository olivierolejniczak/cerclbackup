package main

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/cerclbackup/cerclbackup/internal/api"
	"github.com/cerclbackup/cerclbackup/internal/buddy"
	"github.com/cerclbackup/cerclbackup/internal/circle"
	"github.com/cerclbackup/cerclbackup/internal/rebalance"
	scrubpkg "github.com/cerclbackup/cerclbackup/internal/scrub"
	"github.com/cerclbackup/cerclbackup/pkg/protocol"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is the Wails-bound backend. All real work happens in internal/api;
// App only holds GUI session state (the unlocked password, running
// watch/serve handles) and translates long-running operations into
// runtime events for the frontend.
type App struct {
	ctx context.Context

	mu       sync.Mutex
	password string // empty until Unlock succeeds

	watchHandle *api.WatchHandle
	serveHandle *api.ServeHandle
}

func NewApp() *App {
	return &App{}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.startTray()
}

func (a *App) emit(event string, data ...interface{}) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, event, data...)
	}
}

func (a *App) getPassword() (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.password == "" {
		return "", fmt.Errorf("locked: unlock with your password first")
	}
	return a.password, nil
}

// ---- Setup / session ----

// IsInitialized reports whether a keystore already exists on this machine.
func (a *App) IsInitialized() bool {
	cfgDir, err := api.ConfigDir()
	if err != nil {
		return false
	}
	_, err = os.Stat(cfgDir + "/keystore.enc")
	return err == nil
}

// Unlock verifies password against the existing keystore and, on success,
// keeps it in memory for the rest of the GUI session.
func (a *App) Unlock(password string) error {
	if _, err := api.OpenKeystore(password); err != nil {
		return err
	}
	a.mu.Lock()
	a.password = password
	a.mu.Unlock()
	return nil
}

// Lock clears the in-memory session password.
func (a *App) Lock() {
	a.mu.Lock()
	a.password = ""
	a.mu.Unlock()
}

// IsUnlocked reports whether the session currently holds a password.
func (a *App) IsUnlocked() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.password != ""
}

// Init creates a fresh keystore/identity/circle and unlocks the session.
func (a *App) Init(params api.InitParams) (*api.InitResult, error) {
	res, err := api.Init(params)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	a.password = params.Password
	a.mu.Unlock()
	return res, nil
}

func (a *App) ShowPhrase() (string, error) {
	pw, err := a.getPassword()
	if err != nil {
		return "", err
	}
	return api.ShowPhrase(pw)
}

func (a *App) Recover(phrase, password string) (*api.RecoverResult, error) {
	res, err := api.Recover(phrase, password)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	a.password = password
	a.mu.Unlock()
	return res, nil
}

func (a *App) SetPassword(password string) error { return api.SetPassword(password) }
func (a *App) DeletePassword() error             { return api.DeletePassword() }

func (a *App) Passwd(oldPassword, newPassword string) error {
	if err := api.Passwd(oldPassword, newPassword); err != nil {
		return err
	}
	a.mu.Lock()
	a.password = newPassword
	a.mu.Unlock()
	return nil
}

// ConfigShowResult bundles the config contents with the path it was loaded
// from, since Wails bindings only support a single (value, error) return.
type ConfigShowResult struct {
	Config interface{}
	Path   string
}

func (a *App) ConfigShow() (ConfigShowResult, error) {
	cfg, path := api.ConfigShow()
	return ConfigShowResult{Config: cfg, Path: path}, nil
}

func (a *App) ConfigInit() (string, error) { return api.ConfigInit() }

// ---- Circle ----

func (a *App) CircleList() ([]*circle.Circle, error) {
	pw, err := a.getPassword()
	if err != nil {
		return nil, err
	}
	return api.CircleList(pw)
}

func (a *App) CircleAdd(name, scheme string) (*circle.Circle, error) {
	pw, err := a.getPassword()
	if err != nil {
		return nil, err
	}
	return api.CircleAdd(pw, name, scheme)
}

func (a *App) CircleRemove(name string) error {
	pw, err := a.getPassword()
	if err != nil {
		return err
	}
	return api.CircleRemove(pw, name)
}

// ---- Invite / Join ----

func (a *App) Invite(servePort int) (*api.InviteResult, error) {
	pw, err := a.getPassword()
	if err != nil {
		return nil, err
	}
	return api.Invite(pw, servePort)
}

func (a *App) Join(addr, words, name string, servePort int) (string, error) {
	pw, err := a.getPassword()
	if err != nil {
		return "", err
	}
	return api.Join(pw, addr, words, name, servePort)
}

func (a *App) InviteEmail(circleName, to string) (*api.InviteEmailResult, error) {
	pw, err := a.getPassword()
	if err != nil {
		return nil, err
	}
	return api.InviteEmail(api.InviteEmailParams{Password: pw, Circle: circleName}, to)
}

// JoinEmailResult is the outcome of a verified email invite join.
type JoinEmailResult struct {
	Circle string
	PeerID string
}

func (a *App) JoinEmail(payloadJSON []byte, words string) (*JoinEmailResult, error) {
	pw, err := a.getPassword()
	if err != nil {
		return nil, err
	}
	circleName, peerIDStr, err := api.JoinEmail(pw, payloadJSON, words)
	if err != nil {
		return nil, err
	}
	return &JoinEmailResult{Circle: circleName, PeerID: peerIDStr}, nil
}

// ---- Backup / Watch ----

// Backup runs a synchronous backup, emitting "backup:progress" events with
// each human-readable progress line as it happens.
func (a *App) Backup(storeDir string, buddies int, exclude string, uploadKbps int, autoPrune bool, srcPaths []string) (*api.BackupResult, error) {
	pw, err := a.getPassword()
	if err != nil {
		return nil, err
	}
	merged := &api.BackupResult{}
	for _, src := range srcPaths {
		res, err := api.Backup(api.BackupParams{
			Src:        src,
			StoreDir:   storeDir,
			Password:   pw,
			Buddies:    buddies,
			Exclude:    exclude,
			UploadKbps: uploadKbps,
			AutoPrune:  autoPrune,
			Progress: func(line string) {
				a.emit("backup:progress", line)
			},
		})
		if err != nil {
			a.emit("backup:progress", fmt.Sprintf("error: %v", err))
			continue
		}
		merged.Scheme = res.Scheme
		merged.Files = append(merged.Files, res.Files...)
		merged.PrunedVersions += res.PrunedVersions
		merged.PushedToBuddies += res.PushedToBuddies
	}
	return merged, nil
}

// StartWatch begins watching srcDir, emitting "watch:event" for every
// settled-file backup and progress line.
func (a *App) StartWatch(srcDir, storeDir string, buddies int, debounceSeconds int, exclude string, uploadKbps int, autoPrune bool) error {
	pw, err := a.getPassword()
	if err != nil {
		return err
	}
	a.mu.Lock()
	if a.watchHandle != nil {
		a.mu.Unlock()
		return fmt.Errorf("watch already running")
	}
	a.mu.Unlock()

	handle, err := api.Watch(api.WatchParams{
		SrcDir:     srcDir,
		StoreDir:   storeDir,
		Password:   pw,
		Buddies:    buddies,
		Debounce:   time.Duration(debounceSeconds) * time.Second,
		Exclude:    exclude,
		UploadKbps: uploadKbps,
		AutoPrune:  autoPrune,
		Event: func(ev api.WatchEvent) {
			a.emit("watch:event", ev)
		},
	})
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.watchHandle = handle
	a.mu.Unlock()
	return nil
}

func (a *App) StopWatch() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.watchHandle == nil {
		return fmt.Errorf("watch is not running")
	}
	a.watchHandle.Stop()
	a.watchHandle = nil
	return nil
}

func (a *App) IsWatching() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.watchHandle != nil
}

// ---- Restore ----

func (a *App) ListFiles(all bool) ([]*protocol.ManifestEntry, error) {
	pw, err := a.getPassword()
	if err != nil {
		return nil, err
	}
	return api.ListFiles(api.ListParams{Password: pw, All: all})
}

func (a *App) Versions(file string) ([]*protocol.ManifestEntry, error) {
	pw, err := a.getPassword()
	if err != nil {
		return nil, err
	}
	return api.Versions(pw, file)
}

func (a *App) Restore(storeDir, out, fileID, filePath string, version int) (*api.RestoreResult, error) {
	pw, err := a.getPassword()
	if err != nil {
		return nil, err
	}
	return api.Restore(api.RestoreParams{
		StoreDir: storeDir,
		Password: pw,
		Out:      out,
		FileID:   fileID,
		FilePath: filePath,
		Version:  version,
		Progress: func(line string) {
			a.emit("restore:progress", line)
		},
	})
}

// ---- Buddies ----

func (a *App) BuddyList() ([]*buddy.Entry, error) {
	pw, err := a.getPassword()
	if err != nil {
		return nil, err
	}
	return api.BuddyList(pw)
}

func (a *App) BuddyStatus(timeoutSeconds int) ([]api.BuddyStatusEntry, error) {
	pw, err := a.getPassword()
	if err != nil {
		return nil, err
	}
	return api.BuddyStatus(pw, time.Duration(timeoutSeconds)*time.Second)
}

func (a *App) BuddyRemove(peerID string, skipRebalance bool) error {
	pw, err := a.getPassword()
	if err != nil {
		return err
	}
	return api.BuddyRemove(pw, peerID, skipRebalance)
}

// ---- Maintenance ----

func (a *App) Rebalance() (rebalance.Result, error) {
	pw, err := a.getPassword()
	if err != nil {
		return rebalance.Result{}, err
	}
	return api.Rebalance(pw)
}

func (a *App) Audit(storeDir string) (*api.AuditResult, error) {
	pw, err := a.getPassword()
	if err != nil {
		return nil, err
	}
	return api.Audit(pw, storeDir)
}

func (a *App) Prune(keepAllDays, keepWeeklyDays, maxVersions int, dryRun bool, storeDir string) (*api.PruneResult, error) {
	pw, err := a.getPassword()
	if err != nil {
		return nil, err
	}
	return api.Prune(api.PruneParams{
		Password:       pw,
		KeepAllDays:    keepAllDays,
		KeepWeeklyDays: keepWeeklyDays,
		MaxVersions:    maxVersions,
		DryRun:         dryRun,
		StoreDir:       storeDir,
	})
}

func (a *App) Storage(storeDir string) (*api.StorageStats, error) {
	pw, err := a.getPassword()
	if err != nil {
		return nil, err
	}
	return api.Storage(pw, storeDir)
}

func (a *App) Scrub() (*scrubpkg.Report, error) {
	pw, err := a.getPassword()
	if err != nil {
		return nil, err
	}
	return api.Scrub(pw)
}

func (a *App) Diff(sinceUnixSeconds int64) ([]api.DiffChange, error) {
	pw, err := a.getPassword()
	if err != nil {
		return nil, err
	}
	return api.Diff(pw, time.Unix(sinceUnixSeconds, 0))
}

func (a *App) ManifestPull(addr, out string) (*api.ManifestPullResult, error) {
	pw, err := a.getPassword()
	if err != nil {
		return nil, err
	}
	return api.ManifestPull(pw, addr, out)
}

// ---- Data (export/import) ----

func (a *App) Export(filePath string, version int, outPath, storeDir string) (*api.ExportResult, error) {
	pw, err := a.getPassword()
	if err != nil {
		return nil, err
	}
	return api.Export(pw, filePath, version, outPath, storeDir)
}

func (a *App) Import(cbkPath, storeDir string) (*api.ImportResult, error) {
	pw, err := a.getPassword()
	if err != nil {
		return nil, err
	}
	return api.Import(pw, cbkPath, storeDir)
}

// ---- Doctor / Dashboard ----

func (a *App) Doctor(storeDir string, checkBuddies bool) (*api.DoctorResult, error) {
	pw, err := a.getPassword()
	if err != nil {
		return nil, err
	}
	return api.Doctor(api.DoctorParams{Password: pw, StoreDir: storeDir, CheckBuddies: checkBuddies})
}

func (a *App) Dashboard(storeDir string) (*api.DashboardResult, error) {
	pw, err := a.getPassword()
	if err != nil {
		return nil, err
	}
	return api.Dashboard(pw, storeDir)
}

// ---- Serve (daemon) ----

func (a *App) StartServe(port, uploadKbps int, healthAddr string) (*ServeStatus, error) {
	pw, err := a.getPassword()
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	if a.serveHandle != nil {
		a.mu.Unlock()
		return nil, fmt.Errorf("serve already running")
	}
	a.mu.Unlock()

	handle, err := api.StartServe(api.ServeParams{
		Password:   pw,
		Port:       port,
		UploadKbps: uploadKbps,
		HealthAddr: healthAddr,
	})
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	a.serveHandle = handle
	a.mu.Unlock()
	a.emit("serve:started", handle.PeerID)
	return &ServeStatus{Running: true, PeerID: handle.PeerID, Addrs: handle.Addrs}, nil
}

func (a *App) StopServe() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.serveHandle == nil {
		return fmt.Errorf("serve is not running")
	}
	a.serveHandle.Stop()
	a.serveHandle = nil
	a.emit("serve:stopped")
	return nil
}

// ServeStatus reports the currently running daemon's state (if any).
type ServeStatus struct {
	Running bool
	PeerID  string
	Addrs   []string
}

func (a *App) GetServeStatus() ServeStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.serveHandle == nil {
		return ServeStatus{Running: false}
	}
	return ServeStatus{Running: true, PeerID: a.serveHandle.PeerID, Addrs: a.serveHandle.Addrs}
}

// ---- Language preference (persisted alongside other CerclBackup config) ----

func (a *App) GetLanguage() string {
	cfgDir, err := api.ConfigDir()
	if err != nil {
		return "en"
	}
	data, err := os.ReadFile(cfgDir + "/gui-lang")
	if err != nil {
		return "en"
	}
	lang := string(data)
	if lang != "fr" && lang != "en" {
		return "en"
	}
	return lang
}

func (a *App) SetLanguage(lang string) error {
	if lang != "fr" && lang != "en" {
		return fmt.Errorf("unsupported language %q", lang)
	}
	cfgDir, err := api.ConfigDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(cfgDir+"/gui-lang", []byte(lang), 0o600)
}
