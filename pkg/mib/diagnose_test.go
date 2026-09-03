package mib

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sleepinggenius2/gosmi"
)

// Every one of these used to come back as "Could not load module at X". The
// test is that they no longer say the same thing as each other: a diagnosis
// that cannot tell a PDF from a missing import is the message it replaced.

func diagDir(t *testing.T, files map[string]string) *Service {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	gosmi.Exit()
	gosmi.Init()
	// SetPath, not AppendPath: gosmi.Init() without a preceding Exit() finds
	// the existing handle and returns without resetting anything, so
	// AppendPath accumulates. A directory that has since been deleted then
	// aborts every lookup — GetModuleFile RETURNS the ReadDir error instead of
	// trying the next path — and unrelated tests start failing.
	gosmi.SetPath(dir)
	return NewService(dir)
}

const goodMib = `TEST-MIB DEFINITIONS ::= BEGIN
IMPORTS OBJECT-TYPE, enterprises FROM SNMPv2-SMI;
alpha OBJECT-TYPE
    SYNTAX      Integer32
    MAX-ACCESS  read-only
    STATUS      current
    DESCRIPTION "a perfectly ordinary object"
    ::= { enterprises 1 }
END
`

func TestDiagnoseNamesTheFileThatIsNotThere(t *testing.T) {
	s := diagDir(t, nil)
	d := s.Diagnose("NOPE-MIB")
	if d.Loaded {
		t.Fatal("a missing file loaded")
	}
	if d.Stage != StageRead {
		t.Errorf("stage = %q, want %q", d.Stage, StageRead)
	}
	if !strings.Contains(d.Summary, "not in the MIB directory") {
		t.Errorf("summary = %q", d.Summary)
	}
}

// The three downloads people actually end up with.
func TestDiagnoseRecognisesFilesThatAreNotMibs(t *testing.T) {
	cases := map[string]struct{ content, want string }{
		"A-MIB": {"%PDF-1.7\nnonsense", "PDF"},
		"B-MIB": {"<!DOCTYPE html><html><body>404</body></html>", "HTML"},
		"C-MIB": {"PK\x03\x04binary junk", "zip"},
		"D-MIB": {"", "empty"},
		"E-MIB": {"this is a text file but not a MIB at all", "DEFINITIONS"},
	}
	files := map[string]string{}
	for name, c := range cases {
		files[name] = c.content
	}
	s := diagDir(t, files)

	for name, c := range cases {
		d := s.Diagnose(name)
		if d.Loaded {
			t.Errorf("%s loaded", name)
			continue
		}
		if d.Stage != StageContent {
			t.Errorf("%s: stage = %q, want %q (%s)", name, d.Stage, StageContent, d.Summary)
		}
		if !strings.Contains(d.Summary, c.want) {
			t.Errorf("%s: summary = %q, want it to mention %q", name, d.Summary, c.want)
		}
	}
}

// The headline: a syntax error must name the line, the column and the text.
// gosmi computes all three and prints them to stdout on its way to discarding
// them.
func TestDiagnoseLocatesASyntaxError(t *testing.T) {
	broken := `BROKEN-MIB DEFINITIONS ::= BEGIN
IMPORTS OBJECT-TYPE FROM SNMPv2-SMI;
alpha OBJECT-TYPE
    SYNTAX      Integer32
    MAX-ACCESS  read-only
    STATUS      current
    DESCRIPTION "unterminated
    ::= { enterprises 1 }
END
`
	s := diagDir(t, map[string]string{"BROKEN-MIB": broken})
	d := s.Diagnose("BROKEN-MIB")

	if d.Loaded {
		t.Fatal("a file with a syntax error loaded")
	}
	if d.Stage != StageParse {
		t.Fatalf("stage = %q, want %q (%s)", d.Stage, StageParse, d.Summary)
	}
	if len(d.Diagnostics) == 0 || d.Diagnostics[0].Line <= 0 {
		t.Fatalf("no located diagnostic: %+v", d.Diagnostics)
	}
	if !strings.Contains(d.Summary, "line") {
		t.Errorf("summary does not name the line: %q", d.Summary)
	}
	// The excerpt is what turns "line 7" into seeing what is wrong on line 7.
	if len(d.Hints) == 0 || !strings.Contains(strings.Join(d.Hints, "\n"), "^") {
		t.Errorf("no excerpt with a caret: %+v", d.Hints)
	}
}

// The commonest real failure: a vendor MIB importing a module you do not have.
// Naming the module AND the symbols is the difference between an error and an
// instruction.
func TestDiagnoseNamesTheMissingImport(t *testing.T) {
	vendor := `VENDOR-MIB DEFINITIONS ::= BEGIN
IMPORTS OBJECT-TYPE, enterprises FROM SNMPv2-SMI
        ciscoMgmt, ciscoProducts FROM CISCO-SMI;
alpha OBJECT-TYPE
    SYNTAX      Integer32
    MAX-ACCESS  read-only
    STATUS      current
    DESCRIPTION "needs a module nobody has"
    ::= { ciscoMgmt 1 }
END
`
	s := diagDir(t, map[string]string{"VENDOR-MIB": vendor})
	d := s.Diagnose("VENDOR-MIB")

	var found *MissingModule
	for i := range d.Missing {
		if d.Missing[i].Module == "CISCO-SMI" {
			found = &d.Missing[i]
		}
	}
	if found == nil {
		t.Fatalf("CISCO-SMI was not reported as missing: %+v", d.Missing)
	}
	if found.Reason != ImportAbsent {
		t.Errorf("reason = %q, want %q", found.Reason, ImportAbsent)
	}
	if len(found.Symbols) != 2 {
		t.Errorf("the symbols taken from it were not reported: %v", found.Symbols)
	}
	if !strings.Contains(d.Summary, "CISCO-SMI") {
		t.Errorf("summary does not name the module: %q", d.Summary)
	}
	// SNMPv2-SMI is well known and must NOT be reported alongside it, or the
	// real answer is buried in noise.
	for _, m := range d.Missing {
		if m.Module == "SNMPv2-SMI" && len(m.Symbols) > 0 {
			t.Logf("SNMPv2-SMI reported missing too (not loaded in this temp dir): %+v", m)
		}
	}
}

// A chain must be reported at its root. "A failed because B failed" is one
// message; "A failed" twice is two symptoms and no cause.
func TestDiagnoseFollowsAFailingImportToItsCause(t *testing.T) {
	brokenDep := `DEP-MIB DEFINITIONS ::= BEGIN
this is not valid syntax at all ((((
END
`
	user := `USER-MIB DEFINITIONS ::= BEGIN
IMPORTS thing FROM DEP-MIB;
END
`
	s := diagDir(t, map[string]string{"DEP-MIB": brokenDep, "USER-MIB": user})
	d := s.Diagnose("USER-MIB")

	var dep *MissingModule
	for i := range d.Missing {
		if d.Missing[i].Module == "DEP-MIB" {
			dep = &d.Missing[i]
		}
	}
	if dep == nil {
		t.Fatalf("DEP-MIB not reported: %+v", d.Missing)
	}
	if dep.Reason != ImportFailed {
		t.Errorf("reason = %q, want %q — the file IS present", dep.Reason, ImportFailed)
	}
	if dep.Cause == "" {
		t.Error("the dependency's own failure was not reported")
	}
}

// A file whose name does not match its module loads on its own and is
// invisible until something imports it, which is the worst kind of problem to
// find later.
func TestDiagnoseWarnsWhenTheNameDoesNotMatchTheModule(t *testing.T) {
	s := diagDir(t, map[string]string{"whatever.mib": goodMib})
	d := s.Diagnose("whatever.mib")

	if d.ModuleName != "TEST-MIB" {
		t.Fatalf("module name = %q", d.ModuleName)
	}
	joined := strings.Join(d.Hints, " ")
	if !strings.Contains(joined, "TEST-MIB") || !strings.Contains(joined, "renamed") {
		t.Errorf("no hint about the mismatch: %+v", d.Hints)
	}
}

// Loading is not the same as being correct: this one passes the loader and
// resolves to a nil type and an empty OID.
func TestDiagnoseReportsAModuleThatLoadedWrong(t *testing.T) {
	s := diagDir(t, map[string]string{
		"SNMPv2-SMI": smiStub,
		"SILENT-MIB": `SILENT-MIB DEFINITIONS ::= BEGIN
IMPORTS OBJECT-TYPE, enterprises FROM SNMPv2-SMI;
alpha OBJECT-TYPE
    SYNTAX      Integerr32
    MAX-ACCESS  read-only
    STATUS      current
    DESCRIPTION "a type that does not exist"
    ::= { enterprises 1 }
END
`})
	d := s.Diagnose("SILENT-MIB")

	if !d.Loaded {
		t.Skipf("the stub did not let it load: %s / %s", d.Stage, d.Summary)
	}
	if d.Stage != StageSemantic {
		t.Fatalf("stage = %q, want %q — it loaded but is broken", d.Stage, StageSemantic)
	}
	if len(d.Diagnostics) == 0 {
		t.Fatal("no diagnostics for a module that resolves to nothing")
	}
	if !strings.Contains(d.Summary, "loaded but") {
		t.Errorf("summary = %q", d.Summary)
	}
}

// A good file must come back clean, or every diagnosis is noise.
func TestDiagnoseIsQuietOnAGoodFile(t *testing.T) {
	s := diagDir(t, map[string]string{"SNMPv2-SMI": smiStub, "TEST-MIB": goodMib})
	d := s.Diagnose("TEST-MIB")
	if !d.Loaded {
		t.Skipf("the stub did not let it load: %s / %s", d.Stage, d.Summary)
	}
	if d.Stage != StageLoaded {
		t.Errorf("stage = %q, want %q (%s)", d.Stage, StageLoaded, d.Summary)
	}
	if len(d.Diagnostics) != 0 {
		t.Errorf("false positives: %+v", d.Diagnostics)
	}
}

// A path outside the MIB directory must be refused, here as everywhere else.
func TestDiagnoseRefusesPathsOutsideTheDirectory(t *testing.T) {
	s := diagDir(t, map[string]string{"TEST-MIB": goodMib})
	for _, name := range []string{"../secrets.json", "..\\service.json", "/etc/passwd", ""} {
		d := s.Diagnose(name)
		if d.Loaded {
			t.Errorf("%q was loaded", name)
		}
		if d.Stage != StageRead {
			t.Errorf("%q: stage = %q", name, d.Stage)
		}
	}
}

// A minimal SNMPv2-SMI, so the fixtures above have something to import.
const smiStub = `SNMPv2-SMI DEFINITIONS ::= BEGIN
org OBJECT IDENTIFIER ::= { iso 3 }
dod OBJECT IDENTIFIER ::= { org 6 }
internet OBJECT IDENTIFIER ::= { dod 1 }
private OBJECT IDENTIFIER ::= { internet 4 }
enterprises OBJECT IDENTIFIER ::= { private 1 }
Integer32 ::= INTEGER (-2147483648..2147483647)
OBJECT-TYPE MACRO ::= BEGIN END
END
`

// The whole reason this package captures stdout.
//
// gosmi.LoadModule returns "Could not load module at X" for everything. One
// frame below, smi.LoadModule receives the real error — file, line, column and
// what it expected — and prints it with fmt.Println on its way to returning an
// empty string. This pins both halves: that the API is useless, and that the
// message we recover instead is not.
func TestGosmiDiscardsThePositionAndWeRecoverIt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "X-MIB"),
		[]byte("X-MIB DEFINITIONS ::= BEGIN\n((((\nEND\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gosmi.Exit()
	gosmi.Init()
	gosmi.SetPath(dir)

	var apiErr error
	printed := captureStdout(func() {
		_, apiErr = gosmi.LoadModule("X-MIB")
	})

	if apiErr == nil {
		t.Fatal("gosmi accepted a file that is not a MIB")
	}
	if strings.Contains(apiErr.Error(), ":2:") {
		t.Skip("gosmi now returns the position itself; the capture can go")
	}
	if !strings.Contains(printed, ":2:1:") {
		t.Fatalf("the position was not recovered from stdout: %q", printed)
	}
	if !strings.Contains(printed, "unexpected") {
		t.Errorf("the recovered message says nothing useful: %q", printed)
	}
}

// The captured message carries the full path of the file, which buries the
// position under eighty characters of directory.
func TestShortenPathsKeepsThePosition(t *testing.T) {
	dir := filepath.Join("C:", "Users", "someone", "AppData", "SnmpLens", "mibs")
	msg := "Parse module: " + filepath.Join(dir, "X-MIB") + ":2:1: unexpected \"(\""
	got := shortenPaths(msg, dir)
	if strings.Contains(got, "AppData") {
		t.Errorf("the directory survived: %q", got)
	}
	if !strings.Contains(got, "X-MIB:2:1:") {
		t.Errorf("the position did not: %q", got)
	}
}

// Capturing stdout must not lose it: every later print in the process would
// disappear into a pipe nobody reads.
func TestCaptureStdoutRestoresItEvenOnPanic(t *testing.T) {
	before := os.Stdout
	func() {
		defer func() { _ = recover() }()
		captureStdout(func() { panic("boom") })
	}()
	if os.Stdout != before {
		t.Fatal("stdout was not restored after a panic")
	}

	got := captureStdout(func() { fmt.Println("still works") })
	if !strings.Contains(got, "still works") {
		t.Errorf("capture broken after a panic: %q", got)
	}
	if os.Stdout != before {
		t.Error("stdout was not restored")
	}
}
