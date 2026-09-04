package mib

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sleepinggenius2/gosmi"
)

// corpusCopy puts the real bundled MIBs in a temp directory so a test can
// break one of them.
func corpusCopy(t *testing.T) (string, []string) {
	t.Helper()
	entries, err := os.ReadDir("../../mibs")
	if err != nil {
		t.Skip("no corpus")
	}
	dir := t.TempDir()
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join("../../mibs", e.Name()))
		if err != nil {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, e.Name()), raw, 0o600); err != nil {
			t.Fatal(err)
		}
		names = append(names, e.Name())
	}
	return dir, names
}

func loadInto(t *testing.T, dir string, names []string) MibLoadResponse {
	t.Helper()
	gosmi.Exit()
	gosmi.Init()
	gosmi.SetPath(dir)
	return NewService(dir).LoadWithDiagnostics(names)
}

// The health probe must not call a destroyed tree healthy.
//
// Saving a SNMPv2-SMI that is valid SMI and defines nothing is the worst case:
// it produces no diagnostic anywhere, because it parses. gosmi then resolves
// imports lazily and returns nil rather than failing, so every dependent
// module still "loads" and its objects still exist BY NAME — with an empty
// OID. Measured before the fix: 0 tree roots, Translate answering "iso", and
// this function reporting ok=true with 15 modules, so the editor showed a
// green "reloaded 15 modules" toast over a tree that no longer resolved
// anything.
func TestHealthProbeCatchesAGuttedCoreModule(t *testing.T) {
	dir, names := corpusCopy(t)

	if h := healthAfter(t, dir, names); !h.Ok {
		t.Skipf("the control is already unhealthy: %v", h.Failures)
	}

	for _, broken := range []struct {
		name, body string
	}{
		{"SNMPv2-SMI", "SNMPv2-SMI DEFINITIONS ::= BEGIN\nEND\n"},
		{"SNMPv2-SMI", ""},
		{"SNMPv2-TC", "SNMPv2-TC DEFINITIONS ::= BEGIN\nEND\n"},
	} {
		t.Run(broken.name+"/"+shortDesc(broken.body), func(t *testing.T) {
			dir, names := corpusCopy(t)
			if err := os.WriteFile(filepath.Join(dir, broken.name), []byte(broken.body), 0o600); err != nil {
				t.Fatal(err)
			}

			resp := loadInto(t, dir, names)
			h := checkHealth()

			if len(resp.Tree) == 0 && h.Ok {
				t.Errorf("the tree is empty and the probe says ok=true (modules=%d)", h.Modules)
			}
			if !h.Ok && len(h.Failures) == 0 {
				t.Error("not ok, but nothing named")
			}
			for _, f := range h.Failures {
				if !strings.Contains(f, "1.3.6.1.2.1") {
					t.Errorf("a failure that does not name the OID: %q", f)
				}
			}
		})
	}
}

// And it must not fire on a healthy corpus, or every save reports a problem.
func TestHealthProbeIsQuietOnTheRealCorpus(t *testing.T) {
	dir, names := corpusCopy(t)
	h := healthAfter(t, dir, names)
	if !h.Ok {
		t.Errorf("false failures on the bundled corpus: %v", h.Failures)
	}
	if h.Modules == 0 {
		t.Error("no modules loaded")
	}
}

// The name alone is not enough: an object can keep its name and lose its OID,
// which is exactly what a missing import does.
func TestHealthProbeComparesTheOidAndNotOnlyTheName(t *testing.T) {
	dir, names := corpusCopy(t)
	if err := os.WriteFile(filepath.Join(dir, "SNMPv2-SMI"),
		[]byte("SNMPv2-SMI DEFINITIONS ::= BEGIN\nEND\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	loadInto(t, dir, names)

	node, err := gosmi.GetNode("sysDescr")
	if err != nil || node.Name != "sysDescr" {
		t.Skipf("sysDescr did not survive by name (%v), so this case does not apply", err)
	}
	// It IS there by name, and its OID is gone. That is the whole point.
	if node.Oid.String() == "1.3.6.1.2.1.1.1" {
		t.Skip("the OID survived; the fixture no longer reproduces the case")
	}
	if h := checkHealth(); h.Ok {
		t.Error("the probe passed on a node that kept its name and lost its OID")
	}
}

func healthAfter(t *testing.T, dir string, names []string) Health {
	t.Helper()
	loadInto(t, dir, names)
	return checkHealth()
}

func shortDesc(body string) string {
	if body == "" {
		return "empty"
	}
	return "gutted"
}
