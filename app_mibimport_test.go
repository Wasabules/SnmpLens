package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"SnmpLens/pkg/mib"
)

// A vendor MIB archive must not be able to replace a standard MIB.
//
// Vendor zips routinely ship their own SNMPv2-SMI, SNMPv2-TC and SNMPv2-CONF,
// and nearly every other MIB imports from those three. The import wrote with
// os.WriteFile unconditionally, and ensureStandardMibs restores only files
// that are ABSENT — so a replaced one is never repaired and the tree stays
// broken across restarts, with nothing saying why.
func TestImportRefusesToReplaceABundledMib(t *testing.T) {
	root := t.TempDir()
	mibDir := filepath.Join(root, "SnmpLens", "mibs")
	if err := os.MkdirAll(mibDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// mibs is the real embed.FS from main.go: the bundled list the check
	// consults is the one that actually ships.
	a := &App{persistentMibDir: mibDir, mibService: mib.NewService(mibDir), mibs: mibs}

	// The file an operator already has, and the copy the archive would put
	// over it. Different bytes, so the identical-file skip does not apply.
	good := []byte("SNMPv2-SMI DEFINITIONS ::= BEGIN\n-- the bundled copy\nEND\n")
	dst := filepath.Join(mibDir, "SNMPv2-SMI")
	if err := os.WriteFile(dst, good, 0o644); err != nil {
		t.Fatal(err)
	}

	srcDir := t.TempDir()
	hostile := filepath.Join(srcDir, "SNMPv2-SMI")
	if err := os.WriteFile(hostile, []byte("SNMPv2-SMI DEFINITIONS ::= BEGIN\n-- the vendor copy, truncated\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res := a.importSingleFile(hostile)
	if res.Success {
		t.Fatalf("the bundled SNMPv2-SMI was replaced: %+v", res)
	}
	// The refusal has to say what happened and what to do; this result goes
	// straight into the per-file list the import dialog shows.
	if !strings.Contains(res.Error, "SNMPv2-SMI") || !strings.Contains(res.Error, "MIB editor") {
		t.Errorf("the refusal does not explain itself: %q", res.Error)
	}

	// And the file on disk is untouched.
	after, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(good) {
		t.Fatalf("the file was written anyway:\n%s", after)
	}
}

// Every bundled name is covered, not only the three obvious ones. The list is
// read from the embed rather than repeated here, so a MIB added to mibs/ is
// protected the day it is added.
func TestEveryBundledNameIsRefused(t *testing.T) {
	root := t.TempDir()
	mibDir := filepath.Join(root, "SnmpLens", "mibs")
	if err := os.MkdirAll(mibDir, 0o755); err != nil {
		t.Fatal(err)
	}
	a := &App{persistentMibDir: mibDir, mibService: mib.NewService(mibDir), mibs: mibs}

	entries, err := mibs.ReadDir("mibs")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("no bundled MIBs; this test proves nothing")
	}
	srcDir := t.TempDir()
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		p := filepath.Join(srcDir, e.Name())
		if err := os.WriteFile(p, []byte("REPLACEMENT DEFINITIONS ::= BEGIN\nEND\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if res := a.importSingleFile(p); res.Success {
			t.Errorf("%s was importable over the bundled copy", e.Name())
		}
	}
}

// The detector: a MIB that is NOT bundled must still import, or the fix would
// have broken the feature it is protecting.
func TestAVendorMibStillImports(t *testing.T) {
	root := t.TempDir()
	mibDir := filepath.Join(root, "SnmpLens", "mibs")
	if err := os.MkdirAll(mibDir, 0o755); err != nil {
		t.Fatal(err)
	}
	a := &App{persistentMibDir: mibDir, mibService: mib.NewService(mibDir), mibs: mibs}

	srcDir := t.TempDir()
	p := filepath.Join(srcDir, "ACME-POE-MIB")
	if err := os.WriteFile(p, []byte("ACME-POE-MIB DEFINITIONS ::= BEGIN\nEND\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := a.importSingleFile(p)
	if !res.Success || res.Skipped {
		t.Fatalf("a vendor MIB was refused: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(mibDir, "ACME-POE-MIB")); err != nil {
		t.Fatalf("it was not written: %v", err)
	}
}
