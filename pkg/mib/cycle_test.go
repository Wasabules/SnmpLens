package mib

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sleepinggenius2/gosmi"
)

// X and Y import a symbol FROM EACH OTHER, and use it — so gosmi's lazy
// resolution has to walk the loop rather than merely note the import.
//
// Measured before the guard: LoadWithDiagnostics never returned. The stack was
// internal.LoadModule -> BuildModule -> GetModule -> LoadModule, repeating,
// because a module is not registered until BuildModule finishes and the cycle
// re-enters before that. It hung holding the package write lock, so every
// other MIB operation in the app blocked behind it.
const cycleX = `X-MIB DEFINITIONS ::= BEGIN
IMPORTS OBJECT-TYPE, Integer32 FROM SNMPv2-SMI
        yRoot FROM Y-MIB;
xRoot OBJECT IDENTIFIER ::= { yRoot 1 }
xThing OBJECT-TYPE
    SYNTAX Integer32
    MAX-ACCESS read-only
    STATUS current
    DESCRIPTION "x"
    ::= { xRoot 1 }
END
`

const cycleY = `Y-MIB DEFINITIONS ::= BEGIN
IMPORTS OBJECT-TYPE, Integer32 FROM SNMPv2-SMI
        xRoot FROM X-MIB;
yRoot OBJECT IDENTIFIER ::= { xRoot 1 }
yThing OBJECT-TYPE
    SYNTAX Integer32
    MAX-ACCESS read-only
    STATUS current
    DESCRIPTION "y"
    ::= { yRoot 1 }
END
`

func cycleDir(t *testing.T, files map[string]string) *Service {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	gosmi.Exit()
	gosmi.Init()
	gosmi.SetPath(dir)
	return NewService(dir)
}

// The load must RETURN. Everything else about this test is secondary.
func TestLoadDoesNotHangOnAnImportCycle(t *testing.T) {
	s := cycleDir(t, map[string]string{
		"SNMPv2-SMI": smiStub,
		"X-MIB":      cycleX,
		"Y-MIB":      cycleY,
	})

	for _, list := range [][]string{
		{"X-MIB"},
		{"Y-MIB"},
		{"SNMPv2-SMI", "X-MIB", "Y-MIB"},
	} {
		done := make(chan MibLoadResponse, 1)
		go func(names []string) {
			defer func() {
				if r := recover(); r != nil {
					done <- MibLoadResponse{}
				}
			}()
			done <- s.LoadWithDiagnostics(names)
		}(list)

		select {
		case resp := <-done:
			var refused int
			for _, d := range resp.Diagnostics {
				if !d.Success && strings.Contains(d.Error, "cycle") {
					refused++
				}
			}
			if refused == 0 {
				t.Errorf("%v: nothing was refused for the cycle: %+v", list, resp.Diagnostics)
			}
		case <-time.After(20 * time.Second):
			t.Fatalf("%v: the load did not return — it hangs holding the package lock", list)
		}
	}
}

// The refusal has to say what to do, not just fail.
func TestCycleRefusalNamesTheLoop(t *testing.T) {
	s := cycleDir(t, map[string]string{
		"SNMPv2-SMI": smiStub,
		"X-MIB":      cycleX,
		"Y-MIB":      cycleY,
	})
	resp := s.LoadWithDiagnostics([]string{"X-MIB", "Y-MIB"})

	var found bool
	for _, d := range resp.Diagnostics {
		if d.Success {
			continue
		}
		if !strings.Contains(d.Error, "X-MIB") || !strings.Contains(d.Error, "Y-MIB") {
			continue
		}
		found = true
		if d.Diagnosis == nil || d.Diagnosis.Stage != StageImports {
			t.Errorf("no import-stage diagnosis attached: %+v", d.Diagnosis)
		}
		if d.Diagnosis != nil && len(d.Diagnosis.Hints) == 0 {
			t.Error("the refusal offers no way forward")
		}
	}
	if !found {
		t.Errorf("the refusal does not name both modules of the loop: %+v", resp.Diagnostics)
	}
}

// A module importing ITSELF is the degenerate case.
func TestSelfImportIsRefused(t *testing.T) {
	s := cycleDir(t, map[string]string{
		"SNMPv2-SMI": smiStub,
		"SELF-MIB": `SELF-MIB DEFINITIONS ::= BEGIN
IMPORTS OBJECT-TYPE, Integer32 FROM SNMPv2-SMI
        selfRoot FROM SELF-MIB;
selfThing OBJECT-TYPE
    SYNTAX Integer32
    MAX-ACCESS read-only
    STATUS current
    DESCRIPTION "self"
    ::= { selfRoot 1 }
END
`,
	})
	resp := s.LoadWithDiagnostics([]string{"SELF-MIB"})
	for _, d := range resp.Diagnostics {
		if d.FileName == "SELF-MIB" && !d.Success && strings.Contains(d.Error, "cycle") {
			return
		}
	}
	t.Errorf("a self-import was not refused: %+v", resp.Diagnostics)
}

// And the guard must not fire on the real corpus, which is a DAG.
func TestNoFalseCycleOnTheBundledCorpus(t *testing.T) {
	if _, err := os.Stat("../../mibs"); err != nil {
		t.Skip("no corpus")
	}
	gosmi.Exit()
	gosmi.Init()
	gosmi.SetPath("../../mibs")
	s := NewService("../../mibs")

	files, err := ListMibFiles("../../mibs")
	if err != nil {
		t.Fatal(err)
	}
	if cycles := s.findImportCycles(files); len(cycles) != 0 {
		t.Errorf("false cycles on the standard MIBs: %+v", cycles)
	}

	// And the corpus still loads, with a tree.
	resp := s.LoadWithDiagnostics(files)
	if len(resp.Tree) == 0 {
		t.Error("the corpus produced no tree")
	}
	for _, d := range resp.Diagnostics {
		if !d.Success && strings.Contains(d.Error, "cycle") {
			t.Errorf("%s was refused as a cycle: %s", d.FileName, d.Error)
		}
	}
}

// The scan reads only the IMPORTS clause, and reads it correctly.
func TestReadImports(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}

	cases := []struct {
		name    string
		body    string
		module  string
		imports []string
	}{
		{"multi-line", `A-MIB DEFINITIONS ::= BEGIN
IMPORTS
    OBJECT-TYPE, Integer32
        FROM SNMPv2-SMI
    DisplayString
        FROM SNMPv2-TC;
END
`, "A-MIB", []string{"SNMPv2-SMI", "SNMPv2-TC"}},

		{"single-line", "B-MIB DEFINITIONS ::= BEGIN\nIMPORTS x FROM C-MIB;\nEND\n",
			"B-MIB", []string{"C-MIB"}},

		{"none", "D-MIB DEFINITIONS ::= BEGIN\nEND\n", "D-MIB", nil},

		// A module named in a comment is not an import.
		{"commented", `E-MIB DEFINITIONS ::= BEGIN
IMPORTS
    x FROM REAL-MIB   -- was FROM GHOST-MIB
    ;
END
`, "E-MIB", []string{"REAL-MIB"}},

		// Text after the clause must not be scanned.
		{"after", `F-MIB DEFINITIONS ::= BEGIN
IMPORTS x FROM ONE-MIB;
foo OBJECT IDENTIFIER ::= { iso 1 }
-- FROM TWO-MIB
END
`, "F-MIB", []string{"ONE-MIB"}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			module, imports := readImports(write(c.name, c.body))
			if module != c.module {
				t.Errorf("module = %q, want %q", module, c.module)
			}
			if strings.Join(imports, ",") != strings.Join(c.imports, ",") {
				t.Errorf("imports = %v, want %v", imports, c.imports)
			}
		})
	}
}

// The graph walk itself, independent of any file.
func TestCyclesIn(t *testing.T) {
	cases := []struct {
		name  string
		graph map[string][]string
		want  int
	}{
		{"a DAG", map[string][]string{"a": {"b", "c"}, "b": {"c"}, "c": nil}, 0},
		{"a loop", map[string][]string{"a": {"b"}, "b": {"a"}}, 1},
		{"self", map[string][]string{"a": {"a"}}, 1},
		{"three", map[string][]string{"a": {"b"}, "b": {"c"}, "c": {"a"}}, 1},
		{"edge to nowhere", map[string][]string{"a": {"missing"}}, 0},
		{"two loops", map[string][]string{
			"a": {"b"}, "b": {"a"},
			"c": {"d"}, "d": {"c"},
		}, 2},
		{"a loop plus a tail", map[string][]string{
			"tail": {"a"}, "a": {"b"}, "b": {"a"},
		}, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := cyclesIn(c.graph)
			if len(got) != c.want {
				t.Errorf("%d cycles, want %d: %+v", len(got), c.want, got)
			}
		})
	}
}
