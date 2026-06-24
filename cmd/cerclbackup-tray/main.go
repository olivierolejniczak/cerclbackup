// cerclbackup-tray is the system-tray companion for CerclBackup.
// It shows last-backup status, lets the user trigger an immediate backup,
// and opens the log directory on demand.
// Build for Windows: go build -ldflags "-H=windowsgui" ./cmd/cerclbackup-tray
package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"fyne.io/systray"

	traystatus "github.com/cerclbackup/cerclbackup/internal/tray"
)

func main() {
	systray.Run(onReady, onExit)
}

func onReady() {
	systray.SetIcon(iconPNG())
	systray.SetTitle("CerclBackup")
	systray.SetTooltip("CerclBackup - P2P encrypted backup")

	mStatus := systray.AddMenuItem("Status: checking...", "Last backup status")
	mStatus.Disable()
	systray.AddSeparator()
	mBackup := systray.AddMenuItem("Backup Now", "Run backup immediately")
	mLogs := systray.AddMenuItem("Open Logs", "Open log directory")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit", "Stop CerclBackup tray")

	go func() {
		for {
			select {
			case <-mBackup.ClickedCh:
				go runBackup(mStatus)
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

func onExit() {}

// pollStatus refreshes the tray status label every 30 seconds.
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
	label := formatAge(age)
	if s.Error != "" {
		item.SetTitle(fmt.Sprintf("Status: error %s ago", label))
	} else {
		item.SetTitle(fmt.Sprintf("Status: backed up %s ago", label))
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

// runBackup execs the sibling cerclbackup binary.  Password must be supplied
// via CERCLBACKUP_PASSWORD env var when using the tray (no interactive prompt).
func runBackup(mStatus *systray.MenuItem) {
	mStatus.SetTitle("Status: backup running...")
	exe, err := siblingBinary("cerclbackup")
	if err != nil {
		log.Printf("tray: locate cerclbackup: %v", err)
		mStatus.SetTitle("Status: backup binary not found")
		return
	}
	password := os.Getenv("CERCLBACKUP_PASSWORD")
	if password == "" {
		mStatus.SetTitle("Status: set CERCLBACKUP_PASSWORD to enable tray backup")
		return
	}
	// The tray always uses the watch-mode default path; if --src is required
	// the user should configure CERCLBACKUP_SRC.
	src := os.Getenv("CERCLBACKUP_SRC")
	if src == "" {
		mStatus.SetTitle("Status: set CERCLBACKUP_SRC to enable tray backup")
		return
	}
	cmd := exec.Command(exe, "backup", "--src", src, "--password", password)
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Printf("tray: backup: %v\n%s", err, out)
		// Update status file with error so next poll shows it.
		if dir, e := cerclConfigDir(); e == nil {
			traystatus.Write(dir, traystatus.Status{
				LastBackupAt: time.Now().UTC(),
				LastFile:     src,
				Error:        err.Error(),
			})
		}
		mStatus.SetTitle("Status: backup failed")
		return
	}
	updateStatus(mStatus)
}

// openLogs opens the cerclbackup log directory in the OS file browser.
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

func cerclConfigDir() (string, error) {
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cfgDir, "cerclbackup"), nil
}

// siblingBinary looks for a binary next to the running tray process, then falls
// back to PATH.
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
