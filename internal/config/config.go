// Package config loads the optional user configuration file.
// Location: ~/.config/cerclbackup/config.yaml (Linux) or %APPDATA%\CerclBackup\config.yaml (Windows).
// CLI flags always override config values.
package config

import (
	"os"
	"path/filepath"

	yaml "go.yaml.in/yaml/v2"
)

// Config holds per-user defaults for all commands.
type Config struct {
	Password   string `yaml:"password"`
	Src        string `yaml:"src"`
	Exclude    string `yaml:"exclude"`
	UploadKbps int    `yaml:"upload_kbps"`
	HealthAddr string `yaml:"health_addr"`
	Port       int    `yaml:"port"`
	Debounce   string `yaml:"debounce"`
	AutoPrune  bool   `yaml:"auto_prune"`
	StoreDir   string `yaml:"store_dir"`
}

// DefaultPath returns the platform config file path.
func DefaultPath() string {
	if appdata := os.Getenv("APPDATA"); appdata != "" {
		return filepath.Join(appdata, "CerclBackup", "config.yaml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "cerclbackup", "config.yaml")
}

// Load reads the config from DefaultPath; missing file is not an error.
func Load() Config {
	return LoadFrom(DefaultPath())
}

// LoadFrom reads the config from path; missing file returns defaults.
func LoadFrom(path string) Config {
	cfg := Config{
		HealthAddr: "127.0.0.1:7743",
		Debounce:   "3s",
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg
	}
	_ = yaml.Unmarshal(data, &cfg)
	return cfg
}

// WriteTemplate writes a commented sample config to path.
func WriteTemplate(path string) error {
	const tmpl = `# CerclBackup configuration
# All settings can be overridden with command-line flags.
# CLI flags always win over values set here.

# Default keystore password.
# Prefer CERCLBACKUP_PASSWORD env var over storing it here.
# password: ""

# Default source directory for 'backup' and 'watch'.
# src: ""

# Glob patterns to exclude (comma-separated).
# exclude: ".git,node_modules,*.tmp,*.swp"

# Maximum upload bandwidth in KB/s (0 = unlimited).
# upload_kbps: 0

# HTTP health/metrics endpoint address for 'serve'.
# health_addr: "127.0.0.1:7743"

# libp2p TCP/UDP port for 'serve'.
# port: 4001

# fsnotify debounce interval for 'watch'.
# debounce: "3s"

# Apply retention policy automatically after each 'backup'.
# auto_prune: false

# Local shard store directory.
# store_dir: ""
`
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(tmpl), 0600)
}
