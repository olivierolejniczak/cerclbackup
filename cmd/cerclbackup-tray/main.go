// cerclbackup-tray is the system-tray companion for CerclBackup.
// On launch it starts cerclbackup serve and cerclbackup watch as hidden child
// processes, restarts them if they crash, and exposes a minimal menu for
// status, one-shot backup, folder selection, and log access.
// Build for Windows: go build -ldflags "-H=windowsgui" ./cmd/cerclbackup-tray
package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"fyne.io/systray"

	traystatus "github.com/cerclbackup/cerclbackup/internal/tray"
)

// daemon manages the cerclbackup serve and watch child processes.
type daemon struct {
	exe      string
	password string

	mu       sync.Mutex
	watchSrc string
	serveCmd *exec.Cmd
	watchCmd *exec.Cmd
	quit     chan struct{}

	mServe *systray.MenuItem
	mWatch *systray.MenuItem
}

func newDaemon(exe, password, watchSrc string) *daemon {
	return &daemon{
		exe:      exe,
		password: password,
		watchSrc: watchSrc,
		quit:     make(chan struct{}),
	}
}

// stop signals all loops to exit and kills child processes.
func (d *daemon) stop() {
	select {
	case <-d.quit:
	default:
		close(d.quit)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	killProcess(d.serveCmd)
	killProcess(d.watchCmd)
}

// setWatchSrc switches the watched directory; kills the running watch process
// so the restart loop picks up the new path immediately.
func (d *daemon) setWatchSrc(src string) {
	d.mu.Lock()
	d.watchSrc = src
	old := d.watchCmd
	d.mu.Unlock()
	killProcess(old)
}

func killProcess(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil {
		cmd.Process.Kill()
	}
}

// runServe starts cerclbackup serve in a restart loop (5 s back-off).
func (d *daemon) runServe(logDir string) {
	const backoff = 5 * time.Second
	for {
		select {
		case <-d.quit:
			return
		default:
		}

		args := []string{"serve"}
		if d.password != "" {
			args = append(args, "--password", d.password)
		}
		cmd := exec.Command(d.exe, args...)
		hideWindow(cmd)
		attachLog(cmd, filepath.Join(logDir, "serve.log"))

		d.mu.Lock()
		d.serveCmd = cmd
		d.mu.Unlock()

		if err := cmd.Start(); err != nil {
			log.Printf("tray: serve start: %v", err)
			d.mServe.SetTitle("Serve: failed to start")
		} else {
			d.mServe.SetTitle("Serve: running")
			cmd.Wait()
			select {
			case <-d.quit:
				d.mServe.SetTitle("Serve: stopped")
				return
			default:
				d.mServe.SetTitle("Serve: restarting...")
			}
		}

		select {
		case <-d.quit:
			return
		case <-time.After(backoff):
		}
	}
}

// runWatch starts cerclbackup watch in a restart loop (10 s back-off).
// Polls every 5 s when no folder is configured yet.
func (d *daemon) runWatch(logDir string) {
	const backoff = 10 * time.Second
	for {
		select {
		case <-d.quit:
			return
		default:
		}

		d.mu.Lock()
		src := d.watchSrc
		d.mu.Unlock()

		if src == "" || d.password == "" {
			d.mWatch.SetTitle(watchLabel(src, d.password))
			select {
			case <-d.quit:
				return
			case <-time.After(5 * time.Second):
			}
			continue
		}

		cmd := exec.Command(d.exe, "watch", "--src", src, "--password", d.password)
		hideWindow(cmd)
		attachLog(cmd, filepath.Join(logDir, "watch.log"))

		d.mu.Lock()
		d.watchCmd = cmd
		d.mu.Unlock()

		d.mWatch.SetTitle("Watch: " + shortPath(src))

		if err := cmd.Start(); err != nil {
			log.Printf("tray: watch start: %v", err)
			d.mWatch.SetTitle("Watch: failed to start")
		} else {
			cmd.Wait()
			select {
			case <-d.quit:
				d.mWatch.SetTitle("Watch: stopped")
				return
			default:
				d.mWatch.SetTitle("Watch: restarting...")
			}
		}

		select {
		case <-d.quit:
			return
		case <-time.After(backoff):
		}
	}
}

// attachLog redirects cmd stdout+stderr to a log file (append mode).
func attachLog(cmd *exec.Cmd, path string) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	cmd.Stdout = f
	cmd.Stderr = f
}

// ── tray wiring ──────────────────────────────────────────────────────────────

var dm *daemon

func main() {
	systray.Run(onReady, onExit)
}

func onReady() {
	systray.SetIcon(iconICO())
	systray.SetTitle("CerclBackup")
	systray.SetTooltip("CerclBackup — P2P encrypted backup")

	exe, _ := siblingBinary("cerclbackup")
	cfgDir, _ := cerclConfigDir()
	logDir := filepath.Join(cfgDir, "logs")
	os.MkdirAll(logDir, 0o700)

	cfg, _ := traystatus.ReadConfig(cfgDir)
	password := os.Getenv("CERCLBACKUP_PASSWORD")

	dm = newDaemon(exe, password, cfg.WatchSrc)

	// Status items — display only (always disabled).
	mStatus := systray.AddMenuItem("Status: checking...", "Last backup status")
	mStatus.Disable()
	mServe := systray.AddMenuItem("Serve: starting...", "P2P daemon state")
	mServe.Disable()
	mWatch := systray.AddMenuItem(watchLabel(cfg.WatchSrc, password), "File-watch daemon state")
	mWatch.Disable()
	systray.AddSeparator()

	// Action items.
	mSetup  := systray.AddMenuItem("Initialize...", "First-time setup: create keystore")
	mBackup := systray.AddMenuItem("Backup Now", "Run an immediate backup")
	mFolder := systray.AddMenuItem("Set backup folder...", "Choose the directory to auto-backup")
	mLogs   := systray.AddMenuItem("Open Logs", "Open log directory")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit", "Stop CerclBackup tray and daemons")

	dm.mServe = mServe
	dm.mWatch = mWatch

	if exe != "" {
		go dm.runServe(logDir)
		go dm.runWatch(logDir)
	} else {
		mServe.SetTitle("Serve: cerclbackup not found")
	}

	go func() {
		for {
			select {
			case <-mSetup.ClickedCh:
				openSetup()
			case <-mBackup.ClickedCh:
				go runBackup(mStatus)
			case <-mFolder.ClickedCh:
				go pickAndSaveFolder(cfgDir, dm)
			case <-mLogs.ClickedCh:
				openLogs()
			case <-mQuit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()

	go pollStatus(mStatus)
}

func onExit() {
	if dm != nil {
		dm.stop()
	}
}

// ── menu helpers ─────────────────────────────────────────────────────────────

func pollStatus(item *systray.MenuItem) {
	for {
		updateStatus(item)
		time.Sleep(30 * time.Second)
	}
}

func updateStatus(item *systray.MenuItem) {
	dir, err := cerclConfigDir()
	if err != nil {
		item.SetTitle("Status: config error")
		return
	}
	s, err := traystatus.Read(dir)
	if err != nil {
		item.SetTitle("Status: unreadable")
		return
	}
	if s.LastBackupAt.IsZero() {
		item.SetTitle("Status: no backup yet")
		return
	}
	age := time.Since(s.LastBackupAt)
	if s.Error != "" {
		item.SetTitle(fmt.Sprintf("Status: error %s ago", formatAge(age)))
	} else {
		item.SetTitle(fmt.Sprintf("Status: backed up %s ago", formatAge(age)))
	}
}

func formatAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func watchLabel(src, password string) string {
	if src == "" {
		return "Watch: click 'Set backup folder'"
	}
	if password == "" {
		return "Watch: set CERCLBACKUP_PASSWORD"
	}
	return "Watch: " + shortPath(src)
}

func shortPath(p string) string {
	if home, err := os.UserHomeDir(); err == nil {
		if rel, err := filepath.Rel(home, p); err == nil && !strings.HasPrefix(rel, "..") {
			return "~" + string(filepath.Separator) + rel
		}
	}
	if len(p) > 40 {
		return "..." + p[len(p)-37:]
	}
	return p
}

// ── actions ───────────────────────────────────────────────────────────────────

func runBackup(mStatus *systray.MenuItem) {
	mStatus.SetTitle("Status: backup running...")
	exe, err := siblingBinary("cerclbackup")
	if err != nil {
		mStatus.SetTitle("Status: cerclbackup not found")
		return
	}
	password := os.Getenv("CERCLBACKUP_PASSWORD")
	if password == "" {
		mStatus.SetTitle("Status: set CERCLBACKUP_PASSWORD")
		return
	}
	// Prefer CERCLBACKUP_SRC env var; fall back to configured watch folder.
	src := os.Getenv("CERCLBACKUP_SRC")
	if src == "" && dm != nil {
		dm.mu.Lock()
		src = dm.watchSrc
		dm.mu.Unlock()
	}
	if src == "" {
		mStatus.SetTitle("Status: no backup folder configured")
		return
	}
	cmd := exec.Command(exe, "backup", "--src", src, "--password", password)
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Printf("tray: backup: %v\n%s", err, out)
		mStatus.SetTitle("Status: backup failed")
		return
	}
	updateStatus(mStatus)
}

// pickAndSaveFolder opens a native folder picker, persists the selection,
// and restarts the watch daemon with the new directory.
func pickAndSaveFolder(cfgDir string, d *daemon) {
	src, err := pickFolder()
	if err != nil {
		log.Printf("tray: folder picker: %v", err)
		return
	}
	if src == "" {
		return // user cancelled
	}
	cfg := traystatus.TrayConfig{WatchSrc: src}
	if err := traystatus.WriteConfig(cfgDir, cfg); err != nil {
		log.Printf("tray: save config: %v", err)
		return
	}
	d.setWatchSrc(src)
}

// pickFolder shows a native OS folder picker and returns the selected path.
func pickFolder() (string, error) {
	switch runtime.GOOS {
	case "windows":
		script := `Add-Type -AssemblyName System.Windows.Forms; ` +
			`$d = New-Object System.Windows.Forms.FolderBrowserDialog; ` +
			`$d.Description = 'Select the folder to back up'; ` +
			`if ($d.ShowDialog() -eq 'OK') { Write-Output $d.SelectedPath }`
		out, err := exec.Command(
			"powershell", "-NoProfile", "-NonInteractive", "-Command", script,
		).Output()
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(out)), nil
	default:
		log.Println("tray: folder picker only supported on Windows")
		return "", nil
	}
}

func openSetup() {
	bin, err := siblingBinary("cerclbackup")
	if err != nil {
		return
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd.exe", "/C", "start", "cmd.exe", "/K", bin, "init")
	case "darwin":
		script := `tell application "Terminal" to do script "` + bin + ` init"`
		cmd = exec.Command("osascript", "-e", script)
	default:
		cmd = exec.Command("x-terminal-emulator", "-e", bin, "init")
	}
	cmd.Start()
}

func openLogs() {
	dir, err := cerclConfigDir()
	if err != nil {
		return
	}
	logDir := filepath.Join(dir, "logs")
	os.MkdirAll(logDir, 0o700)
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", logDir)
	case "darwin":
		cmd = exec.Command("open", logDir)
	default:
		cmd = exec.Command("xdg-open", logDir)
	}
	cmd.Start()
}

// ── utilities ─────────────────────────────────────────────────────────────────

func cerclConfigDir() (string, error) {
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cfgDir, "cerclbackup"), nil
}

func siblingBinary(name string) (string, error) {
	self, err := os.Executable()
	if err == nil {
		sibling := filepath.Join(filepath.Dir(self), name)
		if runtime.GOOS == "windows" {
			sibling += ".exe"
		}
		if _, err := os.Stat(sibling); err == nil {
			return sibling, nil
		}
	}
	return exec.LookPath(name)
}
