package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"SnmpLens/pkg/updater"
)

// A check that could not be answered must not read as "you are up to date".
//
// App.CheckForUpdate returns no error across the bridge. It logged the failure
// and returned the zero value, so Available was false and the renderer said
// "You're on the latest version" — reassurance produced by a failure. Every
// reason a check fails (no network, GitHub rate-limiting, a proxy, a refused
// response) looked identical to good news, in a product whose updates carry
// security fixes.
func TestAFailedUpdateCheckSaysSo(t *testing.T) {
	got := withCheckError(updater.UpdateInfo{CurrentVersion: "v1.5.0"},
		errors.New("GitHub API returned 403 Forbidden: rate limit exceeded"))

	if got.Error == "" {
		t.Fatal("a failed check reported nothing; the renderer shows that as " +
			`"You're on the latest version"`)
	}
	if !strings.Contains(got.Error, "403") {
		t.Errorf("the reason was lost: %q", got.Error)
	}
	if got.Available {
		t.Error("a failed check claimed an update is available")
	}
	// What is known is still returned: the current version is read locally and
	// is not in doubt because GitHub was unreachable.
	if got.CurrentVersion != "v1.5.0" {
		t.Errorf("CurrentVersion = %q", got.CurrentVersion)
	}
}

// The control: a successful check reports no error, or every check would turn
// into a warning.
func TestASuccessfulUpdateCheckReportsNoError(t *testing.T) {
	got := withCheckError(updater.UpdateInfo{
		CurrentVersion: "v1.5.0", LatestVersion: "v1.5.0",
	}, nil)
	if got.Error != "" {
		t.Errorf("a successful check reported an error: %q", got.Error)
	}
}

// The renderer has to READ the field, and read it BEFORE concluding "up to
// date" — a failure looks exactly like "not available". Checked over the
// source, because the alternative is a browser.
func TestTheRendererDistinguishesAFailedCheck(t *testing.T) {
	for _, f := range []string{
		filepath.Join("frontend", "src", "settings", "GeneralSettings.svelte"),
		filepath.Join("frontend", "src", "stores", "updateStore.js"),
	} {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if !strings.Contains(string(src), "info.error") {
			t.Errorf("%s does not read the reason a check failed", f)
		}
	}

	body, err := os.ReadFile(filepath.Join("frontend", "src", "settings", "GeneralSettings.svelte"))
	if err != nil {
		t.Fatal(err)
	}
	errAt := strings.Index(string(body), "info.error")
	upAt := strings.Index(string(body), "update.upToDate")
	if errAt < 0 || upAt < 0 || errAt > upAt {
		t.Errorf("the up-to-date message is not guarded by the error check (error@%d upToDate@%d)",
			errAt, upAt)
	}
}
