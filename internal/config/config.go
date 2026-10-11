// Package config holds non-secret settings (ports, intervals, paths).
// Secrets (AES key, device_key) never live here; see internal/secrets.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const (
	DirName  = ".slack-relay"
	DirPerm  = 0o700
	FilePerm = 0o600

	DefaultSlackBundleID = "com.tinyspeck.slackmacgap"
)

// Config is the content of ~/.slack-relay/config.json. Every field is
// optional; zero values fall back to defaults.
type Config struct {
	// Port bark-server listens on (always 127.0.0.1 in normal operation).
	Port int `json:"port"`
	// PollIntervalSeconds is the notification DB polling interval.
	PollIntervalSeconds int `json:"poll_interval_seconds"`
	// NotificationDBPath overrides auto-detection of the notification DB.
	NotificationDBPath string `json:"notification_db_path"`
	// BarkServerPath overrides the bark-server binary location.
	BarkServerPath string `json:"bark_server_path"`
	// SlackBundleIDs lists the app identifiers treated as Slack.
	SlackBundleIDs []string `json:"slack_bundle_ids"`
	// RegisterTimeoutSeconds bounds how long `register` exposes bark-server on the LAN.
	RegisterTimeoutSeconds int `json:"register_timeout_seconds"`
	// Debug enables debug-level logs. Redaction still applies.
	Debug bool `json:"debug"`
}

// Defaults returns the built-in configuration.
func Defaults() Config {
	return Config{
		Port:                   18787,
		PollIntervalSeconds:    4,
		SlackBundleIDs:         []string{DefaultSlackBundleID},
		RegisterTimeoutSeconds: 300,
	}
}

// Validate checks value ranges.
func (c Config) Validate() error {
	if c.Port < 1024 || c.Port > 65535 {
		return fmt.Errorf("config: port must be 1024-65535")
	}
	if c.PollIntervalSeconds < 1 || c.PollIntervalSeconds > 60 {
		return fmt.Errorf("config: poll_interval_seconds must be 1-60")
	}
	if c.RegisterTimeoutSeconds < 30 || c.RegisterTimeoutSeconds > 600 {
		return fmt.Errorf("config: register_timeout_seconds must be 30-600")
	}
	if len(c.SlackBundleIDs) == 0 {
		return fmt.Errorf("config: slack_bundle_ids must not be empty")
	}
	return nil
}

func (c Config) PollInterval() time.Duration {
	return time.Duration(c.PollIntervalSeconds) * time.Second
}

func (c Config) RegisterTimeout() time.Duration {
	return time.Duration(c.RegisterTimeoutSeconds) * time.Second
}

// LoopbackAddr is the address bark-server listens on in normal operation.
func (c Config) LoopbackAddr() string {
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(c.Port))
}

// Paths are the runtime file locations under the data directory.
type Paths struct {
	Dir         string
	ConfigFile  string
	LogFile     string
	BarkDataDir string
	BarkServer  string
	RunPID      string
	BarkPID     string
	CaffeinePID string
	RelayBinary string
}

// DefaultDir returns ~/.slack-relay.
func DefaultDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, DirName), nil
}

// PathsFor returns the runtime paths for a data directory.
func PathsFor(dir string, c Config) Paths {
	p := Paths{
		Dir:         dir,
		ConfigFile:  filepath.Join(dir, "config.json"),
		LogFile:     filepath.Join(dir, "relay.log"),
		BarkDataDir: filepath.Join(dir, "bark-data"),
		BarkServer:  filepath.Join(dir, "bin", "bark-server"),
		RunPID:      filepath.Join(dir, "run.pid"),
		BarkPID:     filepath.Join(dir, "bark-server.pid"),
		CaffeinePID: filepath.Join(dir, "caffeinate.pid"),
		RelayBinary: filepath.Join(dir, "bin", "slack-mention-relay"),
	}
	if c.BarkServerPath != "" {
		p.BarkServer = c.BarkServerPath
	}
	return p
}

// Load reads path on top of Defaults(). A missing file is not an error.
func Load(path string) (Config, error) {
	c := Defaults()
	b, err := os.ReadFile(path) // #nosec G304 -- path is the user's own config file
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	var f Config
	if err := json.Unmarshal(b, &f); err != nil {
		return c, fmt.Errorf("config: parse %s: %w", filepath.Base(path), err)
	}
	if f.Port != 0 {
		c.Port = f.Port
	}
	if f.PollIntervalSeconds != 0 {
		c.PollIntervalSeconds = f.PollIntervalSeconds
	}
	if f.NotificationDBPath != "" {
		c.NotificationDBPath = f.NotificationDBPath
	}
	if f.BarkServerPath != "" {
		c.BarkServerPath = f.BarkServerPath
	}
	if len(f.SlackBundleIDs) != 0 {
		c.SlackBundleIDs = f.SlackBundleIDs
	}
	if f.RegisterTimeoutSeconds != 0 {
		c.RegisterTimeoutSeconds = f.RegisterTimeoutSeconds
	}
	c.Debug = f.Debug
	return c, c.Validate()
}

// EnsureDir creates dir (and the bark data dir) with 0700 if missing.
func EnsureDir(p Paths) error {
	for _, d := range []string{p.Dir, p.BarkDataDir} {
		if err := os.MkdirAll(d, DirPerm); err != nil {
			return err
		}
	}
	return nil
}

// CheckPerms returns warnings for entries under dir that are more permissive
// than 0700 (directories) / 0600 (files). Executables in bin/ may be 0700.
func CheckPerms(dir string) []string {
	var warns []string
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		mode := info.Mode().Perm()
		rel, _ := filepath.Rel(dir, path)
		switch {
		case d.IsDir() && mode&0o077 != 0:
			warns = append(warns, fmt.Sprintf("%s: directory mode %04o (want 0700)", rel, mode))
		case !d.IsDir() && mode&0o077 != 0:
			warns = append(warns, fmt.Sprintf("%s: file mode %04o (want 0600, or 0700 for executables)", rel, mode))
		}
		return nil
	})
	return warns
}

// NotificationDBCandidates returns the known notification DB locations in
// priority order. darwinUserDir is `getconf DARWIN_USER_DIR` (may be empty).
func NotificationDBCandidates(home, darwinUserDir string) []string {
	c := []string{filepath.Join(home, "Library", "Group Containers", "group.com.apple.usernoted", "db2", "db")}
	if darwinUserDir != "" {
		c = append(c, filepath.Join(darwinUserDir, "com.apple.notificationcenter", "db2", "db"))
	}
	return c
}
