package autostart

import (
	"os"
	"strings"
	"testing"
)

// restoreOriginal puts the machine back exactly as it was found. These tests
// touch the REAL login-items mechanism — the developer's own registry key or
// autostart directory — so leaving a stray entry behind would be a genuine
// nuisance rather than a test artefact.
func restoreOriginal(t *testing.T) {
	t.Helper()
	before := Get()
	t.Cleanup(func() {
		after := Get()
		if before.Enabled == after.Enabled {
			return
		}
		if _, err := Set(before.Enabled); err != nil {
			t.Errorf("could not restore the original autostart state (was enabled=%v): %v", before.Enabled, err)
		}
	})
}

func TestGetIsAlwaysAnswerable(t *testing.T) {
	st := Get()
	if !st.Supported {
		t.Skipf("autostart is not implemented on this platform")
	}
	if st.Location == "" {
		t.Error("Location must always be filled in so a user can find the entry themselves")
	}
	if st.Error != "" {
		t.Errorf("reading the state failed: %s", st.Error)
	}
}

// The round trip against the real OS mechanism. This is the test that would
// have caught a wrong registry path or a plist launchd refuses to load.
func TestEnableThenDisableRoundTrip(t *testing.T) {
	if !Get().Supported {
		t.Skip("autostart is not implemented on this platform")
	}
	restoreOriginal(t)

	st, err := Set(true)
	if err != nil {
		t.Fatalf("Set(true): %v", err)
	}
	if !st.Enabled {
		t.Fatalf("after enabling, the OS still reports disabled: %+v", st)
	}
	if st.Command == "" {
		t.Error("no command was registered; the entry would do nothing at login")
	}

	// The registered command must point at THIS binary, or login would start
	// something else entirely.
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	base := exe
	if i := strings.LastIndexAny(base, `/\`); i >= 0 {
		base = base[i+1:]
	}
	if !strings.Contains(st.Command, base) {
		t.Errorf("registered command %q does not reference this executable (%s)", st.Command, base)
	}

	st, err = Set(false)
	if err != nil {
		t.Fatalf("Set(false): %v", err)
	}
	if st.Enabled {
		t.Errorf("after disabling, the OS still reports enabled: %+v", st)
	}
}

// Disabling something that was never enabled must succeed quietly: the desired
// state is already the actual state, and an error here would make the settings
// toggle fail on a fresh install.
func TestDisablingWhenAbsentIsNotAnError(t *testing.T) {
	if !Get().Supported {
		t.Skip("autostart is not implemented on this platform")
	}
	restoreOriginal(t)

	if _, err := Set(false); err != nil {
		t.Fatalf("first Set(false): %v", err)
	}
	if _, err := Set(false); err != nil {
		t.Errorf("second Set(false) should be a no-op, got: %v", err)
	}
}

// Enabling twice must not create a second entry or fail.
func TestEnablingTwiceIsIdempotent(t *testing.T) {
	if !Get().Supported {
		t.Skip("autostart is not implemented on this platform")
	}
	restoreOriginal(t)

	first, err := Set(true)
	if err != nil {
		t.Fatalf("first Set(true): %v", err)
	}
	second, err := Set(true)
	if err != nil {
		t.Fatalf("second Set(true): %v", err)
	}
	if first.Command != second.Command {
		t.Errorf("the registered command changed between calls: %q then %q", first.Command, second.Command)
	}
	if !second.Enabled {
		t.Error("still reported as disabled after enabling twice")
	}
}

// The identity is written into the registry, a plist filename and a .desktop
// filename. Changing it would orphan every entry already registered and
// silently leave two of them behind.
func TestAppNameIsStable(t *testing.T) {
	if AppName != "SnmpLens" {
		t.Errorf("AppName = %q; changing it orphans existing login entries", AppName)
	}
}
