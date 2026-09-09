package main

import (
	"bytes"
	"image"
	"image/gif"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An app with a configuration directory and nothing else: none of this touches
// storage, the scheduler or the network.
func newAssetApp(t *testing.T) (*App, string) {
	t.Helper()
	dir := t.TempDir()
	mibDir := filepath.Join(dir, "SnmpLens", "mibs")
	if err := os.MkdirAll(mibDir, 0o755); err != nil {
		t.Fatal(err)
	}
	a := &App{persistentMibDir: mibDir}
	assetDir, err := a.assetDir()
	if err != nil {
		t.Fatal(err)
	}
	return a, assetDir
}

func writePNG(t *testing.T, path string, w, h int) {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

// The background directory is a SIBLING of mibs/, never a child.
//
// ListMibFiles feeds what it finds in the MIB directory to gosmi: a PNG in
// there is loaded as a module and reported as a broken MIB, in a list of two
// hundred real ones. mib-backups/, mib-drafts/, mib-temp/ and presets/ are
// siblings for the same reason.
func TestBackgroundsLiveBesideTheMibsAndNotInThem(t *testing.T) {
	a, dir := newAssetApp(t)
	if filepath.Dir(dir) != filepath.Dir(a.persistentMibDir) {
		t.Fatalf("%s is not a sibling of %s", dir, a.persistentMibDir)
	}
	if strings.HasPrefix(dir, a.persistentMibDir+string(filepath.Separator)) {
		t.Fatal("the backgrounds are inside the MIB directory")
	}
}

// The gate is a decode. An SVG named .png gets no further than a PDF does.
func TestOnlyAnImageGetsIn(t *testing.T) {
	a, dir := newAssetApp(t)
	src := t.TempDir()

	good := filepath.Join(src, "rack-a.png")
	writePNG(t, good, 40, 30)

	svg := filepath.Join(src, "diagram.png") // named .png, and it is not one
	if err := os.WriteFile(svg, []byte(
		`<svg xmlns="http://www.w3.org/2000/svg"><script>1</script></svg>`), 0o600); err != nil {
		t.Fatal(err)
	}

	results := a.ImportMapAssets([]string{good, svg})
	if len(results) != 2 {
		t.Fatalf("%d result(s)", len(results))
	}
	if !results[0].Success || results[0].Width != 40 || results[0].Height != 30 {
		t.Errorf("a real PNG was refused: %+v", results[0])
	}
	if results[1].Success {
		t.Error("an SVG called .png was imported")
	}
	if !strings.Contains(results[1].Error, "SVG") {
		t.Errorf("the refusal does not say what it is: %q", results[1].Error)
	}

	// And what was refused is not on disk. A preset gate keeps a broken file so
	// its problems can be read; there is nothing to read in an image that does
	// not decode, and keeping it would leave the thing that was refused sitting
	// in the directory the renderer lists.
	if _, err := os.Stat(filepath.Join(dir, "diagram.png")); !os.IsNotExist(err) {
		t.Error("the refused file was written anyway")
	}
	if _, err := os.Stat(filepath.Join(dir, "rack-a.png")); err != nil {
		t.Errorf("the accepted file is not there: %v", err)
	}
}

// The media type is what the DECODER said, never what the name said.
//
// A file called rack.png that is a GIF is served as a GIF — otherwise the
// browser is handed a type that does not match the bytes and is left to sniff,
// which is the one thing a data URI should never require.
func TestTheMediaTypeComesFromTheBytes(t *testing.T) {
	a, dir := newAssetApp(t)

	var buf bytes.Buffer
	if err := gif.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 8, 8)), nil); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "rack.png"), buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	uri, err := a.ReadMapAsset("rack.png")
	if err != nil {
		t.Fatalf("ReadMapAsset: %v", err)
	}
	if !strings.HasPrefix(uri, "data:image/gif;base64,") {
		t.Errorf("served as %.30s…", uri)
	}
}

// Every method here takes a name from outside, and monitoring.db, service.json
// and the secret store sit one directory above this one.
func TestABackgroundNameCannotEscapeItsDirectory(t *testing.T) {
	a, dir := newAssetApp(t)
	writePNG(t, filepath.Join(dir, "ok.png"), 8, 8)

	// Something worth stealing, one level up — where the real database is.
	secret := filepath.Join(filepath.Dir(dir), "monitoring.db")
	if err := os.WriteFile(secret, []byte("not yours"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{
		"../monitoring.db", "..\\monitoring.db", "sub/ok.png", `sub\ok.png`,
		".hidden.png", "", "   ",
	} {
		if _, err := a.ReadMapAsset(name); err == nil {
			t.Errorf("%q was read", name)
		}
		if err := a.DeleteMapAsset(name); err == nil {
			t.Errorf("%q was deleted", name)
		}
	}
	if _, err := os.Stat(secret); err != nil {
		t.Fatalf("the database one level up did not survive: %v", err)
	}
}

// A file that cannot be shown is LISTED with its problem rather than hidden:
// the operator dropped it in the directory by hand and needs to know why the
// map is blank.
func TestAFileThatCannotBeShownIsListedWithItsProblem(t *testing.T) {
	a, dir := newAssetApp(t)
	writePNG(t, filepath.Join(dir, "good.png"), 20, 10)
	if err := os.WriteFile(filepath.Join(dir, "bad.png"), []byte("%PDF-1.7"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A dotfile is not a background, the way it is not a preset or a MIB.
	if err := os.WriteFile(filepath.Join(dir, ".DS_Store"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	list := a.ListMapAssets()
	if len(list) != 2 {
		t.Fatalf("%d background(s): %+v", len(list), list)
	}
	if list[0].Name != "bad.png" || list[0].Error == "" {
		t.Errorf("the broken one is not reported: %+v", list[0])
	}
	if !strings.Contains(list[0].Error, "PDF") {
		t.Errorf("its problem is not named: %q", list[0].Error)
	}
	if list[1].Name != "good.png" || list[1].Error != "" || list[1].Width != 20 {
		t.Errorf("the good one: %+v", list[1])
	}
}

// Deleting a background leaves everything else alone, the way deleting a preset
// leaves bound sessions polling.
func TestDeletingABackgroundRemovesOnlyTheFile(t *testing.T) {
	a, dir := newAssetApp(t)
	writePNG(t, filepath.Join(dir, "one.png"), 8, 8)
	writePNG(t, filepath.Join(dir, "two.png"), 8, 8)

	if err := a.DeleteMapAsset("one.png"); err != nil {
		t.Fatalf("DeleteMapAsset: %v", err)
	}
	if list := a.ListMapAssets(); len(list) != 1 || list[0].Name != "two.png" {
		t.Errorf("after deleting one: %+v", list)
	}
}
