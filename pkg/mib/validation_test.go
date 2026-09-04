package mib

import (
	"os"
	"strings"
	"testing"

	"github.com/sleepinggenius2/gosmi"
)

func diagsFor(t *testing.T, content string, codes ...string) []Diagnostic {
	t.Helper()
	if _, err := os.Stat("../../mibs"); err != nil {
		t.Skip("no corpus")
	}
	gosmi.Exit()
	gosmi.Init()
	gosmi.SetPath("../../mibs")
	if _, err := NewService("../../mibs").LoadAll(); err != nil {
		t.Skip("corpus")
	}
	all := AnalyseAll(content, Symbols()).Diagnostics
	if len(codes) == 0 {
		return all
	}
	var out []Diagnostic
	for _, d := range all {
		for _, c := range codes {
			if d.Code == c {
				out = append(out, d)
			}
		}
	}
	return out
}

// #16: "the module is never closed with END" on a file that DOES end with END.
func TestEndWithTrailingCommentIsClosed(t *testing.T) {
	for _, tail := range []string{
		"END\n",
		"END -- that is all\n",
		"END   \n",
		"END",
		"END\r\n",
	} {
		body := "X-MIB DEFINITIONS ::= BEGIN\nfoo OBJECT IDENTIFIER ::= { iso 1 }\n" + tail
		var found []string
		for _, d := range Validate(body) {
			if strings.Contains(strings.ToLower(d.Message), "end") {
				found = append(found, d.Severity+": "+d.Message)
			}
		}
		if len(found) > 0 {
			t.Errorf("tail %q -> %v", tail, found)
		}
	}
}

// #15: unused-import on symbols that ARE used.
func TestUsedMacrosAreNotReportedUnused(t *testing.T) {
	body := `Y-MIB DEFINITIONS ::= BEGIN
IMPORTS
    OBJECT-TYPE, Integer32, Counter32
        FROM SNMPv2-SMI
    DisplayString, TEXTUAL-CONVENTION
        FROM SNMPv2-TC;

Thing ::= TEXTUAL-CONVENTION
    STATUS current
    DESCRIPTION "uses DisplayString"
    SYNTAX DisplayString

a OBJECT-TYPE
    SYNTAX Integer32
    MAX-ACCESS read-only
    STATUS current
    DESCRIPTION "uses Integer32"
    ::= { iso 1 }

b OBJECT-TYPE
    SYNTAX Counter32
    MAX-ACCESS read-only
    STATUS current
    DESCRIPTION "uses Counter32"
    ::= { iso 2 }
END
`
	for _, d := range diagsFor(t, body, CodeUnusedImport) {
		t.Errorf("%s reported unused, and it is used: %s (line %d)", d.Symbol, d.Message, d.Line)
	}
}

// And on the real corpus, where every import is used.
func TestNoUnusedImportOnTheBundledCorpus(t *testing.T) {
	if _, err := os.Stat("../../mibs"); err != nil {
		t.Skip("no corpus")
	}
	gosmi.Exit()
	gosmi.Init()
	gosmi.SetPath("../../mibs")
	if _, err := NewService("../../mibs").LoadAll(); err != nil {
		t.Skip("corpus")
	}
	cat := Symbols()

	files, _ := ListMibFiles("../../mibs")
	total := 0
	for _, name := range files {
		raw, err := os.ReadFile("../../mibs/" + name)
		if err != nil {
			continue
		}
		content, _ := NormaliseSource(raw)
		var unused []string
		for _, d := range AnalyseAll(content, cat).Diagnostics {
			if d.Code == CodeUnusedImport {
				unused = append(unused, d.Symbol)
			}
		}
		if len(unused) > 0 {
			total += len(unused)
			t.Logf("%-20s %d unused: %v", name, len(unused), unused)
		}
	}
	if total > 0 {
		t.Errorf("%d unused-import diagnostics on the bundled corpus", total)
	}
}

// #10: a MIB rooted at bare numeric sub-identifiers.
func TestBareNumericRootIsAccepted(t *testing.T) {
	body := `Z-MIB DEFINITIONS ::= BEGIN
IMPORTS OBJECT-TYPE, Integer32 FROM SNMPv2-SMI;

acme OBJECT IDENTIFIER ::= { 1 3 6 1 4 1 99999 }

a OBJECT-TYPE
    SYNTAX Integer32
    MAX-ACCESS read-only
    STATUS current
    DESCRIPTION "rooted at a bare numeric OID"
    ::= { acme 1 }
END
`
	// A bare numeric root is legal SMI and must not be reported as an error.
	for _, d := range diagsFor(t, body) {
		if d.Severity == SevError {
			t.Errorf("%s: %s (line %d)", d.Code, d.Message, d.Line)
		}
	}
}

// #36: the same OID written two ways.
func TestDuplicateOidAcrossSpellings(t *testing.T) {
	body := `W-MIB DEFINITIONS ::= BEGIN
IMPORTS OBJECT-TYPE, Integer32 FROM SNMPv2-SMI;

root OBJECT IDENTIFIER ::= { iso 3 }

a OBJECT-TYPE
    SYNTAX Integer32
    MAX-ACCESS read-only
    STATUS current
    DESCRIPTION "one"
    ::= { root 1 }

b OBJECT-TYPE
    SYNTAX Integer32
    MAX-ACCESS read-only
    STATUS current
    DESCRIPTION "the same OID, spelled out"
    ::= { iso 3 1 }
END
`
	// A KNOWN limit, pinned so it is a decision rather than a surprise:
	// duplicate-oid compares the OID as written, so `{ root 1 }` and
	// `{ iso 3 1 }` naming the same node are not reported. Detecting it needs
	// the resolved numeric OID, which the AST does not carry — gosmi does, but
	// only for a module that already loaded, and this check has to work on a
	// buffer that does not.
	dups := diagsFor(t, body, CodeDuplicateOid)
	if len(dups) > 0 {
		t.Logf("now detected (the limit below can go): %s", dups[0].Message)
	}
}

// #35: long-line advice counting bytes.
func TestLongLineCountsCharactersNotBytes(t *testing.T) {
	// 100 accented characters: 100 runes, 200 bytes.
	body := "L-MIB DEFINITIONS ::= BEGIN\n-- " + strings.Repeat("é", 100) + "\nEND\n"
	for _, d := range Validate(body) {
		if strings.Contains(strings.ToLower(d.Message), "line") &&
			(strings.Contains(d.Message, "200") || strings.Contains(d.Message, "203")) {
			t.Errorf("byte count reported as characters: %s", d.Message)
		}
	}

}
