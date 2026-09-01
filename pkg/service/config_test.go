package service

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMissingFileIsNotAnError(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("a fresh install must not report an error: %v", err)
	}
	if cfg != Defaults() {
		t.Errorf("got %+v, want the defaults", cfg)
	}
}

// A fresh install must behave like an ordinary desktop app.
func TestDefaultsDoNotRunInBackground(t *testing.T) {
	d := Defaults()
	if d.RunInBackground || d.StartHidden || d.AutoStartTrapListener {
		t.Errorf("background behaviour must be opt-in, got %+v", d)
	}
}

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := Config{RunInBackground: true, StartHidden: true, AutoStartTrapListener: true, TrapPort: 1162, AutoResumeMonitors: true}
	if err := Save(dir, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

// A hand-edited or truncated file must still leave a launchable app.
func TestCorruptFileStillYieldsUsableDefaults(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(Path(dir), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err == nil {
		t.Error("a malformed file should be reported")
	}
	if cfg != Defaults() {
		t.Errorf("got %+v, want usable defaults despite the error", cfg)
	}
}

// StartHidden without RunInBackground would give a process with no window and
// no tray to reach it from.
func TestStartHiddenRequiresBackgroundMode(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, Config{StartHidden: true}); err != nil {
		t.Fatal(err)
	}
	cfg, _ := Load(dir)
	if cfg.StartHidden {
		t.Error("StartHidden must be cleared when RunInBackground is off")
	}
}

func TestTrapPortIsRepairedNotRejected(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, Config{TrapPort: 70000}); err != nil {
		t.Fatal(err)
	}
	cfg, _ := Load(dir)
	if cfg.TrapPort != 162 {
		t.Errorf("TrapPort = %d, want the 162 fallback", cfg.TrapPort)
	}
}

func TestSaveLeavesNoTempFileBehind(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, Defaults()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, FileName+".tmp")); err == nil {
		t.Error("the temporary file was not renamed away")
	}
}
