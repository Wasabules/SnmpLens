// Package service holds the handful of preferences that must be known BEFORE
// the GUI exists.
//
// Everything else the user configures lives in the renderer's localStorage,
// which is only readable once a webview is running. That is too late for
// StartHidden, HideWindowOnClose and the single-instance lock: Wails consumes
// those when the window is created, and for a headless launch there may never
// be a window at all. So these few settings get their own small JSON file that
// plain Go can read at process start.
package service

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// FileName is the on-disk name inside the SnmpLens config directory.
const FileName = "service.json"

// Config is the pre-GUI preference set.
type Config struct {
	// RunInBackground keeps the process alive when the window is closed.
	// It is only honoured when a tray icon actually appeared — see
	// tray.Start — because an app you cannot quit is worse than one that
	// does not stay resident.
	RunInBackground bool `json:"runInBackground"`
	// StartHidden launches straight to the tray, for a login item.
	StartHidden bool `json:"startHidden"`
	// AutoStartTrapListener binds the trap port at startup rather than
	// waiting for someone to open the Traps tab.
	AutoStartTrapListener bool `json:"autoStartTrapListener"`
	// TrapPort is the port to bind when AutoStartTrapListener is set.
	// 0 means the usual 162.
	TrapPort int `json:"trapPort"`
	// AutoResumeMonitors restarts the sessions that were running at exit.
	AutoResumeMonitors bool `json:"autoResumeMonitors"`
	// AuditFailedSets journals refused SNMP SETs. Off by default: a SET is
	// rare in normal use, and an audit trail nobody asked for is just noise.
	AuditFailedSets bool `json:"auditFailedSets"`
}

// Defaults is the configuration of a fresh install: an ordinary desktop app
// that quits when you close it. Background operation is strictly opt-in.
func Defaults() Config {
	return Config{TrapPort: 162}
}

// Path returns the config file location inside the SnmpLens directory.
func Path(dir string) string { return filepath.Join(dir, FileName) }

// Load reads the configuration, falling back to Defaults.
//
// It never reports "file missing" as an error — that is simply a fresh
// install. A malformed file IS reported, but Load still returns usable
// defaults so a bad edit cannot make the app unlaunchable.
func Load(dir string) (Config, error) {
	raw, err := os.ReadFile(Path(dir))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Defaults(), nil
		}
		return Defaults(), err
	}

	cfg := Defaults()
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Defaults(), err
	}
	return cfg.normalised(), nil
}

// Save writes the configuration, creating the directory if needed.
func Save(dir string, cfg Config) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(cfg.normalised(), "", "  ")
	if err != nil {
		return err
	}
	// Write-then-rename: a crash mid-write must not leave a truncated file
	// that the next launch refuses to parse.
	tmp := Path(dir) + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, Path(dir))
}

// normalised repairs values that would misbehave rather than rejecting them.
func (c Config) normalised() Config {
	if c.TrapPort <= 0 || c.TrapPort > 65535 {
		c.TrapPort = 162
	}
	// Starting hidden without background mode would produce a process with no
	// window and no way to reach it.
	if !c.RunInBackground {
		c.StartHidden = false
	}
	return c
}
