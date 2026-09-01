//go:build darwin

package secrets

import (
	"encoding/base64"
	"fmt"
	"os/exec"
	"strings"
)

// On macOS the data-encryption key lives in the login Keychain, reached through
// the bundled `security` tool. Using the CLI rather than the Security framework
// keeps the build CGO-free, which matters because the release workflow builds
// darwin/universal.
type keychainProtector struct{ service, account string }

func newProtector(dir string) keyProtector {
	_ = dir
	return &keychainProtector{service: "SnmpLens", account: "sink-secrets"}
}

func (p *keychainProtector) name() string { return "macos-keychain" }

func (p *keychainProtector) loadKey() ([]byte, error) {
	out, err := exec.Command("security", "find-generic-password",
		"-s", p.service, "-a", p.account, "-w").Output()
	if err != nil {
		return nil, fmt.Errorf("keychain lookup: %w", err)
	}
	return base64.StdEncoding.DecodeString(strings.TrimSpace(string(out)))
}

func (p *keychainProtector) saveKey(key []byte) error {
	encoded := base64.StdEncoding.EncodeToString(key)
	// -U updates an existing item instead of failing. The secret is passed with
	// -w on stdin-free form; `security` is exec'd directly, never through a
	// shell, so it never reaches a shell history.
	cmd := exec.Command("security", "add-generic-password",
		"-s", p.service, "-a", p.account, "-w", encoded, "-U")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("keychain store: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}
