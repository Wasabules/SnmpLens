package updater

import (
	"strings"
	"testing"
)

// The browser path applies nothing — the operator downloads the .dmg or .deb
// over HTTPS and installs it by hand — so it is not the "install an unverified
// binary" the self-apply path would be. What it does do is send the operator to
// an address taken from the GitHub JSON, and that address was the only thing
// here nobody checked. Anyone able to change what the app reads back could
// point it at any host, and the operator would have every reason to trust the
// destination: the application sent them.
//
// The self-apply path needs none of this, because a redirected asset fails the
// checksum from the signed manifest. This one has no such backstop.
func TestOnlyAGitHubReleaseAddressIsOpened(t *testing.T) {
	ok := []string{
		"https://github.com/Wasabules/SnmpLens/releases/download/v1.5.0/SnmpLens-macos-universal.dmg",
		"https://objects.githubusercontent.com/github-production-release-asset/1/2?X-Amz-Algorithm=x",
		"https://release-assets.githubusercontent.com/github-production-release-asset/1/2",
		"https://GitHub.com/Wasabules/SnmpLens/releases/download/v1.5.0/x.deb",
	}
	for _, u := range ok {
		if err := checkReleaseHost(u); err != nil {
			t.Errorf("a real release address was refused: %s (%v)", u, err)
		}
	}

	bad := []struct {
		url  string
		says string
	}{
		{"https://github.evil.example/Wasabules/SnmpLens/releases/download/v1/x.dmg", "github.evil.example"},
		{"https://attacker.example/SnmpLens-macos-universal.dmg", "attacker.example"},
		{"http://github.com/Wasabules/SnmpLens/releases/download/v1/x.dmg", "http"},
		{"file:///tmp/evil.dmg", "file"},
		{"https://github.com.attacker.example/x.dmg", "github.com.attacker.example"},
		{"://not a url", "parse"},
	}
	for _, c := range bad {
		err := checkReleaseHost(c.url)
		if err == nil {
			t.Errorf("%s was accepted as a release address", c.url)
			continue
		}
		if !strings.Contains(err.Error(), c.says) {
			t.Errorf("%s: the refusal does not say why: %v", c.url, err)
		}
	}
}
