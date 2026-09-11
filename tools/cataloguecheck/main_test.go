package main

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The catalogue is built from what the repository already has: the example
// custom model, the package under pkg/simulator/testdata, and a bundled preset.
// Anything else would be a second copy of the format, which is the failure this
// command exists to prevent.
const repoRoot = "../.."

func TestACatalogueOfWhatTheApplicationShips(t *testing.T) {
	dir := t.TempDir()
	slug := copyLoneModel(t, dir)
	pkg := copyPackage(t, dir)
	name := copyFirstPreset(t, dir)

	r := &report{}
	if err := r.walkCatalogue(dir); err != nil {
		t.Fatalf("walkCatalogue: %v", err)
	}
	if r.failed != 0 {
		t.Fatalf("a catalogue of what the application ships failed:\n%s", r.text())
	}
	if r.models != 2 || r.presets != 1 {
		t.Fatalf("counted %d models and %d presets, wanted 2 and 1:\n%s", r.models, r.presets, r.text())
	}
	for _, want := range []string{slug, pkg, name} {
		if !strings.Contains(r.text(), want) {
			t.Errorf("the report does not name %q:\n%s", want, r.text())
		}
	}
}

func TestAModelThatDoesNotParseFails(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "models", "broken.json"), []byte(`{"kind": "snmplens-simulator-model",`))

	r := &report{}
	if err := r.walkCatalogue(dir); err != nil {
		t.Fatalf("walkCatalogue: %v", err)
	}
	if r.failed == 0 {
		t.Fatalf("a model that does not parse was accepted:\n%s", r.text())
	}
}

func TestAModelUnderAnotherIdFails(t *testing.T) {
	dir := t.TempDir()
	slug := copyLoneModel(t, dir)
	from := filepath.Join(dir, "models", slug+".json")
	if err := os.Rename(from, filepath.Join(dir, "models", "somebody-elses-name.json")); err != nil {
		t.Fatal(err)
	}

	r := &report{}
	if err := r.walkCatalogue(dir); err != nil {
		t.Fatalf("walkCatalogue: %v", err)
	}
	if r.failed == 0 {
		t.Fatalf("a model filed under an id it does not declare was accepted:\n%s", r.text())
	}
}

// A catalogue with neither half is not an error: the command is run on a
// repository that may hold only presets, or only models.
func TestAnEmptyCatalogueIsNotAFailure(t *testing.T) {
	r := &report{}
	if err := r.walkCatalogue(t.TempDir()); err != nil {
		t.Fatalf("walkCatalogue: %v", err)
	}
	if r.failed != 0 || r.models != 0 || r.presets != 0 {
		t.Fatalf("an empty catalogue was not empty:\n%s", r.text())
	}
}

func TestAMissingCatalogueIsAnError(t *testing.T) {
	r := &report{}
	if err := r.walkCatalogue(filepath.Join(t.TempDir(), "nothing-here")); err == nil {
		t.Fatal("a path that does not exist reported success")
	}
}

/* --- the catalogue the tests build --------------------------------------- */

func copyLoneModel(t *testing.T, dir string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot, "pkg", "simulator", "testdata", "custom-model.json"))
	if err != nil {
		t.Fatal(err)
	}
	slug := idOf(t, raw)
	write(t, filepath.Join(dir, "models", slug+".json"), raw)
	return slug
}

func copyPackage(t *testing.T, dir string) string {
	t.Helper()
	src := filepath.Join(repoRoot, "pkg", "simulator", "testdata", "package")
	raw, err := os.ReadFile(filepath.Join(src, "model.json"))
	if err != nil {
		t.Fatal(err)
	}
	slug := idOf(t, raw)
	dst := filepath.Join(dir, "models", slug)
	err = filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		write(t, filepath.Join(dst, rel), data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return slug
}

func copyFirstPreset(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(repoRoot, "presets"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(repoRoot, "presets", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		write(t, filepath.Join(dir, "presets", e.Name()), data)
		return e.Name()
	}
	t.Fatal("the repository ships no preset to check against")
	return ""
}

func idOf(t *testing.T, raw []byte) string {
	t.Helper()
	var head struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &head); err != nil || head.ID == "" {
		t.Fatalf("the model file names no id: %v", err)
	}
	return head.ID
}

func write(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
