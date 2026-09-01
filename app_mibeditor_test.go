package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"SnmpLens/pkg/mib"
)

func newMibApp(t *testing.T) (*App, string) {
	t.Helper()
	root := t.TempDir()
	mibDir := filepath.Join(root, "SnmpLens", "mibs")
	if err := os.MkdirAll(mibDir, 0o755); err != nil {
		t.Fatal(err)
	}
	a := &App{persistentMibDir: mibDir, mibService: mib.NewService(mibDir)}
	return a, mibDir
}

// The editor WRITES, and monitoring.db, service.json and the secret store all
// live one directory above the MIB folder. A name must never be able to reach
// them.
func TestResolveMibPathRefusesEscapes(t *testing.T) {
	a, mibDir := newMibApp(t)

	escapes := []string{
		"../monitoring.db",
		"../../monitoring.db",
		`..\..\service.json`,
		"subdir/../../secrets",
		"/etc/passwd",
		`C:\Windows\System32\hosts`,
		"",
		"   ",
		".",
	}
	for _, name := range escapes {
		path, err := a.resolveMibPath(name)
		if err != nil {
			continue // refused outright, which is fine
		}
		if filepath.Dir(path) != filepath.Clean(mibDir) {
			t.Errorf("%q resolved to %q, outside the MIB directory", name, path)
		}
	}

	// A plain name must still work.
	path, err := a.resolveMibPath("IF-MIB")
	if err != nil {
		t.Fatalf("a legitimate name was refused: %v", err)
	}
	if filepath.Dir(path) != filepath.Clean(mibDir) {
		t.Errorf("path = %q", path)
	}
}

func TestSaveAndReadRoundTrip(t *testing.T) {
	a, _ := newMibApp(t)
	const src = "TEST-MIB DEFINITIONS ::= BEGIN\nEND\n"

	res, err := a.MibEditorSave("TEST-MIB", src, "", false)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if !res.Saved {
		t.Fatal("save reported no write")
	}

	got, err := a.MibEditorRead("TEST-MIB")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.Content != src {
		t.Errorf("content = %q", got.Content)
	}
	if got.Sha256 != res.Sha256 {
		t.Error("the checksum from save does not match the one from read")
	}
}

// A backup beside the original would be loaded as a second copy of the same
// module, and os.ReadDir being alphabetical, "IF-MIB.123.bak" would win over
// "IF-MIB" — stale content, no error anywhere.
func TestBackupsLandOutsideTheMibDirectory(t *testing.T) {
	a, mibDir := newMibApp(t)

	if _, err := a.MibEditorSave("TEST-MIB", "TEST-MIB DEFINITIONS ::= BEGIN\nEND\n", "", false); err != nil {
		t.Fatal(err)
	}
	res, err := a.MibEditorSave("TEST-MIB", "TEST-MIB DEFINITIONS ::= BEGIN\n-- edited\nEND\n", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if res.BackupPath == "" {
		t.Fatal("no backup was taken before overwriting")
	}
	if strings.HasPrefix(filepath.Clean(res.BackupPath), filepath.Clean(mibDir)) {
		t.Errorf("the backup landed inside the MIB directory: %s", res.BackupPath)
	}
	if _, err := os.Stat(res.BackupPath); err != nil {
		t.Errorf("the backup does not exist: %v", err)
	}

	// And the MIB directory must still hold exactly one file.
	entries, _ := os.ReadDir(mibDir)
	if len(entries) != 1 {
		names := []string{}
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("the MIB directory holds %v; a stray file there loads as a MIB", names)
	}
}

// Saving must not silently discard an edit made underneath the editor.
func TestSaveDetectsAConflict(t *testing.T) {
	a, _ := newMibApp(t)
	const original = "TEST-MIB DEFINITIONS ::= BEGIN\nEND\n"

	first, _ := a.MibEditorSave("TEST-MIB", original, "", false)

	// Someone else changes the file.
	path, _ := a.resolveMibPath("TEST-MIB")
	os.WriteFile(path, []byte("TEST-MIB DEFINITIONS ::= BEGIN\n-- theirs\nEND\n"), 0o644)

	res, err := a.MibEditorSave("TEST-MIB", "TEST-MIB DEFINITIONS ::= BEGIN\n-- mine\nEND\n", first.Sha256, false)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if !res.Conflict {
		t.Fatal("the concurrent edit was overwritten silently")
	}
	if res.Saved {
		t.Error("the save went ahead despite the conflict")
	}

	// force must get through.
	res, err = a.MibEditorSave("TEST-MIB", "TEST-MIB DEFINITIONS ::= BEGIN\n-- mine\nEND\n", first.Sha256, true)
	if err != nil || !res.Saved {
		t.Errorf("force did not override the conflict: %+v %v", res, err)
	}
}

// A CRLF file must stay CRLF: rewriting every line because one word changed
// turns a one-line diff into a whole-file diff.
func TestSavePreservesLineEndings(t *testing.T) {
	a, _ := newMibApp(t)
	path, _ := a.resolveMibPath("CRLF-MIB")
	os.WriteFile(path, []byte("CRLF-MIB DEFINITIONS ::= BEGIN\r\nEND\r\n"), 0o644)

	src, err := a.MibEditorRead("CRLF-MIB")
	if err != nil {
		t.Fatal(err)
	}
	if src.Eol != "crlf" {
		t.Fatalf("eol = %q", src.Eol)
	}
	if strings.Contains(src.Content, "\r") {
		t.Error("CR reached the editor buffer; it would double up on save")
	}

	if _, err := a.MibEditorSave("CRLF-MIB", src.Content+"-- added\n", src.Sha256, false); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "\r\n") {
		t.Error("the file was rewritten with LF endings")
	}
	if strings.Contains(string(raw), "\r\r") {
		t.Error("the line endings were doubled")
	}
}

// Diagnostics must reach the frontend with the file, or opening a broken MIB
// says nothing about why it is broken.
func TestReadCarriesDiagnostics(t *testing.T) {
	a, _ := newMibApp(t)
	path, _ := a.resolveMibPath("BAD-MIB")
	os.WriteFile(path, []byte("BAD-MIB DEFINITIONS ::= BEGIN\nnot valid at all\nEND\n"), 0o644)

	src, err := a.MibEditorRead("BAD-MIB")
	if err != nil {
		t.Fatal(err)
	}
	if len(src.Diagnostics) == 0 {
		t.Fatal("a broken MIB opened with no diagnostics")
	}
	located := false
	for _, d := range src.Diagnostics {
		if d.Line > 0 {
			located = true
		}
	}
	if !located {
		t.Errorf("no diagnostic carries a line: %+v", src.Diagnostics)
	}
}

func TestListMarksBundledAndModified(t *testing.T) {
	a, _ := newMibApp(t)
	// No embed.FS in the test App, so nothing can be bundled — the point here
	// is that a plain vendor MIB is not falsely flagged.
	path, _ := a.resolveMibPath("VENDOR-MIB")
	os.WriteFile(path, []byte("VENDOR-MIB DEFINITIONS ::= BEGIN\nEND\n"), 0o644)

	list, err := a.MibEditorList()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("got %d files", len(list))
	}
	if list[0].Bundled || list[0].Modified {
		t.Errorf("a vendor MIB was flagged as bundled: %+v", list[0])
	}
	if list[0].Size == 0 {
		t.Error("size was not filled in")
	}
}

// Restoring something that never shipped must say so rather than writing an
// empty file over the user's work.
func TestRestoreBundledRefusesAVendorMib(t *testing.T) {
	a, _ := newMibApp(t)
	path, _ := a.resolveMibPath("VENDOR-MIB")
	os.WriteFile(path, []byte("VENDOR-MIB DEFINITIONS ::= BEGIN\nEND\n"), 0o644)

	if _, err := a.MibEditorRestoreBundled("VENDOR-MIB"); err == nil {
		t.Fatal("restoring a non-bundled MIB was allowed")
	}
	raw, _ := os.ReadFile(path)
	if len(raw) == 0 {
		t.Error("the file was emptied by a failed restore")
	}
}

// A draft must survive leaving the tab, and must NOT be written into the MIB
// directory: a half-finished MIB there would be loaded by the app.
func TestDraftsLandOutsideTheMibDirectory(t *testing.T) {
	a, mibDir := newMibApp(t)

	if err := a.MibEditorSaveDraft("WIP-MIB", "WIP-MIB DEFINITIONS ::= BEGIN\n"); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	entries, _ := os.ReadDir(mibDir)
	if len(entries) != 0 {
		names := []string{}
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("the draft landed in the MIB directory: %v", names)
	}

	got, err := a.MibEditorReadDraft("WIP-MIB")
	if err != nil {
		t.Fatalf("ReadDraft: %v", err)
	}
	if got != "WIP-MIB DEFINITIONS ::= BEGIN\n" {
		t.Errorf("draft = %q", got)
	}

	list, err := a.MibEditorListDrafts()
	if err != nil || len(list) != 1 || list[0].Name != "WIP-MIB" {
		t.Errorf("ListDrafts = %+v (%v)", list, err)
	}

	if err := a.MibEditorDiscardDraft("WIP-MIB"); err != nil {
		t.Fatalf("DiscardDraft: %v", err)
	}
	if got, _ := a.MibEditorReadDraft("WIP-MIB"); got != "" {
		t.Error("the draft survived being discarded")
	}
}

// Reading a draft that was never written is a normal state, not an error:
// every open would otherwise have to handle a failure that means "nothing to
// recover".
func TestReadingAMissingDraftIsNotAnError(t *testing.T) {
	a, _ := newMibApp(t)
	got, err := a.MibEditorReadDraft("NEVER-SAVED")
	if err != nil {
		t.Errorf("err = %v", err)
	}
	if got != "" {
		t.Errorf("content = %q", got)
	}
}

func TestDraftNamesAreContained(t *testing.T) {
	a, _ := newMibApp(t)
	for _, name := range []string{"../escape", "../../monitoring.db", "/etc/passwd"} {
		path, err := a.draftPath(name)
		if err != nil {
			continue
		}
		if strings.Contains(filepath.ToSlash(path), "/mibs/") {
			t.Errorf("%q resolved into the MIB directory: %s", name, path)
		}
		if !strings.Contains(filepath.ToSlash(path), mibDraftDirName) {
			t.Errorf("%q escaped the draft directory: %s", name, path)
		}
	}
}
