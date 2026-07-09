//go:build linux

package updater

import "os"

const (
	linuxDebAsset    = "SnmpLens-linux-amd64.deb"
	linuxBinaryAsset = "SnmpLens-linux-amd64"
	// debInstallPath is where the .deb package installs the binary (see the
	// release workflow). A copy running from there is system-managed.
	debInstallPath = "/usr/local/bin/snmplens"
)

// target self-replaces a portable binary, but defers .deb installs to the
// browser since replacing a system-managed file needs root.
func target() (string, applyMode) {
	if exe, err := os.Executable(); err == nil && exe == debInstallPath {
		return linuxDebAsset, applyBrowser
	}
	return linuxBinaryAsset, applyReplace
}
