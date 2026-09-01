package mib

import (
	"strings"
	"testing"
)

// A small stand-in for the loaded tree, so these tests are deterministic and
// do not need gosmi initialised.
func testCatalogue() Catalogue {
	return Catalogue{
		Modules: []string{"SNMPv2-SMI", "SNMPv2-TC"},
		Symbols: []Symbol{
			{Name: "MODULE-IDENTITY", Module: "SNMPv2-SMI", Kind: "type"},
			{Name: "OBJECT-TYPE", Module: "SNMPv2-SMI", Kind: "type"},
			{Name: "Counter32", Module: "SNMPv2-SMI", Kind: "type"},
			{Name: "Integer32", Module: "SNMPv2-SMI", Kind: "type"},
			{Name: "enterprises", Module: "SNMPv2-SMI", Kind: "node"},
			{Name: "DisplayString", Module: "SNMPv2-TC", Kind: "type"},
			{Name: "TruthValue", Module: "SNMPv2-TC", Kind: "type"},
		},
	}
}

const withImports = `ACME-MIB DEFINITIONS ::= BEGIN

IMPORTS
    MODULE-IDENTITY, OBJECT-TYPE
        FROM SNMPv2-SMI;

acmeCounter OBJECT-TYPE
    SYNTAX      Counter32
    MAX-ACCESS  read-only
    STATUS      current
    DESCRIPTION
        "A counter. This description mentions DisplayString on purpose."
    ::= { enterprises 1 }

END
`

// The question people actually have in front of a vendor MIB: which symbol am
// I using without importing, and where does it come from.
func TestCheckImportsFindsTheMissingSymbol(t *testing.T) {
	missing := CheckImports(withImports, testCatalogue())

	names := map[string]string{}
	for _, m := range missing {
		names[m.Symbol] = m.Module
	}
	if names["Counter32"] != "SNMPv2-SMI" {
		t.Errorf("Counter32 was not reported as missing from SNMPv2-SMI: %+v", missing)
	}
	if names["enterprises"] != "SNMPv2-SMI" {
		t.Errorf("enterprises was not reported: %+v", missing)
	}
	// Already imported: must not be reported.
	if _, wrong := names["OBJECT-TYPE"]; wrong {
		t.Error("a symbol that IS imported was reported as missing")
	}
	// Defined in the file itself.
	if _, wrong := names["acmeCounter"]; wrong {
		t.Error("a locally defined symbol was reported as missing")
	}
	// Inside a DESCRIPTION string: prose is not code.
	if _, wrong := names["DisplayString"]; wrong {
		t.Error("a word inside a DESCRIPTION was read as a symbol reference")
	}
	for _, m := range missing {
		if m.Line == 0 {
			t.Errorf("%s has no position: %+v", m.Symbol, m)
		}
	}
}

// A file that already imports everything must produce nothing, or the feature
// is noise.
func TestCheckImportsIsQuietWhenComplete(t *testing.T) {
	complete := strings.Replace(withImports,
		"    MODULE-IDENTITY, OBJECT-TYPE\n        FROM SNMPv2-SMI;",
		"    MODULE-IDENTITY, OBJECT-TYPE, Counter32, enterprises\n        FROM SNMPv2-SMI;", 1)
	if missing := CheckImports(complete, testCatalogue()); len(missing) != 0 {
		t.Errorf("a complete file reported %d missing imports: %+v", len(missing), missing)
	}
}

// The bundled MIBs are correct by construction. Reporting missing imports in
// them would prove the check is wrong.
func TestCheckImportsIsQuietOnACorrectModule(t *testing.T) {
	src := `X-MIB DEFINITIONS ::= BEGIN
IMPORTS
    OBJECT-TYPE, Integer32
        FROM SNMPv2-SMI;
x OBJECT-TYPE
    SYNTAX Integer32
    MAX-ACCESS read-only
    STATUS current
    DESCRIPTION "x"
    ::= { 1 }
END
`
	if missing := CheckImports(src, testCatalogue()); len(missing) != 0 {
		t.Errorf("false positives: %+v", missing)
	}
}

// A file that does not parse has bigger problems; guessing at its imports
// would pile confusing advice on top of a real error.
func TestCheckImportsSkipsAnUnparseableFile(t *testing.T) {
	if missing := CheckImports("BROKEN {{{ not a mib", testCatalogue()); missing != nil {
		t.Errorf("advice was offered on a file that does not parse: %+v", missing)
	}
}

// The fix must add the names to the existing clause and leave the rest of the
// file alone — a MIB carries comments and alignment no printer would preserve.
func TestFixImportsExtendsAnExistingClause(t *testing.T) {
	fix := FixImports(withImports, testCatalogue())
	if fix.Content == "" {
		t.Fatal("no fixed content returned")
	}
	if strings.Count(fix.Content, "IMPORTS") != 1 {
		t.Errorf("a second IMPORTS block was created:\n%s", fix.Content)
	}
	if !strings.Contains(fix.Content, "Counter32") || !strings.Contains(fix.Content, "enterprises") {
		t.Errorf("the missing symbols were not added:\n%s", fix.Content)
	}
	if !strings.Contains(fix.Content, "FROM SNMPv2-SMI;") {
		t.Errorf("the clause is not terminated:\n%s", fix.Content)
	}
	if strings.Contains(fix.Content, ";;") {
		t.Errorf("the semicolon was doubled:\n%s", fix.Content)
	}
	// The body must be untouched.
	if !strings.Contains(fix.Content, `"A counter. This description mentions DisplayString on purpose."`) {
		t.Error("the fix rewrote the body")
	}
	// And the result must now be complete.
	if again := CheckImports(fix.Content, testCatalogue()); len(again) != 0 {
		t.Errorf("the fix did not resolve everything: %+v", again)
	}
}

// A MIB with no IMPORTS at all must get one, placed after BEGIN.
func TestFixImportsCreatesAClauseWhenThereIsNone(t *testing.T) {
	src := `ACME-MIB DEFINITIONS ::= BEGIN

acme OBJECT-TYPE
    SYNTAX Counter32
    MAX-ACCESS read-only
    STATUS current
    DESCRIPTION "x"
    ::= { enterprises 1 }

END
`
	fix := FixImports(src, testCatalogue())
	if !strings.Contains(fix.Content, "IMPORTS") {
		t.Fatalf("no IMPORTS clause was created:\n%s", fix.Content)
	}
	beginAt := strings.Index(fix.Content, "BEGIN")
	importsAt := strings.Index(fix.Content, "IMPORTS")
	if importsAt < beginAt {
		t.Error("IMPORTS was placed before BEGIN")
	}
	if again := CheckImports(fix.Content, testCatalogue()); len(again) != 0 {
		t.Errorf("still incomplete after the fix: %+v", again)
	}
}

func TestFixImportsDoesNothingWhenNothingIsMissing(t *testing.T) {
	fix := FixImports(`X-MIB DEFINITIONS ::= BEGIN
IMPORTS
    OBJECT-TYPE FROM SNMPv2-SMI;
END
`, testCatalogue())
	if fix.Content != "" {
		t.Error("a complete file was rewritten anyway")
	}
	if len(fix.Missing) != 0 {
		t.Errorf("missing = %+v", fix.Missing)
	}
}

// Comments and strings must be skipped, or every English word in a MIB becomes
// a symbol reference.
func TestScanIdentifiersSkipsCommentsAndStrings(t *testing.T) {
	got := scanIdentifiers(`-- Counter32 in a comment
"Integer32 in a string"
realSymbol
`)
	if _, found := got["Counter32"]; found {
		t.Error("an identifier inside a comment was collected")
	}
	if _, found := got["Integer32"]; found {
		t.Error("an identifier inside a string was collected")
	}
	if pos, found := got["realSymbol"]; !found || pos[0] != 3 {
		t.Errorf("realSymbol = %v", got["realSymbol"])
	}
}

func TestFirstLineTrims(t *testing.T) {
	if got := firstLine("  one\ntwo  "); got != "one" {
		t.Errorf("firstLine = %q", got)
	}
	if got := firstLine(strings.Repeat("x", 200)); len(got) > 130 {
		t.Errorf("not truncated: %d chars", len(got))
	}
}
