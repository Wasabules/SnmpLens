// Command cataloguecheck validates a catalogue of contributed files —
// simulator models and dashboard presets — with the code the application reads
// them with, so that what the check accepts is what SnmpLens will import.
//
//	go run ./tools/cataloguecheck <catalogue directory>
//
// The directory is the one snmplens-community holds:
//
//	models/<id>/       a package: model.json, oids.json or oids/*.json,
//	                   traps.json, walks/*, and the icon model.json names
//	models/<id>.json   a model in one file, which brings no icon
//	presets/*.json     dashboard presets
//
// A contribution is a file somebody else wrote, and the format is a frozen
// vocabulary it PICKS from — see pkg/simulator/custom.go and pkg/preset. This
// command is deliberately a thin shell around those packages rather than a
// second reading of the same rules: a checker that drifts from the importer
// accepts what the application then refuses, in a repository whose whole
// purpose is to hand files to the application.
package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"SnmpLens/pkg/preset"
	"SnmpLens/pkg/simulator"
)

// maxIconBytes is the application's own bound on an icon
// (internal/app/simmodels.go), which also holds it to 512 px a side once it is
// decoded. The side is not checked here: it is a property of the image, and
// the import reports it.
const maxIconBytes = 256 << 10

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: cataloguecheck <catalogue directory>")
		os.Exit(2)
	}

	r := &report{}
	if err := r.walkCatalogue(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	fmt.Print(r.text())
	if r.failed > 0 {
		os.Exit(1)
	}
}

type report struct {
	lines    []string
	models   int
	presets  int
	warnings int
	failed   int
}

func (r *report) ok(format string, a ...any) {
	r.lines = append(r.lines, "  ok    "+fmt.Sprintf(format, a...))
}

func (r *report) warn(format string, a ...any) {
	r.warnings++
	r.lines = append(r.lines, "  warn  "+fmt.Sprintf(format, a...))
}

func (r *report) fail(format string, a ...any) {
	r.failed++
	r.lines = append(r.lines, "  FAIL  "+fmt.Sprintf(format, a...))
}

func (r *report) text() string {
	var b strings.Builder
	for _, l := range r.lines {
		b.WriteString(l + "\n")
	}
	fmt.Fprintf(&b, "%d model(s), %d preset(s), %d warning(s), %d failure(s)\n",
		r.models, r.presets, r.warnings, r.failed)
	return b.String()
}

// walkCatalogue reads both halves. Either may be absent — a catalogue of
// presets alone is a catalogue — but a directory that cannot be read is an
// error rather than an empty result, or a wrong path would report success.
func (r *report) walkCatalogue(root string) error {
	if _, err := os.Stat(root); err != nil {
		return fmt.Errorf("catalogue: %w", err)
	}
	if err := r.checkModels(filepath.Join(root, "models")); err != nil {
		return err
	}
	return r.checkPresets(filepath.Join(root, "presets"))
}

func (r *report) checkModels(dir string) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		if skipped(name) {
			continue
		}
		switch {
		case e.IsDir():
			r.checkPackage(filepath.Join(dir, name), name)
		case strings.EqualFold(filepath.Ext(name), ".json"):
			r.checkLoneModel(filepath.Join(dir, name), strings.TrimSuffix(name, filepath.Ext(name)))
		default:
			r.fail("models/%s is neither a package folder nor a .json model", name)
		}
	}
	return nil
}

// checkPackage reads one package folder and builds the model from it, which is
// what the import does: an OID given twice, one under another, or a value its
// type cannot carry is refused here rather than at the first start.
func (r *report) checkPackage(dir, slug string) {
	files, err := r.readPackage(dir, slug)
	if err != nil {
		r.fail("models/%s: %v", slug, err)
		return
	}
	m, warnings, err := simulator.ParseCustomPackage(files)
	if err != nil {
		r.fail("models/%s: %v", slug, err)
		return
	}
	for _, w := range warnings {
		r.warn("models/%s: %s %s", slug, w.Key, w.Detail)
	}
	// A package sits under the id it declares, so that a reader looking for a
	// model in the application finds the folder it came from.
	if m.Slug() != slug {
		r.fail("models/%s declares the id %q", slug, m.Slug())
		return
	}
	if icon := m.Icon(); icon != "" {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(icon))); err != nil {
			r.warn("models/%s names the icon %q, which the folder does not hold", slug, icon)
		}
	}
	r.models++
	r.ok("models/%s — %s, %d file(s)", slug, m.Name(), len(files))
}

// readPackage keeps the files a package READS, by the application's own rule,
// under the application's own per-file bounds. A JSON file it does not read is
// NAMED rather than passed over: traps.json misspelt would otherwise leave a
// device with no notifications and nothing saying why.
func (r *report) readPackage(dir, slug string) ([]simulator.PackageFile, error) {
	var files []simulator.PackageFile
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if !simulator.PackageReads(rel) {
			if strings.EqualFold(path.Ext(rel), ".json") {
				r.warn("models/%s: %s is not a file a package reads", slug, rel)
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if limit := fileLimit(rel); info.Size() > limit {
			return fmt.Errorf("%s is %d bytes, past the %d the application reads", rel, info.Size(), limit)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		files = append(files, simulator.PackageFile{Name: rel, Data: data})
		return nil
	})
	return files, err
}

func (r *report) checkLoneModel(file, slug string) {
	data, err := os.ReadFile(file)
	if err != nil {
		r.fail("models/%s.json: %v", slug, err)
		return
	}
	if int64(len(data)) > simulator.MaxCustomModelBytes {
		r.fail("models/%s.json is %d bytes, past the %d the application reads", slug, len(data), simulator.MaxCustomModelBytes)
		return
	}
	m, err := simulator.ParseCustomModel(data)
	if err != nil {
		r.fail("models/%s.json: %v", slug, err)
		return
	}
	if m.Slug() != slug {
		r.fail("models/%s.json declares the id %q", slug, m.Slug())
		return
	}
	r.models++
	r.ok("models/%s.json — %s", slug, m.Name())
}

// checkPresets reads each preset as the library does, and states what it would
// cost: a preset makes the application emit SNMP traffic to somebody's own
// devices, at OIDs the file chose, so the figure belongs in the review.
func (r *report) checkPresets(dir string) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		if skipped(name) {
			continue
		}
		if e.IsDir() || !strings.EqualFold(filepath.Ext(name), ".json") {
			r.fail("presets/%s is not a .json preset", name)
			continue
		}
		p, problems, err := preset.LoadFile(filepath.Join(dir, name))
		if err != nil {
			r.fail("presets/%s: %v", name, err)
			continue
		}
		if len(problems) > 0 {
			for _, problem := range problems {
				r.fail("presets/%s: %s — %s", name, problem.Field, problem.Message)
			}
			continue
		}
		c := preset.Estimate(p)
		r.presets++
		r.ok("presets/%s — %d widget(s), %d OID(s) every %d s", name, c.Widgets, c.OIDs, c.IntervalSec)
	}
	return nil
}

// A catalogue is a repository: it carries its own README, its licence and
// whatever a dotfile brings, none of which is a contribution.
func skipped(name string) bool {
	return strings.HasPrefix(name, ".") ||
		strings.EqualFold(name, "README.md") ||
		strings.EqualFold(name, "LICENSE")
}

// fileLimit mirrors the application's per-file bounds (internal/app,
// packageFileLimit): a walk is the recording of a real device and is allowed to
// be large, an icon is small, and everything written by hand is held to the
// model file's own limit.
func fileLimit(rel string) int64 {
	switch {
	case strings.HasPrefix(rel, "walks/"):
		return simulator.MaxWalkBytes
	case isImage(rel):
		return maxIconBytes
	default:
		return simulator.MaxCustomModelBytes
	}
}

func isImage(rel string) bool {
	switch strings.ToLower(path.Ext(rel)) {
	case ".png", ".jpg", ".jpeg", ".gif":
		return true
	}
	return false
}
