//go:build darwin

package updater

// macAsset is offered via the browser: self-replacing an unsigned, un-notarized
// .app bundle triggers Gatekeeper quarantine, so we let the user install the
// signed disk image manually.
const macAsset = "SnmpLens-macos-universal.dmg"

func target() (string, applyMode) {
	return macAsset, applyBrowser
}
