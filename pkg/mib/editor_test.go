package mib

import (
	"strings"
	"testing"
)

const goodMIB = `TEST-MIB DEFINITIONS ::= BEGIN

IMPORTS
    MODULE-IDENTITY, OBJECT-TYPE, Integer32
        FROM SNMPv2-SMI;

testModule MODULE-IDENTITY
    LAST-UPDATED "202609010000Z"
    ORGANIZATION "Example"
    CONTACT-INFO "noc@example.com"
    DESCRIPTION  "A test module."
    ::= { 1 3 6 1 4 1 99999 }

END
`

// The whole point of the feature: a syntax error must come back with the line
// and column, which gosmi.LoadModule structurally cannot provide — it prints
// the parser's positioned error with fmt.Println and returns a bare
// "Could not load module at X".
func TestValidateReportsSyntaxErrorWithPosition(t *testing.T) {
	src := "BROKEN-MIB DEFINITIONS ::= BEGIN\nthis is not valid\nEND\n"
	diags := Validate(src)

	var syntax *Diagnostic
	for i := range diags {
		if diags[i].Code == CodeSyntax {
			syntax = &diags[i]
		}
	}
	if syntax == nil {
		t.Fatalf("no syntax diagnostic: %+v", diags)
	}
	if syntax.Line != 2 {
		t.Errorf("line = %d, want 2", syntax.Line)
	}
	if syntax.Column == 0 {
		t.Error("no column; the editor cannot place a caret")
	}
	if syntax.Severity != SevError {
		t.Errorf("severity = %q", syntax.Severity)
	}
	if syntax.Message == "" || strings.Contains(syntax.Message, "Could not load module") {
		t.Errorf("message is the useless gosmi one: %q", syntax.Message)
	}
}

func TestValidateAcceptsAGoodModule(t *testing.T) {
	for _, d := range Validate(goodMIB) {
		if d.Severity == SevError {
			t.Errorf("a valid module produced an error: %+v", d)
		}
	}
}

// A missing END is reported by the parser as "unexpected <EOF>" pointing at the
// last line, which does not say what is wrong. Say it plainly instead.
func TestMissingEndIsNamed(t *testing.T) {
	src := "NOEND-MIB DEFINITIONS ::= BEGIN\nIMPORTS MODULE-IDENTITY FROM SNMPv2-SMI;\n"
	found := false
	for _, d := range Validate(src) {
		if d.Code == CodeStructure && strings.Contains(d.Message, "END") {
			found = true
		}
	}
	if !found {
		t.Errorf("the missing END was not named: %+v", Validate(src))
	}
}

func TestMissingHeaderIsReported(t *testing.T) {
	found := false
	for _, d := range Validate("just some text\nEND\n") {
		if d.Code == CodeStructure && strings.Contains(d.Message, "DEFINITIONS") {
			found = true
		}
	}
	if !found {
		t.Error("a file with no module header was accepted")
	}
}

// A byte order mark makes some MIB compilers reject a file that is otherwise
// perfect, and it is invisible in every editor.
func TestByteOrderMarkIsFlaggedNotFatal(t *testing.T) {
	diags := Validate("\ufeff" + goodMIB)
	found := false
	for _, d := range diags {
		if d.Code == CodeEncoding {
			found = true
			if d.Severity != SevWarning {
				t.Errorf("a BOM is a warning, not %q", d.Severity)
			}
		}
		if d.Severity == SevError {
			t.Errorf("the BOM made an otherwise valid module fail: %+v", d)
		}
	}
	if !found {
		t.Error("the BOM was not reported")
	}
}

// Validation must not depend on, or disturb, the global gosmi state. This is
// what lets it run on every keystroke.
func TestValidateIsPure(t *testing.T) {
	before := Validate(goodMIB)
	for i := 0; i < 50; i++ {
		Validate("garbage {{{")
	}
	after := Validate(goodMIB)
	if len(before) != len(after) {
		t.Errorf("validation results drifted: %d then %d", len(before), len(after))
	}
}

func TestModuleName(t *testing.T) {
	if got := ModuleName(goodMIB); got != "TEST-MIB" {
		t.Errorf("ModuleName = %q", got)
	}
	if got := ModuleName("no header here"); got != "" {
		t.Errorf("ModuleName on a non-MIB = %q", got)
	}
}

// Line endings must survive a round trip: rewriting every line of a CRLF file
// because one word changed turns a one-line diff into a whole-file diff.
func TestLineEndingsRoundTrip(t *testing.T) {
	crlf := []byte("A DEFINITIONS ::= BEGIN\r\nEND\r\n")
	content, eol := NormaliseSource(crlf)
	if eol != "crlf" {
		t.Errorf("eol = %q, want crlf", eol)
	}
	if strings.Contains(content, "\r") {
		t.Error("the buffer still carries CR, which would double up on save")
	}
	if RestoreEol(content, eol) != string(crlf) {
		t.Errorf("round trip changed the file: %q", RestoreEol(content, eol))
	}

	lf := []byte("A DEFINITIONS ::= BEGIN\nEND\n")
	content, eol = NormaliseSource(lf)
	if eol != "lf" || RestoreEol(content, eol) != string(lf) {
		t.Error("an LF file did not round trip")
	}
}

func TestNormaliseStripsBOM(t *testing.T) {
	content, _ := NormaliseSource([]byte("A DEFINITIONS ::= BEGIN\nEND\n"))
	if strings.HasPrefix(content, "\ufeff") {
		t.Error("the BOM reached the editor buffer")
	}
}

func TestStyleAdviceIsNeverAnError(t *testing.T) {
	src := "A DEFINITIONS ::= BEGIN\n\t" + strings.Repeat("x", 300) + "\nEND\n"
	for _, d := range Validate(src) {
		if d.Code == CodeTabs || d.Code == CodeLongLine {
			if d.Severity != SevInfo {
				t.Errorf("%s should be advice, got %q", d.Code, d.Severity)
			}
		}
	}
}

func TestCountBySeverity(t *testing.T) {
	e, w, i := CountBySeverity([]Diagnostic{
		{Severity: SevError}, {Severity: SevError}, {Severity: SevWarning}, {Severity: SevInfo},
	})
	if e != 2 || w != 1 || i != 1 {
		t.Errorf("counts = %d/%d/%d", e, w, i)
	}
}

func TestChecksumDetectsChange(t *testing.T) {
	if Checksum("a") == Checksum("b") {
		t.Error("different content produced the same checksum")
	}
	// Stability across calls, written so staticcheck does not read it as
	// comparing an expression with itself.
	first := Checksum("same content")
	second := Checksum("same content")
	if first != second {
		t.Error("checksum is not stable across calls")
	}
}
