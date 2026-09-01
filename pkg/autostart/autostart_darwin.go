//go:build darwin

package autostart

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// label is the reverse-DNS identifier launchd uses, and also the plist name.
const label = "com.wasabules.snmplens"

var errUnsupported = errors.New("autostart: not supported on this platform")

func supported() bool { return true }

func plistPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Library", "LaunchAgents", label+".plist")
}

func location() string {
	if p := plistPath(); p != "" {
		return p
	}
	return "~/Library/LaunchAgents/" + label + ".plist"
}

func executable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate the executable: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	// Inside a .app bundle the binary sits in Contents/MacOS. launchd is
	// perfectly happy to run it directly, and doing so avoids depending on
	// `open`, which detaches and loses the exit status.
	return exe, nil
}

// escapeXML keeps a path containing & or < from producing a plist that launchd
// silently refuses to load.
func escapeXML(s string) string {
	return strings.NewReplacer(
		"&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;",
	).Replace(s)
}

func isEnabled() (bool, string, error) {
	path := plistPath()
	if path == "" {
		return false, "", fmt.Errorf("cannot determine the home directory")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, "", nil
		}
		return false, "", fmt.Errorf("read %s: %w", path, err)
	}
	// Report the registered command rather than the whole plist, so the
	// settings screen can show something a person can read.
	body := string(raw)
	start := strings.Index(body, "<string>")
	end := strings.Index(body, "</string>")
	cmd := ""
	if start >= 0 && end > start {
		cmd = body[start+len("<string>") : end]
	}
	return true, cmd, nil
}

func enable() error {
	path := plistPath()
	if path == "" {
		return fmt.Errorf("cannot determine the home directory")
	}
	exe, err := executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}

	// RunAtLoad only; deliberately NOT KeepAlive. KeepAlive would relaunch the
	// app every time the user quits it, which is indistinguishable from
	// malware from the user's point of view.
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
</dict>
</plist>
`, escapeXML(label), escapeXML(exe))

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(plist), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	return os.Rename(tmp, path)
}

func disable() error {
	path := plistPath()
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}
