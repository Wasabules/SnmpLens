package mib

import (
	"testing"

	"github.com/sleepinggenius2/gosmi"
)

// Status, Units and Parent reach the tree.
//
// `NodeDetails.svelte` has rendered all three since it was written, each behind
// an `{#if}`, and this struct carried none of them — so the three rows never
// appeared and nobody could see that they never appeared. gosmi has had the
// data all along: `Node.Status` and `Type.Units`.
//
// Status is the one that matters. SMI marks an object `deprecated` or
// `obsolete`, and a browser that does not say so lets someone build monitoring
// on an OID the vendor has withdrawn.
func TestTreeCarriesStatusUnitsAndParent(t *testing.T) {
	// Exit THEN Init, and SetPath rather than AppendPath: Init alone finds the
	// existing handle with every previously loaded module still in it, and
	// AppendPath accumulates directories.
	gosmi.Exit()
	gosmi.Init()
	gosmi.SetPath("../../mibs")
	roots, err := NewService("../../mibs").LoadAll()
	if err != nil {
		t.Skipf("could not load the bundled MIBs: %v", err)
	}
	if len(roots) == 0 {
		t.Fatal("no tree was built")
	}

	byName := map[string]*Node{}
	var walk func(*Node)
	walk = func(n *Node) {
		byName[n.Name] = n
		for _, c := range n.Children {
			walk(c)
		}
	}
	for _, r := range roots {
		walk(r)
	}

	// UNITS "seconds" in IP-MIB. A concrete object rather than "some node has
	// units", so the test says what it checked.
	if n := byName["ipReasmTimeout"]; n == nil {
		t.Error("ipReasmTimeout is not in the tree")
	} else if n.Units != "seconds" {
		t.Errorf("ipReasmTimeout units = %q, want \"seconds\"", n.Units)
	}

	// Every node the standard MIBs declare carries a STATUS clause.
	if n := byName["ifDescr"]; n == nil {
		t.Error("ifDescr is not in the tree")
	} else {
		if n.Status == "" {
			t.Error("ifDescr has no status; the SMI declares one for every object")
		}
		if n.Status == "Unknown" {
			t.Error("ifDescr reports gosmi's zero value as though the MIB declared it")
		}
		if n.Parent != "ifEntry" {
			t.Errorf("ifDescr parent = %q, want \"ifEntry\"", n.Parent)
		}
	}

	// A root has no parent, and must not claim one.
	for _, r := range roots {
		if r.Parent != "" {
			t.Errorf("root %s claims parent %q", r.Name, r.Parent)
		}
	}

	// The status the interface will show has to be a word, not a number: it is
	// rendered straight into the panel.
	withStatus := 0
	for _, n := range byName {
		if n.Status == "" {
			continue
		}
		withStatus++
		switch n.Status {
		case "Current", "Deprecated", "Obsolete", "Mandatory", "Optional":
		default:
			t.Errorf("%s has status %q, which is not an SMI status", n.Name, n.Status)
		}
	}
	if withStatus == 0 {
		t.Error("not one node in the corpus carries a status")
	}
	t.Logf("%d of %d nodes carry a status", withStatus, len(byName))
}
