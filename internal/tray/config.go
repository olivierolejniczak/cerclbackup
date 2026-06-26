package tray

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const configFile = "tray-config.json"

// TrayConfig holds persistent tray-app settings (not secrets).
type TrayConfig struct {
	WatchSrc string `json:"watch_src,omitempty"`
}

// ReadConfig loads the tray config from dir; returns zero value if not found.
func ReadConfig(dir string) (TrayConfig, error) {
	var cfg TrayConfig
	b, err := os.ReadFile(filepath.Join(dir, configFile))
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	return cfg, json.Unmarshal(b, &cfg)
}

// WriteConfig atomically saves cfg to dir/tray-config.json.
func WriteConfig(dir string, cfg TrayConfig) error {
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp := filepath.Join(dir, configFile+".tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, configFile))
}
