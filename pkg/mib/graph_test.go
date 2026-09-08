package mib

import (
	"os"
	"path/filepath"
	"testing"
)

func nodeOf(nodes []GraphNode, module string) *GraphNode {
	for i := range nodes {
		if nodes[i].Module == module {
			return &nodes[i]
		}
	}
	return nil
}

// The graph over the bundled corpus, which is the shape people actually have:
// a handful of standard modules that nearly everything imports from.
func TestTheGraphOfTheBundledCorpus(t *testing.T) {
	nodes, err := NewService("../../mibs").ModuleGraph()
	if err != nil {
		t.Skipf("no corpus: %v", err)
	}
	if len(nodes) < 10 {
		t.Fatalf("%d modules; the bundled set is fourteen files", len(nodes))
	}

	smi := nodeOf(nodes, "SNMPv2-SMI")
	if smi == nil {
		t.Fatal("SNMPv2-SMI is not in the graph")
	}
	if !smi.Present {
		t.Error("SNMPv2-SMI ships with the application and is reported as absent")
	}
	// It is what nearly everything imports from, which is exactly why the edge
	// read backwards is worth carrying.
	if len(smi.ImportedBy) < 5 {
		t.Errorf("SNMPv2-SMI is imported by %d modules: %v", len(smi.ImportedBy), smi.ImportedBy)
	}

	ifmib := nodeOf(nodes, "IF-MIB")
	if ifmib == nil {
		t.Fatal("IF-MIB is not in the graph")
	}
	if len(ifmib.Imports) == 0 {
		t.Error("IF-MIB imports nothing, which no real MIB does")
	}

	// Both directions describe the same edge.
	for _, n := range nodes {
		for _, dep := range n.Imports {
			d := nodeOf(nodes, dep)
			if d == nil {
				t.Errorf("%s imports %s, which is not a node", n.Module, dep)
				continue
			}
			found := false
			for _, back := range d.ImportedBy {
				if back == n.Module {
					found = true
				}
			}
			if !found {
				t.Errorf("%s imports %s and %s does not list it back", n.Module, dep, dep)
			}
		}
	}
}

// A module named in an IMPORTS clause with no file behind it is the whole point
// of drawing this: it is what somebody has to go and download. Leaving it out
// would draw a tree whose branches stop without saying why.
func TestAModuleThatIsOnlyImportedIsStillANode(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("ACME-POE-MIB", `ACME-POE-MIB DEFINITIONS ::= BEGIN
IMPORTS
    acmeProducts FROM ACME-SMI
    DisplayString FROM SNMPv2-TC;
END
`)
	write("ACME-SMI", `ACME-SMI DEFINITIONS ::= BEGIN
IMPORTS
    MODULE-IDENTITY FROM SNMPv2-SMI;
END
`)

	nodes, err := NewService(dir).ModuleGraph()
	if err != nil {
		t.Fatal(err)
	}

	poe := nodeOf(nodes, "ACME-POE-MIB")
	if poe == nil || !poe.Present {
		t.Fatalf("the file that exists is not present: %+v", poe)
	}
	if len(poe.Imports) != 2 {
		t.Errorf("ACME-POE-MIB imports %v", poe.Imports)
	}

	smi := nodeOf(nodes, "ACME-SMI")
	if smi == nil || !smi.Present {
		t.Fatalf("ACME-SMI is a file in the directory: %+v", smi)
	}

	// SNMPv2-TC is imported and has no file here.
	tc := nodeOf(nodes, "SNMPv2-TC")
	if tc == nil {
		t.Fatal("an imported module with no file was dropped from the graph")
	}
	if tc.Present {
		t.Error("SNMPv2-TC has no file in this directory and is reported as present")
	}
	if len(tc.ImportedBy) != 1 || tc.ImportedBy[0] != "ACME-POE-MIB" {
		t.Errorf("the missing module does not say who wanted it: %v", tc.ImportedBy)
	}
	if tc.File != "" {
		t.Errorf("a module with no file reported one: %q", tc.File)
	}
}

// gosmi looks a module up BY FILE NAME, so a file whose name does not match
// what it declares is invisible to everything that imports it. The graph keeps
// both, since that mismatch is the thing to notice.
func TestTheGraphKeepsTheFileAndTheModuleApart(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "vendor_power.txt"),
		[]byte("VENDOR-POWER-MIB DEFINITIONS ::= BEGIN\nEND\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	nodes, err := NewService(dir).ModuleGraph()
	if err != nil {
		t.Fatal(err)
	}
	n := nodeOf(nodes, "VENDOR-POWER-MIB")
	if n == nil {
		t.Fatal("the module it declares is not in the graph")
	}
	if n.File != "vendor_power.txt" {
		t.Errorf("File = %q; the file name is what gosmi searches by", n.File)
	}
}

// An empty directory is empty, not an error — and never nil, which crosses the
// bridge as null.
func TestAnEmptyDirectoryGraphsToNothing(t *testing.T) {
	nodes, err := NewService(t.TempDir()).ModuleGraph()
	if err != nil {
		t.Fatalf("an empty directory reported an error: %v", err)
	}
	if nodes == nil {
		t.Error("ModuleGraph returned nil")
	}
	if len(nodes) != 0 {
		t.Errorf("%d modules in an empty directory", len(nodes))
	}
}

// NOT TESTED HERE, deliberately: that the scan does not hold the exclusive
// gosmi lock across the directory.
//
// It does not — the lock is taken once at the end, for the IsLoaded pass — and
// the property matters, because this is offered on a folder that can hold four
// hundred files and holding the lock for that long stops every OID translation
// in every other tab. But it is not observable from outside: a competing
// goroutine blocks for the same total time whether the lock is taken at the
// start and held, or taken at the end. A test written for it would pass either
// way, which is worse than none.
