package preset

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func marshal(t *testing.T, p Preset) string {
	t.Helper()
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// A preset survives the round trip it will actually make: written as JSON by
// whoever authored it, read back here.
//
// Match carries `json:",omitzero"`, so a preset that names no device is
// marshalled WITHOUT the key at all. That is the shape most community files
// will have, and it must still validate.
func TestAPresetRoundTripsThroughJson(t *testing.T) {
	dir := t.TempDir()

	full := valid()
	back, errs := Parse([]byte(marshal(t, full)))
	if len(errs) != 0 {
		t.Fatalf("a valid preset did not survive JSON: %s", fieldsOf(errs))
	}
	if len(back.Widgets) != len(full.Widgets) || back.IntervalSec != full.IntervalSec {
		t.Errorf("the preset changed shape: %+v", back)
	}
	if len(PollOIDs(back)) != len(PollOIDs(full)) {
		t.Error("the OIDs did not survive")
	}

	bare := valid()
	bare.Match = Match{}
	raw := marshal(t, bare)
	if strings.Contains(raw, "\"match\"") {
		t.Errorf("a zero Match was marshalled anyway: %s", raw)
	}
	if _, errs := Parse([]byte(raw)); len(errs) != 0 {
		t.Errorf("a preset naming no device was refused: %s", fieldsOf(errs))
	}

	// And from disk, which is the only way one ever really arrives.
	p := writeFile(t, dir, "acme.json", raw)
	if _, errs, err := LoadFile(p); err != nil || len(errs) != 0 {
		t.Errorf("LoadFile: err=%v errs=%s", err, fieldsOf(errs))
	}
}

// The three answers are three different things, and collapsing any two of them
// reports the wrong problem to whoever has to fix it.
func TestReadFailureAndFormatFailureAreNotTheSameAnswer(t *testing.T) {
	dir := t.TempDir()

	if _, _, err := LoadFile(filepath.Join(dir, "nope.json")); err == nil {
		t.Error("a missing file was not reported as a read failure")
	}
	if _, _, err := LoadFile(dir); err == nil {
		t.Error("a directory was accepted as a preset")
	}

	// Not JSON at all: one error, no field path, and not a raw parser message
	// sitting in a column that renders field names.
	notJSON := writeFile(t, dir, "page.json", "<!DOCTYPE html><html><body>404</body></html>")
	_, errs, err := LoadFile(notJSON)
	if err != nil {
		t.Fatalf("a readable file reported a read failure: %v", err)
	}
	if len(errs) != 1 || errs[0].Field != "" || errs[0].Message != "preset.err.notJson" {
		t.Fatalf("an HTML page was reported as %+v", errs)
	}

	// JSON, but not a preset: every problem at once, with field paths.
	empty := writeFile(t, dir, "empty.json", "{}")
	if _, errs, _ := LoadFile(empty); !has(errs, "name") || !has(errs, "widgets") || !has(errs, "formatVersion") {
		t.Errorf("an empty object was reported as %s", fieldsOf(errs))
	}
}

// The size bound is checked before the read, so the file that is a video does
// not get loaded into memory in order to be refused.
func TestAnOversizedFileIsRefusedWithoutBeingRead(t *testing.T) {
	dir := t.TempDir()
	big := writeFile(t, dir, "big.json", strings.Repeat("x", MaxFileBytes+1))
	_, errs, err := LoadFile(big)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if len(errs) != 1 || errs[0].Message != "preset.err.tooBig" {
		t.Fatalf("an oversized file was reported as %+v", errs)
	}
}

// A folder of files from strangers has a bad one in it. A listing that fails
// entirely is a listing nobody can use to find which.
func TestOneBadFileDoesNotHideTheGoodOnes(t *testing.T) {
	dir := t.TempDir()
	good := valid()
	good.Name = "Alpha"
	writeFile(t, dir, "alpha.json", marshal(t, good))
	good.Name = "Zulu"
	writeFile(t, dir, "zulu.json", marshal(t, good))
	writeFile(t, dir, "broken.json", "{\"formatVersion\": 1}")
	writeFile(t, dir, "notjson.txt", "hello")
	writeFile(t, dir, ".hidden", marshal(t, good))
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}

	list, err := List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 4 {
		t.Fatalf("%d entries, want 4 (two good, two broken; the dotfile and the directory skipped): %+v", len(list), list)
	}
	// Sorted by name, and a broken file still appears — with its count.
	byFile := map[string]Info{}
	for _, in := range list {
		byFile[in.File] = in
	}
	if byFile["alpha.json"].Name != "Alpha" || byFile["alpha.json"].Problems != 0 {
		t.Errorf("a good preset listed as %+v", byFile["alpha.json"])
	}
	if byFile["broken.json"].Problems == 0 {
		t.Error("a broken preset listed as having no problems")
	}
	if byFile["notjson.txt"].Problems == 0 {
		t.Error("a file that is not JSON listed as having no problems")
	}
	if byFile["alpha.json"].OIDs != 4 || byFile["alpha.json"].Widgets != 3 {
		t.Errorf("the listing does not carry what it takes to choose: %+v", byFile["alpha.json"])
	}
}

// A directory nobody has put a preset in yet is empty, not broken — and it is
// the state the feature ships in.
func TestAnAbsentDirectoryListsEmpty(t *testing.T) {
	list, err := List(filepath.Join(t.TempDir(), "presets"))
	if err != nil {
		t.Fatalf("an absent directory reported an error: %v", err)
	}
	if list == nil {
		t.Error("List returned nil, which crosses the bridge as null and throws on .map")
	}
	if len(list) != 0 {
		t.Errorf("%d entries from nothing", len(list))
	}
}

// The match is arc by arc, never as text.
//
// 1.3.6.1.4.1.9 is Cisco. 1.3.6.1.4.1.911 is somebody else. strings.HasPrefix
// is the obvious implementation and says they are the same vendor.
func TestAPrefixMatchesOnSubIdentifierBoundaries(t *testing.T) {
	const cisco = "1.3.6.1.4.1.9"
	yes := []string{"1.3.6.1.4.1.9", "1.3.6.1.4.1.9.1.2494", ".1.3.6.1.4.1.9.1.1"}
	no := []string{"1.3.6.1.4.1.911", "1.3.6.1.4.1.911.1", "1.3.6.1.4.1.90", "1.3.6.1.4.1", ""}

	for _, oid := range yes {
		if MatchDepth([]string{cisco}, oid) == 0 {
			t.Errorf("%q should match %q", oid, cisco)
		}
	}
	for _, oid := range no {
		if d := MatchDepth([]string{cisco}, oid); d != 0 {
			t.Errorf("%q matched %q at depth %d", oid, cisco, d)
		}
	}

	// The leading dot is not part of the OID on either side: formatSnmpValue
	// renders an ObjectIdentifier with one, and a file is written by hand.
	if MatchDepth([]string{".1.3.6.1.4.1.9"}, "1.3.6.1.4.1.9.1") == 0 {
		t.Error("a prefix written with a leading dot did not match")
	}

	// A preset that names no device claims none.
	if Matches(valid(), "") || Matches(Preset{}, "1.3.6.1.4.1.9") {
		t.Error("a preset with no prefixes claimed a device")
	}
}

// Ranking ORDERS, it never filters: Match is advice to the person binding, and
// hiding the rest would turn advice into a decision.
func TestRankingPutsTheMostSpecificFirstAndKeepsEverything(t *testing.T) {
	infos := []Info{
		{File: "zzz-generic.json", Name: "Generic interfaces"},
		{File: "cisco.json", Name: "Cisco", SysObjectIDPrefix: []string{"1.3.6.1.4.1.9"}},
		{File: "cat9k.json", Name: "Catalyst 9300", SysObjectIDPrefix: []string{"1.3.6.1.4.1.9.1.2494"}},
		{File: "juniper.json", Name: "Juniper", SysObjectIDPrefix: []string{"1.3.6.1.4.1.2636"}},
	}
	got := Rank(infos, "1.3.6.1.4.1.9.1.2494")

	if len(got) != len(infos) {
		t.Fatalf("ranking dropped %d entries", len(infos)-len(got))
	}
	if got[0].File != "cat9k.json" {
		t.Errorf("first is %q; the most specific match should lead", got[0].File)
	}
	if got[1].File != "cisco.json" {
		t.Errorf("second is %q; the vendor match should follow the model match", got[1].File)
	}
	// The rest keep a stable, name-ordered place rather than an arbitrary one.
	if got[2].Name != "Generic interfaces" || got[3].Name != "Juniper" {
		t.Errorf("the non-matching tail is not name-ordered: %q then %q", got[2].Name, got[3].Name)
	}

	// And the input is not reordered under the caller.
	if infos[0].File != "zzz-generic.json" {
		t.Error("Rank sorted its argument in place")
	}
}
