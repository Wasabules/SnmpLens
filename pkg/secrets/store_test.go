package secrets

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newStore(t *testing.T) (Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s, dir
}

func TestRoundTrip(t *testing.T) {
	s, _ := newStore(t)

	if err := s.Set(SinkRef("abc"), "hunter2"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := s.Get(SinkRef("abc"))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "hunter2" {
		t.Errorf("got %q, want hunter2", got)
	}
}

func TestMissingSecretIsDistinguishable(t *testing.T) {
	s, _ := newStore(t)
	_, err := s.Get(SinkRef("nope"))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// Clearing a password in the UI must remove it, not store an empty string that
// would later be sent as a credential.
func TestEmptySecretDeletes(t *testing.T) {
	s, _ := newStore(t)
	s.Set(SinkRef("abc"), "hunter2")
	if err := s.Set(SinkRef("abc"), ""); err != nil {
		t.Fatalf("Set empty: %v", err)
	}
	if _, err := s.Get(SinkRef("abc")); !errors.Is(err, ErrNotFound) {
		t.Errorf("secret survived being cleared: %v", err)
	}
}

func TestOversizedSecretRejected(t *testing.T) {
	s, _ := newStore(t)
	err := s.Set(SinkRef("abc"), strings.Repeat("x", MaxSecretLen+1))
	if !errors.Is(err, ErrTooLong) {
		t.Fatalf("expected ErrTooLong, got %v", err)
	}
}

// The whole point: the secret must not be readable from the file on disk.
func TestSecretIsNotStoredInClear(t *testing.T) {
	s, dir := newStore(t)
	const secret = "s3cr3t-passphrase-not-in-clear"
	if err := s.Set(SinkRef("abc"), secret); err != nil {
		t.Fatalf("Set: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		found = true
		if strings.Contains(string(raw), secret) {
			t.Fatalf("%s contains the secret in clear", e.Name())
		}
	}
	if !found {
		t.Fatal("no file was written; the test proved nothing")
	}
}

// A second Open on the same directory must find the same key, or every restart
// would look like "my credentials vanished".
func TestSecretsSurviveReopen(t *testing.T) {
	dir := t.TempDir()

	first, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := first.Set(SinkRef("abc"), "keepme"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	second, err := Open(dir)
	if err != nil {
		t.Fatalf("re-Open: %v", err)
	}
	got, err := second.Get(SinkRef("abc"))
	if err != nil {
		t.Fatalf("Get after reopen: %v", err)
	}
	if got != "keepme" {
		t.Errorf("got %q after reopen, want keepme", got)
	}
}

// A tampered or foreign store must fail loudly rather than silently look empty,
// which would read as "my credentials vanished" while they are merely
// unreadable.
func TestCorruptStoreIsReported(t *testing.T) {
	s, dir := newStore(t)
	s.Set(SinkRef("abc"), "hunter2")

	path := filepath.Join(dir, "sinks.secrets")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)-1] ^= 0xFF
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Get(SinkRef("abc")); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("a corrupt store must be reported, got %v", err)
	}
}

func TestBackendIsNamed(t *testing.T) {
	s, _ := newStore(t)
	switch s.Backend() {
	case "windows-dpapi", "macos-keychain", "encrypted-file":
	default:
		t.Errorf("unexpected backend name %q", s.Backend())
	}
}

func TestSinkRefShape(t *testing.T) {
	if got := SinkRef("uuid-1"); got != "sink/uuid-1/secret" {
		t.Errorf("SinkRef = %q", got)
	}
}
