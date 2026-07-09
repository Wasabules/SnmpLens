//go:build windows

package updater

import (
	"os"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const (
	windowsSetupAsset  = "SnmpLens-windows-amd64-setup.exe"
	windowsBinaryAsset = "SnmpLens-windows-amd64.exe"
)

// target picks the asset and apply strategy for Windows: the NSIS installer for
// installed copies, an in-place self-replace for the portable executable.
func target() (string, applyMode) {
	if isInstalled() {
		return windowsSetupAsset, applyInstaller
	}
	return windowsBinaryAsset, applyReplace
}

// isInstalled reports whether this copy was installed by the NSIS installer,
// which writes an uninstall registry key (per-user or per-machine).
func isInstalled() bool {
	const uninstallKey = `Software\Microsoft\Windows\CurrentVersion\Uninstall\SnmpLens`
	for _, root := range []registry.Key{registry.LOCAL_MACHINE, registry.CURRENT_USER} {
		if k, err := registry.OpenKey(root, uninstallKey, registry.QUERY_VALUE); err == nil {
			k.Close()
			return true
		}
	}
	// Fallback heuristic: running from under %ProgramFiles%.
	if exe, err := os.Executable(); err == nil {
		if pf := os.Getenv("ProgramFiles"); pf != "" &&
			strings.HasPrefix(strings.ToLower(exe), strings.ToLower(pf)) {
			return true
		}
	}
	return false
}
