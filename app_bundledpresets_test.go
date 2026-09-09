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
		// A CEILING, not a count: a discovering preset polls whatever the
		// equipment has, and its estimate is the upper bound it agreed to. The
		// bar here is that even at that ceiling a shipped example is a
		// dashboard rather than a stress test — a full 96-port chassis is four
		// requests a round, and that is fine.
		const maxRoundsPerPoll = 8
		rounds := (c.OIDs + snmp.MaxVarbindsPerGet - 1) / snmp.MaxVarbindsPerGet
		if rounds > maxRoundsPerPoll {
			t.Errorf("%s polls at most %d OIDs, %d requests a round", e.Name(), c.OIDs, rounds)
		}
		if c.Discovered > 0 && !strings.Contains(strings.ToLower(p.Description), "discover") {
			t.Errorf("%s discovers its instances and its description does not say so", e.Name())
		}
		// A band that is evaluated raises an incident, which the operator's own
		// rules may route to a sink. Binding one of these is a decision, and it
		// is made from the description in the library list — where the cost
		// screen has not been opened yet.
		if c.Alerting > 0 && !strings.Contains(strings.ToLower(p.Description), "watch") {
			t.Errorf("%s arms %d incident(s) and its description does not say so", e.Name(), c.Alerting)
		}

		// Every shipped preset ARRANGES itself. These are the files somebody
		// copies to write their own, so a library where none of them places a
		// widget teaches that a preset cannot — and the reflowing fallback
		// looks identical to a layout that failed to load.
		if !preset.HasLayout(p) {
			t.Errorf("%s places none of its widgets", e.Name())
			continue
		}
		for i, w := range p.Widgets {
			if w.Layout == nil {
				t.Errorf("%s: widget %d (%q) is left to fall where it may", e.Name(), i, w.Title)
			}
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

	// A first run. The directory existing is not what decides this — see
	// TestAnEmptyDirectoryStillGetsTheExamples for why it cannot be.
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

	// Deleting one and restarting must leave it deleted: the marker is what
	// says the examples have been offered, and it survives the deletion.
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

	// And deleting the whole directory is how somebody asks for them back: the
	// marker goes with it.
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	a.ensureBundledPresets()
	if again := a.ListPresets(); len(again) != len(first) {
		t.Errorf("%d preset(s) after deleting the directory, want %d", len(again), len(first))
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

// The directory existing is NOT the same as the library existing.
//
// presetDir() creates the directory as a SIDE EFFECT and is called by
// ListPresets, so opening the preset settings once — on any earlier version —
// was enough to make every later startup skip the extraction. An installation
// that had been running for weeks got no examples at all, which is exactly what
// happened. The marker is what "already offered" means now.
func TestAnEmptyDirectoryStillGetsTheExamples(t *testing.T) {
	a, dir := newPresetApp(t) // newPresetApp calls presetDir(), which creates it
	a.presets = presets

	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("setup: the directory should already exist: %v", err)
	}
	a.ensureBundledPresets()

	list := a.ListPresets()
	if len(list) < 3 {
		t.Fatalf("an empty-but-existing directory got %d preset(s)", len(list))
	}
	// The marker is a dotfile, so it is not one of them.
	for _, p := range list {
		if strings.HasPrefix(p.File, ".") {
			t.Errorf("the marker is listed as a preset: %s", p.File)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, extractedMarker)); err != nil {
		t.Errorf("nothing recorded that the extraction ran: %v", err)
	}
}

// A library somebody has already curated is left alone, and the marker is
// written anyway so the examples do not land on top of it later.
func TestAnExistingLibraryIsNotOverwritten(t *testing.T) {
	a, dir := newPresetApp(t)
	a.presets = presets
	a.importSinglePreset(writeSrc(t, t.TempDir(), "mine.json", goodPreset))

	a.ensureBundledPresets()

	list := a.ListPresets()
	if len(list) != 1 || list[0].File != "mine.json" {
		t.Fatalf("a curated library was added to: %+v", list)
	}
	if _, err := os.Stat(filepath.Join(dir, extractedMarker)); err != nil {
		t.Error("the marker was not written, so the examples would arrive on a later start")
	}

	// And a second start changes nothing.
	a.ensureBundledPresets()
	if list := a.ListPresets(); len(list) != 1 {
		t.Errorf("%d preset(s) after a second start", len(list))
	}
}
