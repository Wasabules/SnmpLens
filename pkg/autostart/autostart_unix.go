//go:build !windows && !darwin

package autostart

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// desktopFile is the XDG autostart entry name.
const desktopFile = "snmplens.desktop"

var errUnsupported = errors.New("autostart: not supported on this platform")

func supported() bool { return true }

// autostartDir follows the XDG base directory specification: $XDG_CONFIG_HOME
// when set, ~/.config otherwise. Every mainstream desktop reads it.
func autostartDir() string {
	if dir := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); dir != "" {
		return filepath.Join(dir, "autostart")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "autostart")
}

func entryPath() string {
	dir := autostartDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, desktopFile)
}

func location() string {
	if p := entryPath(); p != "" {
		return p
	}
	return "~/.config/autostart/" + desktopFile
}

func executable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate the executable: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return exe, nil
}

func isEnabled() (bool, string, error) {
	path := entryPath()
	if path == "" {
		return false, "", fmt.Errorf("cannot determine the configuration directory")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, "", nil
		}
		return false, "", fmt.Errorf("read %s: %w", path, err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		// A desktop entry can be present but switched off by the desktop's own
		// startup-applications UI, which writes this key rather than deleting
		// the file. Reporting it as enabled would contradict what the user
		// sees in their own settings.
		if strings.EqualFold(line, "Hidden=true") {
			return false, "", nil
		}
		if after, ok := strings.CutPrefix(line, "Exec="); ok {
			return true, after, nil
		}
	}
	return true, "", nil
}

func enable() error {
	path := entryPath()
	if path == "" {
		return fmt.Errorf("cannot determine the configuration directory")
	}
	exe, err := executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}

	// Exec is not quoted the way a shell would quote it: the desktop entry
	// specification uses its own escaping, where a literal space inside an
	// argument is written as a double-quoted field.
	execLine := exe
	if strings.ContainsAny(exe, " \t") {
		execLine = `"` + strings.ReplaceAll(exe, `"`, `\"`) + `"`
	}

	entry := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=SnmpLens
Comment=SNMP MIB browser and network monitoring
Exec=%s
Terminal=false
X-GNOME-Autostart-enabled=true
`, execLine)

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(entry), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	return os.Rename(tmp, path)
}

func disable() error {
	path := entryPath()
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}
