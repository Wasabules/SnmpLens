package mib

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sleepinggenius2/gosmi"
	"github.com/sleepinggenius2/gosmi/parser"
)

// FixImports is the one place the editor goes from "here is the problem" to
// "here is the repair", so a repair that breaks the file is worse than no
// button at all. Every case below broke a file that was correct.

func loadedCatalogue(t *testing.T) Catalogue {
	t.Helper()
	if _, err := os.Stat("../../mibs"); err != nil {
		t.Skip("no corpus")
	}
	gosmi.Exit()
	gosmi.Init()
	gosmi.SetPath("../../mibs")
	if _, err := NewService("../../mibs").LoadAll(); err != nil {
		t.Skipf("could not load the corpus: %v", err)
	}
	return Symbols()
}

// A correct MIB must report nothing missing. All four of these are bundled and
// load cleanly, and two of them were reported as broken.
func TestNoMissingImportsOnCorrectBundledMibs(t *testing.T) {
	cat := loadedCatalogue(t)

	for _, name := range []string{"SNMPv2-SMI", "SNMPv2-TC", "IANAifType-MIB", "IF-MIB", "IP-MIB", "TCP-MIB"} {
		raw, err := os.ReadFile(filepath.Join("../../mibs", name))
		if err != nil {
			continue
		}
		content, _ := NormaliseSource(raw)

		missing := CheckImports(content, cat)
		if len(missing) == 0 {
			continue
		}
		var described []string
		for _, m := range missing {
			described = append(described, m.Symbol+" from "+m.Module)
		}
		t.Errorf("%s: %d false missing imports: %v", name, len(missing), described)

		// And whatever the fix would do must at least still parse.
		if fix := FixImports(content, cat); fix.Content != "" && fix.Content != content {
			if _, err := parser.Parse(strings.NewReader(fix.Content)); err != nil {
				t.Errorf("%s stops parsing after the fix: %v", name, err)
			}
		}
	}
}

// gosmi exposes the ASN.1 roots under a pseudo-module. They are not
// importable, and writing "FROM <well-known>" does not parse — measured,
// `5:14: unexpected "<"`. Every MIB rooted at { iso 3 6 1 … } hit this,
// including the bundled SNMPv2-SMI.
func TestPseudoModulesAreNotOfferedAsImports(t *testing.T) {
	cat := loadedCatalogue(t)

	var pseudo []string
	for _, s := range cat.Symbols {
		if strings.ContainsAny(s.Module, "<>") {
			pseudo = append(pseudo, s.Name)
		}
	}
	if len(pseudo) == 0 {
		t.Skip("this gosmi does not expose a pseudo-module")
	}
	t.Logf("pseudo-module symbols: %v", pseudo)

	index := cat.index()
	for _, name := range pseudo {
		if module, ok := index[name]; ok {
			t.Errorf("%s is offered as importable from %q", name, module)
		}
	}
}

// `ip(126)` in an INTEGER enumeration is a named number, not a reference.
func TestEnumerationLabelsAreNotSymbolReferences(t *testing.T) {
	body := `E-MIB DEFINITIONS ::= BEGIN
IMPORTS TEXTUAL-CONVENTION FROM SNMPv2-TC;
Thing ::= TEXTUAL-CONVENTION
    STATUS current
    DESCRIPTION "an enumeration"
    SYNTAX INTEGER {
        other(1),
        ip (126),
        udp(8),
        tcp   ( 9 ),
        negative(-1)
    }
END
`
	ids := scanIdentifiers(body)
	for _, label := range []string{"ip", "udp", "tcp", "negative", "other"} {
		if _, found := ids[label]; found {
			t.Errorf("the enumeration label %q was scanned as a reference", label)
		}
	}
	// And an ordinary reference on the same shape of line still is one.
	if _, found := ids["TEXTUAL-CONVENTION"]; !found {
		t.Error("a real reference was skipped")
	}
}

func TestNamedNumberDetection(t *testing.T) {
	cases := map[string]bool{
		"ip(126)":     true,
		"ip (126)":    true,
		"ip\t( 9 )":   true,
		"x(-1)":       true,
		"x(+2)":       true,
		"f(x)":        false, // not digits
		"f()":         false,
		"f":           false,
		"f (":         false,
		"OBJECT-TYPE": false,
	}
	for line, want := range cases {
		// The identifier ends where the non-identifier characters begin.
		end := 0
		for end < len(line) && isIdentPart(line[end]) {
			end++
		}
		if got := namedNumberAt(line, end); got != want {
			t.Errorf("namedNumberAt(%q) = %v, want %v", line, got, want)
		}
	}
}

// The clause shapes insertImports has to survive. Matching by line shape
// missed the two commonest ones and produced a SECOND IMPORTS clause, which
// fails with `unexpected "IMPORTS"`.
func TestFixImportsHandlesEveryClauseShape(t *testing.T) {
	cat := loadedCatalogue(t)

	const body = `
a OBJECT-TYPE
    SYNTAX Counter32
    MAX-ACCESS read-only
    STATUS current
    DESCRIPTION "needs Counter32, which is not imported"
    ::= { enterprises 1 }
END
`
	cases := map[string]string{
		"single line":        "T-MIB DEFINITIONS ::= BEGIN\nIMPORTS OBJECT-TYPE FROM SNMPv2-SMI;\n" + body,
		"trailing comment":   "U-MIB DEFINITIONS ::= BEGIN\nIMPORTS\n    OBJECT-TYPE\n        FROM SNMPv2-SMI;   -- all we need\n" + body,
		"multi line":         "V-MIB DEFINITIONS ::= BEGIN\nIMPORTS\n    OBJECT-TYPE\n        FROM SNMPv2-SMI;\n" + body,
		"BEGIN in a comment": "-- This file BEGINs the ACME tree.\nW-MIB DEFINITIONS ::= BEGIN\nIMPORTS\n    OBJECT-TYPE\n        FROM SNMPv2-SMI;\n" + body,
		"no IMPORTS at all":  "X-MIB DEFINITIONS ::= BEGIN\n" + body,
	}

	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			fix := FixImports(src, cat)
			if fix.Content == "" || fix.Content == src {
				t.Skip("no fix offered for this fixture")
			}
			if n := strings.Count(fix.Content, "IMPORTS"); n != 1 {
				t.Errorf("%d IMPORTS clauses after the fix:\n%s", n, fix.Content)
			}
			if _, err := parser.Parse(strings.NewReader(fix.Content)); err != nil {
				t.Errorf("stops parsing after the fix: %v\n%s", err, fix.Content)
			}
			if !strings.Contains(fix.Content, "Counter32") {
				t.Errorf("the missing symbol was not added:\n%s", fix.Content)
			}
		})
	}
}

func TestContainsWord(t *testing.T) {
	cases := []struct {
		s, word string
		want    bool
	}{
		{"IMPORTS", "IMPORTS", true},
		{"IMPORTS OBJECT-TYPE FROM X;", "IMPORTS", true},
		{"    IMPORTS", "IMPORTS", true},
		{"REIMPORTS", "IMPORTS", false},
		{"IMPORTSX", "IMPORTS", false},
		{"X-MIB DEFINITIONS ::= BEGIN", "BEGIN", true},
		{"BEGINNING", "BEGIN", false},
		{"", "BEGIN", false},
	}
	for _, c := range cases {
		if got := containsWord(c.s, c.word); got != c.want {
			t.Errorf("containsWord(%q, %q) = %v, want %v", c.s, c.word, got, c.want)
		}
	}
}
