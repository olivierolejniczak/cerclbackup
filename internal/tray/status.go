// Package tray provides status file I/O shared between the CLI and the tray app.
// After each backup the CLI writes ~/.config/cerclbackup/status.json;
// the tray process reads it to display last-backup time and error state.
package tray

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const statusFile = "status.json"

// Status is the last-known backup state persisted to disk.
type Status struct {
	LastBackupAt time.Time `json:"last_backup_at"`
	LastFile     string    `json:"last_file"`
	Error        string    `json:"error,omitempty"`
}

// Write atomically saves s to <dir>/status.json.
func Write(dir string, s Status) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp := filepath.Join(dir, statusFile+".tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, statusFile))
}

// Read loads the status file from dir; returns a zero Status if not found.
func Read(dir string) (Status, error) {
	var s Status
	b, err := os.ReadFile(filepath.Join(dir, statusFile))
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	return s, json.Unmarshal(b, &s)
}
