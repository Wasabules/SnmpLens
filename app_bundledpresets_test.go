package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"SnmpLens/pkg/preset"
	"SnmpLens/pkg/snmp"
)

// Every preset that ships must be bindable.
//
// The bar `pkg/mib`'s Analyse holds over the fourteen bundled MIBs, for the same
// reason: a shipped file with problems is listed WITH them and cannot be bound,
// so it would be an example that demonstrates the error list rather than the
// feature. And nothing else would say so — it imports, it lists, it simply has
// a number beside it.
func TestEveryBundledPresetIsValid(t *testing.T) {
	entries, err := os.ReadDir("presets")
	if err != nil {
		t.Fatalf("no bundled presets: %v", err)
	}
	if len(entries) < 3 {
		t.Fatalf("%d bundled preset(s); the library ships as the feature's first impression", len(entries))
	}

	names := map[string]string{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		p, errs, err := preset.LoadFile(filepath.Join("presets", e.Name()))
		if err != nil {
			t.Errorf("%s: %v", e.Name(), err)
			continue
		}
		for _, ve := range errs {
			t.Errorf("%s: %s %s %v", e.Name(), ve.Field, ve.Message, ve.Args)
		}
		if len(errs) > 0 {
			continue
		}

		// A name is what the picker shows, so two files with the same one are
		// two rows nobody can tell apart.
		if other, clash := names[p.Name]; clash {
			t.Errorf("%s and %s are both called %q", e.Name(), other, p.Name)
		}
		names[p.Name] = e.Name()

		if strings.TrimSpace(p.Description) == "" {
			t.Errorf("%s has no description; the library is a list of names without one", e.Name())
		}
		if p.Author == "" {
			t.Errorf("%s has no author", e.Name())
		}

		// The cost has to be honest before it is shown, and these are the files
		// most people will bind first.
		c := presetCost(p)
		if c.OIDs == 0 || c.VarbindsPerDay == 0 {
			t.Errorf("%s costs nothing: %+v", e.Name(), c)
		}
		// One round in one request keeps the example cheap to reason about.
		if c.OIDs > snmp.MaxVarbindsPerGet*2 {
			t.Errorf("%s polls %d OIDs, more than two requests a round", e.Name(), c.OIDs)
		}
	}
}

// The examples are extracted on FIRST RUN ONLY, and that is the difference from
// the bundled MIBs.
//
// ensureStandardMibs restores an absent MIB on every startup because nearly
// every other MIB imports from those three. Nothing depends on a preset, and
// restoring one the operator deleted would be the application arguing with
// them, once per restart, forever.
func TestBundledPresetsAreExtractedOnceAndNotRestored(t *testing.T) {
	a, dir := newPresetApp(t)
	a.presets = presets

	// newPresetApp already created the directory, which is what a second run
	// looks like: nothing is written.
	a.ensureBundledPresets()
	if list := a.ListPresets(); len(list) != 0 {
		t.Fatalf("an existing library was populated anyway: %+v", list)
	}

	// A first run: the directory does not exist yet.
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	a.ensureBundledPresets()
	first := a.ListPresets()
	if len(first) < 3 {
		t.Fatalf("a first run extracted %d preset(s)", len(first))
	}
	for _, p := range first {
		if p.Problems != 0 {
			t.Errorf("%s shipped with %d problem(s)", p.File, p.Problems)
		}
	}

	// Deleting one and restarting must leave it deleted.
	if err := a.DeletePreset(first[0].File); err != nil {
		t.Fatal(err)
	}
	a.ensureBundledPresets()
	after := a.ListPresets()
	if len(after) != len(first)-1 {
		t.Errorf("a deleted example came back: %d entries, was %d", len(after), len(first))
	}
	for _, p := range after {
		if p.File == first[0].File {
			t.Errorf("%s was restored after being deleted", p.File)
		}
	}
}

// One of them names a vendor, so the matching feature has something to match on
// out of the box — otherwise Detect always answers "no preset names this
// equipment" on a fresh installation, which reads as a broken feature.
func TestAtLeastOneBundledPresetClaimsADevice(t *testing.T) {
	list, err := preset.List("presets")
	if err != nil {
		t.Fatal(err)
	}
	claiming := 0
	for _, in := range list {
		if len(in.SysObjectIDPrefix) > 0 {
			claiming++
		}
	}
	if claiming == 0 {
		t.Error("no bundled preset names a device; Detect can never match anything on a fresh install")
	}

	// And it really matches what it says it does.
	if _, matched := rankForDevice(list, "1.3.6.1.4.1.9.1.2494"); matched == 0 {
		t.Error("nothing matches a Cisco sysObjectID")
	}
	if _, matched := rankForDevice(list, "1.3.6.1.4.1.911.1"); matched != 0 {
		t.Error("a Cisco preset matched enterprise 911; the prefix is being compared as text")
	}
}
