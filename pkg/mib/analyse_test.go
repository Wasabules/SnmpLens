package mib

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The case that motivated the whole pass. Measured beforehand: gosmi loads this
// with err=nil and IsLoaded=true, then resolves both objects to a nil type and
// an EMPTY OID, and nothing anywhere says a word.
const silentlyBroken = `BAD-MIB DEFINITIONS ::= BEGIN
IMPORTS OBJECT-TYPE FROM SNMPv2-SMI;
alpha OBJECT-TYPE
    SYNTAX      Integerr32
    MAX-ACCESS  read-only
    STATUS      current
    DESCRIPTION "a type that does not exist"
    ::= { enterprises 1 }
beta OBJECT-TYPE
    SYNTAX      Integer32
    MAX-ACCESS  read-only
    STATUS      current
    DESCRIPTION "the same OID as alpha"
    ::= { enterprises 1 }
END
`

func by(diags []Diagnostic, code string) []Diagnostic {
	var out []Diagnostic
	for _, d := range diags {
		if d.Code == code {
			out = append(out, d)
		}
	}
	return out
}

func TestAnalyseCatchesTheUnknownType(t *testing.T) {
	diags := Analyse(silentlyBroken, testCatalogue())
	hits := by(diags, CodeUnknownType)
	if len(hits) != 1 {
		t.Fatalf("got %d unknown-type diagnostics, want 1: %+v", len(hits), diags)
	}
	if hits[0].Symbol != "Integerr32" {
		t.Errorf("symbol = %q", hits[0].Symbol)
	}
	if hits[0].Line != 4 {
		t.Errorf("line = %d, want 4 (the SYNTAX line)", hits[0].Line)
	}
	if hits[0].Severity != SevError {
		t.Errorf("severity = %q; gosmi produces a typeless object, this is not advice", hits[0].Severity)
	}
}

// Two objects on one OID: gosmi accepts it and one of them becomes
// permanently unreachable.
func TestAnalyseCatchesTheDuplicateOid(t *testing.T) {
	hits := by(Analyse(silentlyBroken, testCatalogue()), CodeDuplicateOid)
	if len(hits) != 1 {
		t.Fatalf("got %d duplicate-oid diagnostics, want 1", len(hits))
	}
	if !strings.Contains(hits[0].Message, "alpha") || !strings.Contains(hits[0].Message, "beta") {
		t.Errorf("the message should name both objects: %q", hits[0].Message)
	}
}

// A parent nobody defines. Without this the object silently lands nowhere.
func TestAnalyseCatchesAnUnknownParent(t *testing.T) {
	src := `X-MIB DEFINITIONS ::= BEGIN
IMPORTS OBJECT-TYPE, Integer32 FROM SNMPv2-SMI;
x OBJECT-TYPE
    SYNTAX Integer32
    MAX-ACCESS read-only
    STATUS current
    DESCRIPTION "d"
    ::= { nowhereAtAll 1 }
END
`
	hits := by(Analyse(src, testCatalogue()), CodeUnknownParent)
	if len(hits) != 1 || hits[0].Symbol != "nowhereAtAll" {
		t.Fatalf("unknown parent not reported: %+v", Analyse(src, testCatalogue()))
	}
}

// The commonest reason a pasted vendor MIB never loads, and the remedy is to
// fetch the module rather than to edit anything.
func TestAnalyseCatchesAMissingModule(t *testing.T) {
	src := `X-MIB DEFINITIONS ::= BEGIN
IMPORTS OBJECT-TYPE FROM SNMPv2-SMI
        ciscoMgmt FROM CISCO-SMI;
END
`
	hits := by(Analyse(src, testCatalogue()), CodeUnknownModule)
	if len(hits) != 1 || hits[0].Symbol != "CISCO-SMI" {
		t.Fatalf("missing module not reported: %+v", Analyse(src, testCatalogue()))
	}
	if hits[0].Line == 0 {
		t.Error("no position")
	}
}

// RFC2578: a conceptual row and a table must be not-accessible.
func TestAnalyseCatchesAReadableRow(t *testing.T) {
	src := `X-MIB DEFINITIONS ::= BEGIN
IMPORTS OBJECT-TYPE, Integer32 FROM SNMPv2-SMI;
xEntry OBJECT-TYPE
    SYNTAX Integer32
    MAX-ACCESS read-only
    STATUS current
    DESCRIPTION "a row that should not be readable"
    INDEX { xIndex }
    ::= { enterprises 1 }
xIndex OBJECT-TYPE
    SYNTAX Integer32
    MAX-ACCESS read-only
    STATUS current
    DESCRIPTION "d"
    ::= { enterprises 2 }
END
`
	if len(by(Analyse(src, testCatalogue()), CodeRowAccess)) != 1 {
		t.Errorf("a readable conceptual row was accepted: %+v", Analyse(src, testCatalogue()))
	}
}

func TestAnalyseCatchesAnUndefinedIndex(t *testing.T) {
	src := `X-MIB DEFINITIONS ::= BEGIN
IMPORTS OBJECT-TYPE, Integer32 FROM SNMPv2-SMI;
xEntry OBJECT-TYPE
    SYNTAX Integer32
    MAX-ACCESS not-accessible
    STATUS current
    DESCRIPTION "d"
    INDEX { doesNotExist }
    ::= { enterprises 1 }
END
`
	hits := by(Analyse(src, testCatalogue()), CodeIndexUndefined)
	if len(hits) != 1 || hits[0].Symbol != "doesNotExist" {
		t.Errorf("undefined INDEX not reported: %+v", Analyse(src, testCatalogue()))
	}
}

// A file that does not parse already has a syntax error on screen; piling
// guesses on top would bury it.
func TestAnalyseSaysNothingAboutAnUnparseableFile(t *testing.T) {
	if d := Analyse("BROKEN {{{", testCatalogue()); len(d) != 0 {
		t.Errorf("advice offered on an unparseable file: %+v", d)
	}
}

// The bar for the whole feature: the shipped MIBs are correct, so any ERROR on
// them is a bug in the analyser rather than in the file.
func TestAnalyseIsQuietOnTheBundledCorpus(t *testing.T) {
	entries, err := os.ReadDir("../../mibs")
	if err != nil {
		t.Skip("no corpus")
	}

	// Build a catalogue naming every bundled module, so cross-module imports
	// resolve the way they do at runtime.
	cat := Catalogue{}
	for _, e := range entries {
		if !e.IsDir() {
			cat.Modules = append(cat.Modules, e.Name())
		}
	}
	// Every symbol any bundled MIB exports would be in the real catalogue; the
	// corpus check therefore only asserts on the checks that do not need it.
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join("../../mibs", e.Name()))
		if err != nil {
			continue
		}
		content, _ := NormaliseSource(raw)
		for _, d := range Analyse(content, cat) {
			if d.Severity != SevError {
				continue
			}
			// Missing-module and unknown-symbol checks need a full catalogue,
			// which this test deliberately does not build.
			if d.Code == CodeUnknownModule || d.Code == CodeUnknownType ||
				d.Code == CodeUnknownParent || d.Code == CodeIndexUndefined {
				continue
			}
			t.Errorf("%s: FALSE POSITIVE %d:%d [%s] %s", e.Name(), d.Line, d.Column, d.Code, d.Message)
		}
	}
}

// Duplicate detection must not fire on the corpus, where every OID is unique.
func TestNoDuplicateOidsInTheBundledCorpus(t *testing.T) {
	entries, err := os.ReadDir("../../mibs")
	if err != nil {
		t.Skip("no corpus")
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		raw, _ := os.ReadFile(filepath.Join("../../mibs", e.Name()))
		content, _ := NormaliseSource(raw)
		if hits := by(Analyse(content, Catalogue{}), CodeDuplicateOid); len(hits) > 0 {
			t.Errorf("%s: %d false duplicate-OID reports, first: %s", e.Name(), len(hits), hits[0].Message)
		}
	}
}
