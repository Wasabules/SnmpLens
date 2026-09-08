package mib

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sleepinggenius2/gosmi"
)

// A world with the bundled MIBs in it, and nothing left behind for the next
// test: gosmi's state is global.
func withBundled(t *testing.T) {
	t.Helper()
	gosmi.Exit()
	gosmi.Init()
	gosmi.SetPath("../../mibs")
	if _, err := NewService("../../mibs").LoadAll(); err != nil {
		t.Skipf("could not load the corpus: %v", err)
	}
	t.Cleanup(func() {
		gosmi.Exit()
		gosmi.Init()
	})
}

// The cache exists because the catalogue is expensive and is asked for on every
// pause in typing. Measured on a corpus the size a vendor folder really is —
// 135 modules, 24 227 symbols — one call cost 113 ms, all of it holding the
// EXCLUSIVE gosmi mutex, so every OID translation in every other tab waited.
func TestTheCatalogueIsBuiltOnceUntilSomethingChanges(t *testing.T) {
	withBundled(t)

	first := Symbols()
	if len(first.Symbols) == 0 {
		t.Fatal("the corpus produced no symbols")
	}

	// The second call must not rebuild. Timing is the only observable
	// difference, so this asserts an order of magnitude rather than a number:
	// building is thousands of map writes and slice appends, reading a cached
	// pointer is neither.
	start := time.Now()
	for i := 0; i < 50; i++ {
		Symbols()
	}
	cached := time.Since(start) / 50

	gosmiMu.Lock()
	invalidateCatalogue()
	gosmiMu.Unlock()

	start = time.Now()
	Symbols()
	built := time.Since(start)

	if cached*4 > built {
		t.Errorf("a cached call costs %v and building costs %v; the cache is not being used",
			cached.Round(time.Microsecond), built.Round(time.Microsecond))
	}
	t.Logf("cached %v vs built %v", cached.Round(time.Microsecond), built.Round(time.Microsecond))
}

// The cache must never outlive what it describes.
//
// A stale catalogue does not look like a cache problem: the editor reports an
// import as MISSING that has just been satisfied, and offers completions for
// names that no longer exist. That reads as a broken file.
func TestLoadingAMibDropsTheCache(t *testing.T) {
	dir := t.TempDir()

	// Start with the core modules only.
	for _, name := range []string{"SNMPv2-SMI", "SNMPv2-TC"} {
		raw, err := os.ReadFile(filepath.Join("../../mibs", name))
		if err != nil {
			t.Skip("no corpus")
		}
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	gosmi.Exit()
	gosmi.Init()
	gosmi.SetPath(dir)
	t.Cleanup(func() {
		gosmi.Exit()
		gosmi.Init()
	})

	svc := NewService(dir)
	if _, err := svc.LoadAll(); err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	before := Symbols()
	if hasModule(before, "IF-MIB") {
		t.Fatal("setup: IF-MIB is already loaded")
	}

	// Now add one and load it.
	raw, err := os.ReadFile(filepath.Join("../../mibs", "IF-MIB"))
	if err != nil {
		t.Skip("no IF-MIB in the corpus")
	}
	if err := os.WriteFile(filepath.Join(dir, "IF-MIB"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.LoadSpecific([]string{"IF-MIB"}); err != nil {
		t.Fatalf("LoadSpecific: %v", err)
	}

	after := Symbols()
	if !hasModule(after, "IF-MIB") {
		t.Fatal("the catalogue still describes the world from before the load")
	}
	if len(after.Symbols) <= len(before.Symbols) {
		t.Errorf("%d symbols before, %d after: the cache was not dropped",
			len(before.Symbols), len(after.Symbols))
	}

	// And the check that actually breaks for a user: an import that is now
	// satisfiable must stop being reported as missing.
	src := "ACME-MIB DEFINITIONS ::= BEGIN\nIMPORTS ifIndex FROM IF-MIB;\nEND\n"
	for _, m := range CheckImports(src, after) {
		if m.Module == "IF-MIB" {
			t.Error("IF-MIB is loaded and is still reported as a missing import")
		}
	}
}

// Rebuild replaces the world entirely, so nothing cached about it survives.
func TestRebuildDropsTheCache(t *testing.T) {
	withBundled(t)

	before := Symbols()
	if len(before.Symbols) == 0 {
		t.Fatal("setup: no symbols")
	}

	// Rebuild with nothing but the core modules: the catalogue must shrink.
	svc := NewService("../../mibs")
	svc.Rebuild([]string{"SNMPv2-SMI", "SNMPv2-TC"})

	after := Symbols()
	if len(after.Symbols) >= len(before.Symbols) {
		t.Errorf("%d symbols before the rebuild, %d after; the cache survived it",
			len(before.Symbols), len(after.Symbols))
	}
	if hasModule(after, "IF-MIB") {
		t.Error("a module the rebuild did not load is still in the catalogue")
	}
}

// The cache is shared, so a caller that appends to what it got back must not be
// able to write into it.
func TestACallerCannotGrowIntoTheCache(t *testing.T) {
	withBundled(t)

	got := Symbols()
	n := len(got.Symbols)
	got.Symbols = append(got.Symbols, Symbol{Name: "INJECTED", Module: "nowhere", Kind: "node"})
	got.Modules = append(got.Modules, "INJECTED-MIB")

	again := Symbols()
	if len(again.Symbols) != n {
		t.Errorf("the catalogue grew to %d after a caller appended to its copy", len(again.Symbols))
	}
	for _, s := range again.Symbols {
		if s.Name == "INJECTED" {
			t.Fatal("a caller's append reached the cache")
		}
	}
	for _, m := range again.Modules {
		if m == "INJECTED-MIB" {
			t.Fatal("a caller's append reached the cached module list")
		}
	}
}

func hasModule(c Catalogue, name string) bool {
	for _, m := range c.Modules {
		if strings.EqualFold(m, name) {
			return true
		}
	}
	return false
}
