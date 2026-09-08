package mib

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func outlineOfSource(src string) []OutlineItem {
	return AnalyseAll(src, Catalogue{}).Outline
}

func find(items []OutlineItem, name string) *OutlineItem {
	for i := range items {
		if items[i].Name == name {
			return &items[i]
		}
	}
	return nil
}

const outlineSample = `ACME-MIB DEFINITIONS ::= BEGIN

IMPORTS
    OBJECT-TYPE, MODULE-IDENTITY, Integer32, Counter32, enterprises
        FROM SNMPv2-SMI
    TEXTUAL-CONVENTION, DisplayString
        FROM SNMPv2-TC;

acmeMIB MODULE-IDENTITY
    LAST-UPDATED "202601010000Z"
    ORGANIZATION "Acme"
    CONTACT-INFO "noc@acme.example"
    DESCRIPTION  "The Acme MIB.
                  A second line nobody needs in a list."
    ::= { enterprises 99999 }

AcmeState ::= TEXTUAL-CONVENTION
    STATUS      current
    DESCRIPTION "A port state."
    SYNTAX      INTEGER { up(1), down(2) }

acmeObjects OBJECT IDENTIFIER ::= { acmeMIB 1 }

acmeUptime OBJECT-TYPE
    SYNTAX      Counter32
    MAX-ACCESS  read-only
    STATUS      current
    DESCRIPTION "Seconds since boot."
    ::= { acmeObjects 1 }

acmePortTable OBJECT-TYPE
    SYNTAX      SEQUENCE OF AcmePortEntry
    MAX-ACCESS  not-accessible
    STATUS      current
    DESCRIPTION "The ports."
    ::= { acmeObjects 2 }

acmePortEntry OBJECT-TYPE
    SYNTAX      AcmePortEntry
    MAX-ACCESS  not-accessible
    STATUS      current
    DESCRIPTION "One port."
    INDEX       { acmePortIndex }
    ::= { acmePortTable 1 }

AcmePortEntry ::= SEQUENCE {
    acmePortIndex Integer32,
    acmePortState AcmeState
}

acmePortIndex OBJECT-TYPE
    SYNTAX      Integer32 (1..48)
    MAX-ACCESS  not-accessible
    STATUS      current
    DESCRIPTION "The port number."
    ::= { acmePortEntry 1 }

acmePortState OBJECT-TYPE
    SYNTAX      AcmeState
    MAX-ACCESS  read-only
    STATUS      current
    DESCRIPTION "Whether the port is up."
    ::= { acmePortEntry 2 }

END
`

// The outline names what the file defines, and says which KIND each one is —
// a table, a row and a column read the same in the source and are three
// different things in the tree.
func TestTheOutlineTellsTheKindsApart(t *testing.T) {
	items := outlineOfSource(outlineSample)
	if len(items) == 0 {
		t.Fatal("the outline is empty")
	}

	want := map[string]string{
		"acmeMIB":       OutlineModule,
		"AcmeState":     OutlineType,
		"acmeObjects":   OutlineNode,
		"acmeUptime":    OutlineObject,
		"acmePortTable": OutlineTable,
		"acmePortEntry": OutlineRow,
		"acmePortIndex": OutlineObject,
		"AcmePortEntry": OutlineType,
	}
	for name, kind := range want {
		got := find(items, name)
		if got == nil {
			t.Errorf("%s is missing from the outline", name)
			continue
		}
		if got.Kind != kind {
			t.Errorf("%s: kind = %q, want %q", name, got.Kind, kind)
		}
	}
}

// It is a navigation aid, so the position is the point.
func TestEveryItemPointsAtItsDefinition(t *testing.T) {
	items := outlineOfSource(outlineSample)
	lines := strings.Split(outlineSample, "\n")

	for _, it := range items {
		if it.Line < 1 || it.Line > len(lines) {
			t.Errorf("%s: line %d is outside the file", it.Name, it.Line)
			continue
		}
		// The name must be on the line the outline points at — this is what
		// makes clicking an entry land on the definition rather than near it.
		if !strings.Contains(lines[it.Line-1], it.Name) {
			t.Errorf("%s: line %d reads %q", it.Name, it.Line, strings.TrimSpace(lines[it.Line-1]))
		}
	}
}

// The three things a reader checks before scrolling.
func TestAnObjectCarriesWhatTheListNeedsToShow(t *testing.T) {
	items := outlineOfSource(outlineSample)

	up := find(items, "acmeUptime")
	if up == nil {
		t.Fatal("acmeUptime is missing")
	}
	if up.Syntax != "Counter32" || up.Access != "read-only" || up.Status != "current" {
		t.Errorf("acmeUptime: %+v", up)
	}
	if up.Parent != "acmeObjects" || up.SubID != "1" {
		t.Errorf("acmeUptime is placed at %q %q; the outline cannot be a tree without that",
			up.Parent, up.SubID)
	}
	// The first line only: a DESCRIPTION routinely runs longer than the whole
	// list it would be shown in.
	mod := find(items, "acmeMIB")
	if mod == nil || strings.Contains(mod.Description, "second line") {
		t.Errorf("the module description is not shortened: %q", mod.Description)
	}

	// An enumeration is named, never spelled out: "INTEGER { up(1), down(2) }"
	// is the reason to open the definition, not a column in a list.
	state := find(items, "AcmeState")
	if state == nil || !strings.HasPrefix(state.Syntax, "INTEGER") || strings.Contains(state.Syntax, "up(1)") {
		t.Errorf("AcmeState syntax = %q", state.Syntax)
	}
	tbl := find(items, "acmePortTable")
	if tbl == nil || tbl.Syntax != "SEQUENCE OF AcmePortEntry" {
		t.Errorf("acmePortTable syntax = %q", tbl.Syntax)
	}
}

// The moment an outline is most useful is the moment it is easiest to lose: a
// file with a syntax error part way down still has everything above it.
func TestABrokenFileStillHasAnOutlineOfWhatParsed(t *testing.T) {
	broken := strings.Replace(outlineSample,
		`acmePortIndex OBJECT-TYPE
    SYNTAX      Integer32 (1..48)`,
		`acmePortIndex OBJECT-TYPE
    SYNTAX      Integer32 ((((`, 1)

	analysis := AnalyseAll(broken, Catalogue{})
	if len(analysis.Diagnostics) == 0 {
		t.Fatal("setup: the broken file produced no diagnostic")
	}
	if len(analysis.Outline) == 0 {
		t.Fatal("a file with a syntax error has no outline at all")
	}
	if find(analysis.Outline, "acmeUptime") == nil {
		t.Error("a definition before the error is missing from the outline")
	}
}

// It must survive what a half-typed file really looks like, because that is all
// it ever sees: this runs 350 ms after a pause in typing.
func TestTheOutlineSurvivesHalfTypedInput(t *testing.T) {
	for _, src := range []string{
		"", "\n\n", "ACME-MIB",
		"ACME-MIB DEFINITIONS ::= BEGIN",
		"ACME-MIB DEFINITIONS ::= BEGIN\nEND\n",
		"ACME-MIB DEFINITIONS ::= BEGIN\nfoo OBJECT-TYPE\n  SYNTAX\nEND\n",
		"ACME-MIB DEFINITIONS ::= BEGIN\nfoo OBJECT-TYPE\n  SYNTAX SEQUENCE OF\nEND\n",
		"ACME-MIB DEFINITIONS ::= BEGIN\nfoo ::= TEXTUAL-CONVENTION\nEND\n",
		"ACME-MIB DEFINITIONS ::= BEGIN\nfoo OBJECT IDENTIFIER ::= {\nEND\n",
	} {
		got := AnalyseAll(src, Catalogue{})
		if got.Outline == nil {
			t.Errorf("%q produced a nil outline, which crosses the bridge as null", src)
		}
	}
}

// And on the real corpus, where the shapes are the ones people actually write.
func TestTheOutlineHandlesTheBundledCorpus(t *testing.T) {
	entries, err := os.ReadDir("../../mibs")
	if err != nil {
		t.Skip("no corpus")
	}
	total := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join("../../mibs", e.Name()))
		if err != nil {
			continue
		}
		content, _ := NormaliseSource(raw)
		items := AnalyseAll(content, Catalogue{}).Outline
		if len(items) == 0 {
			t.Errorf("%s: no outline", e.Name())
			continue
		}
		total += len(items)
		for _, it := range items {
			if it.Name == "" {
				t.Errorf("%s: an item with no name at line %d", e.Name(), it.Line)
			}
			if it.Line < 1 {
				t.Errorf("%s: %s has no position", e.Name(), it.Name)
			}
		}
	}
	t.Logf("%d definitions across the bundled corpus", total)
}
