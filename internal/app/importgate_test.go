package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"SnmpLens/pkg/mib"
)

func newImportApp(t *testing.T) (*App, string) {
	t.Helper()
	root := t.TempDir()
	mibDir := filepath.Join(root, "SnmpLens", "mibs")
	if err := os.MkdirAll(mibDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return &App{persistentMibDir: mibDir, mibService: mib.NewService(mibDir), mibs: mibs}, mibDir
}

// ImportMibFiles reads any absolute path the renderer names, and MibEditorRead
// hands the content of anything in the MIB directory back. Those two calls
// together are an arbitrary-file-read primitive over the bridge, and what
// decides how much it is worth is what is allowed to land in the directory.
func TestImportRefusesWhatIsNotAMib(t *testing.T) {
	a, mibDir := newImportApp(t)
	srcDir := t.TempDir()

	cases := []struct {
		name    string
		content []byte
		says    string
	}{
		{"id_rsa", []byte("-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEA\n-----END OPENSSH PRIVATE KEY-----\n"), "DEFINITIONS"},
		{"passwords.txt", []byte("admin:hunter2\nroot:correcthorse\n"), "DEFINITIONS"},
		{"spec.pdf", []byte("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n"), "PDF"},
		{"mibs.zip", []byte("PK\x03\x04\x14\x00\x00\x00"), "zip"},
		{"page.html", []byte("<!DOCTYPE html>\n<html><body>IF-MIB</body></html>"), "HTML"},
		{"cookies.sqlite", []byte("SQLite format 3\x00\x00\x00\x01"), "binary"},
		{"empty", []byte(""), "empty"},
	}

	for _, c := range cases {
		p := filepath.Join(srcDir, c.name)
		if err := os.WriteFile(p, c.content, 0o644); err != nil {
			t.Fatal(err)
		}
		res := a.importSingleFile(p)
		if res.Success {
			t.Errorf("%s was imported; it is now readable through MibEditorRead", c.name)
			continue
		}
		if !strings.Contains(res.Error, c.says) {
			t.Errorf("%s: the refusal does not say what the file is: %q", c.name, res.Error)
		}
		// And nothing was left behind.
		if _, err := os.Stat(filepath.Join(mibDir, c.name)); err == nil {
			t.Errorf("%s was copied in despite the refusal", c.name)
		}
	}
}

// The gate must not become a validity check. A MIB with a syntax error is
// exactly what the diagnosis feature exists for, and refusing those would
// remove the reason anyone imports a broken file.
func TestABrokenMibStillImports(t *testing.T) {
	a, mibDir := newImportApp(t)
	srcDir := t.TempDir()

	broken := "ACME-POE-MIB DEFINITIONS ::= BEGIN\nIMPORTS FROM;\nacme OBJECT IDENTIFIER ::= { enterprises\nEND\n"
	p := filepath.Join(srcDir, "ACME-POE-MIB")
	if err := os.WriteFile(p, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	if res := a.importSingleFile(p); !res.Success {
		t.Fatalf("a MIB with a syntax error was refused: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(mibDir, "ACME-POE-MIB")); err != nil {
		t.Fatalf("it was not written: %v", err)
	}
}

// Vendor MIBs routinely carry a licence header longer than the 4 KiB window
// describeNonMib looks at, so the gate re-checks the whole file. Refusing a
// real MIB is a worse failure here than accepting a text file that happens to
// contain the word.
func TestALongLicenceHeaderDoesNotLookLikeANonMib(t *testing.T) {
	a, mibDir := newImportApp(t)
	srcDir := t.TempDir()

	var b strings.Builder
	for b.Len() < 6000 {
		b.WriteString("-- Copyright (c) 2026 Example Corp. All rights reserved. Redistribution\n")
	}
	b.WriteString("BIG-VENDOR-MIB DEFINITIONS ::= BEGIN\nEND\n")

	p := filepath.Join(srcDir, "BIG-VENDOR-MIB")
	if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	if res := a.importSingleFile(p); !res.Success {
		t.Fatalf("a MIB behind a 6 KB licence header was refused: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(mibDir, "BIG-VENDOR-MIB")); err != nil {
		t.Fatalf("it was not written: %v", err)
	}

	// And the narrower diagnosis window still says what it says — this test is
	// about the gate being wider, not about changing the diagnosis.
	if _, _, bad := mib.ImportRejection([]byte(b.String())); bad {
		t.Error("ImportRejection disagrees with importSingleFile")
	}
}

// An unbounded os.ReadFile on a renderer-named path is a way to take the
// process down without sending a packet.
func TestImportRefusesAFileTooLargeToBeAMib(t *testing.T) {
	a, _ := newImportApp(t)
	srcDir := t.TempDir()

	p := filepath.Join(srcDir, "HUGE-MIB")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	// Sparse where the filesystem supports it, so this costs no real disk.
	if err := f.Truncate(maxImportBytes + 1); err != nil {
		f.Close()
		t.Skipf("cannot create a large sparse file: %v", err)
	}
	f.Close()

	res := a.importSingleFile(p)
	if res.Success {
		t.Fatal("a file larger than the cap was read into memory and imported")
	}
	if !strings.Contains(res.Error, "MB") {
		t.Errorf("the refusal does not say how big it is: %q", res.Error)
	}
}
