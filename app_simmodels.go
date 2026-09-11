package main

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"SnmpLens/pkg/imagegate"
	"SnmpLens/pkg/simulator"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Custom simulator models: model files somebody else wrote (pkg/simulator,
// ParseCustomModel), imported from a JSON file, or from a ZIP that carries their
// icons beside them.
//
// They are kept in simulator-models/, a SIBLING of simulator.json, mibs/ and
// presets/ and never inside one of them, for the reason assets/ is: whatever
// lists those directories would read a model as a broken MIB or preset. A model
// is filed under its own id — `<id>.json`, and its icon `<id>.png`, `.jpg` or
// `.gif` — never under a name the file or the archive chose.
//
// Only the dialog is bound. The renderer never names a path here: a method that
// took one would read any file the renderer asked for, and a JSON parser's
// errors quote what it read.
const simulatorModelSubdir = "simulator-models"

// The bounds on what an import reads. A ZIP's sizes are what its central
// directory SAYS, and a decompression bomb says little: every entry is read
// through a limit, and what is unpacked is counted as it is read.
const (
	maxModelArchiveBytes = 8 << 20
	maxArchiveEntries    = 64
	maxArchiveUnpacked   = 16 << 20
	maxModelIconBytes    = 256 << 10
	// maxModelIconSide is far more than a picker draws; the bound is what
	// keeps the icons the model list carries small.
	maxModelIconSide = 512
	maxCustomModels  = 64
)

// iconExtensions are what an icon is kept as, by the format imagegate decoded —
// never by the name it arrived with.
var iconExtensions = map[string]string{"png": ".png", "jpeg": ".jpg", "gif": ".gif"}

var utf8BOM = []byte("\xef\xbb\xbf")

// SimulatorModelImportResult is what became of one model file.
type SimulatorModelImportResult struct {
	// File is what was read: the file picked and, for a model in a ZIP, the
	// entry in it.
	File     string `json:"file"`
	ModelID  string `json:"modelId,omitempty"`
	Name     string `json:"name,omitempty"`
	Success  bool   `json:"success"`
	Replaced bool   `json:"replaced"`
	Icon     bool   `json:"icon"`
	Error    string `json:"error,omitempty"`
	// Warnings are about what was imported anyway.
	Warnings []SimulatorModelWarning `json:"warnings"`
}

// SimulatorModelWarning is an i18n key suffix (simulator.models.warning.<key>)
// and what its sentence quotes.
type SimulatorModelWarning struct {
	Key    string `json:"key"`
	Detail string `json:"detail"`
}

// SimulatorModelIcon is a custom model's icon, as a data URI.
type SimulatorModelIcon struct {
	ID   string `json:"id"`
	Icon string `json:"icon"`
}

// modelIcon is an icon that passed the gate, and the extension it is kept as.
type modelIcon struct {
	data []byte
	ext  string
}

// loadModels reads the custom models from their directory and makes them the
// simulator's. A file that no longer reads — edited by hand, or written by a
// later version — is logged and left where it is; a device of its model says
// which model it lacks when it is started.
func (s *simulatorService) loadModels() {
	var list []simulator.CustomModel
	entries, err := os.ReadDir(s.modelDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("simulator: cannot list %s: %v", s.modelDir, err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.EqualFold(filepath.Ext(name), ".json") {
			continue
		}
		raw, err := readAtMost(filepath.Join(s.modelDir, name), simulator.MaxCustomModelBytes)
		if err != nil {
			log.Printf("simulator: model %s: %v", name, err)
			continue
		}
		m, err := simulator.ParseCustomModel(bytes.TrimPrefix(raw, utf8BOM))
		if err != nil {
			log.Printf("simulator: model %s no longer reads: %v", name, err)
			continue
		}
		if m.Slug()+".json" != name {
			log.Printf("simulator: %s says it is %q; a model is kept under its own id", name, m.Slug())
			continue
		}
		list = append(list, m)
	}
	if err := simulator.SetCustomModels(list); err != nil {
		log.Printf("simulator: the custom models: %v", err)
		return
	}
	s.models = list
}

// emitSimulatorModelsChanged tells the renderer to list the models again.
func (a *App) emitSimulatorModelsChanged() {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "simulator:models")
	}
}

// ListSimulatorModelIcons returns the icon of each custom model that has one.
//
// Decoded again on every listing, as a map background is: a file in the
// directory can be replaced by hand, and whether it draws is decided by the
// same gate either way.
func (a *App) ListSimulatorModelIcons() []SimulatorModelIcon {
	out := []SimulatorModelIcon{}
	s := a.sim
	if s == nil {
		return out
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.models {
		if uri, ok := s.iconOf(m.Slug()); ok {
			out = append(out, SimulatorModelIcon{ID: m.ID(), Icon: uri})
		}
	}
	return out
}

// iconOf is the icon kept for a model, as a data URI whose media type is what
// the DECODER said.
func (s *simulatorService) iconOf(slug string) (string, bool) {
	for _, ext := range []string{".png", ".jpg", ".gif"} {
		data, err := readAtMost(filepath.Join(s.modelDir, slug+ext), maxModelIconBytes)
		if err != nil {
			continue
		}
		meta, err := checkIcon(data)
		if err != nil {
			log.Printf("simulator: the icon of model %s: %v", slug, err)
			continue
		}
		return "data:" + meta.Media + ";base64," + base64.StdEncoding.EncodeToString(data), true
	}
	return "", false
}

// checkIcon is pkg/imagegate's decode, and a picker's bounds on top of it.
func checkIcon(data []byte) (imagegate.Info, error) {
	if len(data) > maxModelIconBytes {
		return imagegate.Info{}, fmt.Errorf("an icon is at most %d KB", maxModelIconBytes>>10)
	}
	meta, err := imagegate.Inspect(data)
	if err != nil {
		return meta, err
	}
	if meta.Width > maxModelIconSide || meta.Height > maxModelIconSide {
		return meta, fmt.Errorf("an icon is at most %d×%d pixels, and this one is %d×%d",
			maxModelIconSide, maxModelIconSide, meta.Width, meta.Height)
	}
	if _, ok := iconExtensions[meta.Format]; !ok {
		return meta, fmt.Errorf("%s is not a format an icon is kept in", meta.Format)
	}
	return meta, nil
}

// ImportSimulatorModelsDialog asks for model files — JSON, or ZIP archives with
// icons — and imports them.
func (a *App) ImportSimulatorModelsDialog() ([]SimulatorModelImportResult, error) {
	if a.ctx == nil {
		return nil, fmt.Errorf("no window")
	}
	paths, err := runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Import simulator models",
		Filters: []runtime.FileFilter{
			{DisplayName: "Simulator models (JSON, ZIP)", Pattern: "*.json;*.zip"},
		},
	})
	if err != nil {
		return nil, err
	}
	// Cancelled is an empty list, never null: the caller reports each result.
	return a.importSimulatorModels(paths), nil
}

// importSimulatorModels imports each file, makes what the directory now holds
// the simulator's models, and restarts the running devices of a model that
// changed — as an edited device restarts, so that it answers as it now is.
func (a *App) importSimulatorModels(paths []string) []SimulatorModelImportResult {
	results := []SimulatorModelImportResult{}
	s := a.sim
	if s == nil {
		for _, p := range paths {
			results = append(results, SimulatorModelImportResult{File: filepath.Base(p), Error: errNoSimulator.Error(),
				Warnings: []SimulatorModelWarning{}})
		}
		return results
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range paths {
		results = append(results, s.importModelFile(p)...)
	}
	s.loadModels()
	for r := range results {
		res := &results[r]
		if !res.Success || !res.Replaced {
			continue
		}
		for i, d := range s.devices {
			if d.Model != res.ModelID || !s.fleet.Stop(d.ID) {
				continue
			}
			if err := a.startSimulatedLocked(s, i); err != nil {
				res.Warnings = append(res.Warnings, SimulatorModelWarning{Key: "restartFailed", Detail: fmt.Sprintf("%s: %v", d.Name, err)})
			}
		}
	}
	a.emitSimulatorModelsChanged()
	return results
}

// importModelFile imports what one picked file holds: a model, or an archive of
// them. What it IS is read from its first bytes, not from its name.
func (s *simulatorService) importModelFile(src string) []SimulatorModelImportResult {
	name := filepath.Base(src)
	fail := func(format string, args ...any) []SimulatorModelImportResult {
		return []SimulatorModelImportResult{{File: name, Error: fmt.Sprintf(format, args...), Warnings: []SimulatorModelWarning{}}}
	}
	data, err := readAtMost(src, maxModelArchiveBytes)
	if err != nil {
		return fail("%v", err)
	}
	if bytes.HasPrefix(data, []byte("PK\x03\x04")) {
		return s.importModelArchive(name, data)
	}
	data = bytes.TrimPrefix(data, utf8BOM)
	if trimmed := bytes.TrimLeft(data, " \t\r\n"); len(trimmed) == 0 || trimmed[0] != '{' {
		return fail("this is neither a model file (JSON) nor a ZIP archive")
	}
	// A lone file cannot bring an icon: the one it names would have to be read
	// from wherever the file came from, and a path a stranger's file chooses is
	// exactly what is never followed.
	skipped := func(icon string) (*modelIcon, *SimulatorModelWarning) {
		return nil, &SimulatorModelWarning{Key: "iconSkipped", Detail: icon}
	}
	return []SimulatorModelImportResult{s.importModel(name, data, skipped)}
}

// importModelArchive imports every model in a ZIP, each with the icon it names
// from the same archive. Nothing in the archive is ever written under the name
// it has there.
func (s *simulatorService) importModelArchive(name string, data []byte) []SimulatorModelImportResult {
	fail := func(format string, args ...any) []SimulatorModelImportResult {
		return []SimulatorModelImportResult{{File: name, Error: fmt.Sprintf(format, args...), Warnings: []SimulatorModelWarning{}}}
	}
	// An entry named ../x is harmless here, since no entry is written by its
	// name; Go reports one as ErrInsecurePath beside a reader that works.
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil && !errors.Is(err, zip.ErrInsecurePath) {
		return fail("this ZIP archive cannot be read: %v", err)
	}
	var files []*zip.File
	for _, f := range zr.File {
		base := path.Base(f.Name)
		if f.FileInfo().IsDir() || strings.HasPrefix(f.Name, "__MACOSX/") || strings.HasPrefix(base, ".") {
			continue
		}
		files = append(files, f)
	}
	if len(files) > maxArchiveEntries {
		return fail("the archive holds %d files, and an import reads at most %d", len(files), maxArchiveEntries)
	}

	var unpacked int64
	read := func(f *zip.File, limit int64) ([]byte, error) {
		if f.UncompressedSize64 > uint64(limit) {
			return nil, fmt.Errorf("%s unpacks to more than %d KB", path.Base(f.Name), limit>>10)
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("%s cannot be unpacked: %w", path.Base(f.Name), err)
		}
		defer rc.Close()
		// archive/zip stops at the size an entry declares, and the limit is the
		// bound on what it may declare.
		b, err := io.ReadAll(io.LimitReader(rc, limit+1))
		if err != nil {
			return nil, fmt.Errorf("%s cannot be unpacked: %w", path.Base(f.Name), err)
		}
		if int64(len(b)) > limit {
			return nil, fmt.Errorf("%s unpacks to more than %d KB", path.Base(f.Name), limit>>10)
		}
		if unpacked += int64(len(b)); unpacked > maxArchiveUnpacked {
			return nil, fmt.Errorf("the archive unpacks to more than %d MB", maxArchiveUnpacked>>20)
		}
		return b, nil
	}

	results := []SimulatorModelImportResult{}
	for _, f := range files {
		if !strings.EqualFold(path.Ext(f.Name), ".json") {
			continue
		}
		entry := name + " › " + clipText(f.Name, 120)
		raw, err := read(f, simulator.MaxCustomModelBytes)
		if err != nil {
			results = append(results, SimulatorModelImportResult{File: entry, Error: err.Error(), Warnings: []SimulatorModelWarning{}})
			if unpacked > maxArchiveUnpacked {
				break
			}
			continue
		}
		dir := path.Dir(f.Name)
		lookup := func(icon string) (*modelIcon, *SimulatorModelWarning) {
			// Beside the model first: two models of one archive may each call
			// theirs icon.png.
			var found *zip.File
			for _, g := range files {
				if path.Base(g.Name) == icon && (found == nil || path.Dir(g.Name) == dir) {
					found = g
				}
			}
			if found == nil {
				return nil, &SimulatorModelWarning{Key: "iconMissing", Detail: icon}
			}
			data, err := read(found, maxModelIconBytes)
			if err != nil {
				return nil, &SimulatorModelWarning{Key: "iconRefused", Detail: err.Error()}
			}
			meta, err := checkIcon(data)
			if err != nil {
				return nil, &SimulatorModelWarning{Key: "iconRefused", Detail: err.Error()}
			}
			return &modelIcon{data: data, ext: iconExtensions[meta.Format]}, nil
		}
		results = append(results, s.importModel(entry, bytes.TrimPrefix(raw, utf8BOM), lookup))
	}
	if len(results) == 0 {
		return fail("the archive holds no model: a model is a .json file in it")
	}
	return results
}

// importModel checks one model file and keeps it, with the icon it names when
// iconFor finds one. A model imported without an icon keeps the one it had:
// someone editing the JSON alone should not lose what the archive brought.
func (s *simulatorService) importModel(file string, raw []byte, iconFor func(string) (*modelIcon, *SimulatorModelWarning)) SimulatorModelImportResult {
	res := SimulatorModelImportResult{File: file, Warnings: []SimulatorModelWarning{}}
	m, err := simulator.ParseCustomModel(raw)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	res.ModelID, res.Name = m.ID(), m.Name()
	jsonPath := filepath.Join(s.modelDir, m.Slug()+".json")
	if _, err := os.Stat(jsonPath); err == nil {
		res.Replaced = true
	} else if s.countModelFiles() >= maxCustomModels {
		res.Error = fmt.Sprintf("SnmpLens keeps at most %d custom models", maxCustomModels)
		return res
	}
	if err := os.MkdirAll(s.modelDir, 0o755); err != nil {
		res.Error = fmt.Sprintf("could not create the model directory: %v", err)
		return res
	}

	var icon *modelIcon
	var warning *SimulatorModelWarning
	if m.Icon() != "" {
		icon, warning = iconFor(m.Icon())
	}
	// The icon before the model: a model kept is one the picker shows, and it
	// should not show without the icon it came with.
	if icon != nil {
		for _, ext := range iconExtensions {
			if ext != icon.ext {
				_ = os.Remove(filepath.Join(s.modelDir, m.Slug()+ext))
			}
		}
		if err := writeFileAtomic(filepath.Join(s.modelDir, m.Slug()+icon.ext), icon.data); err != nil {
			res.Error = fmt.Sprintf("could not keep the icon: %v", err)
			return res
		}
		res.Icon = true
	} else if _, kept := s.iconOf(m.Slug()); kept {
		res.Icon = true
		if warning != nil && warning.Key == "iconSkipped" {
			warning = nil
		}
	}
	if warning != nil {
		res.Warnings = append(res.Warnings, *warning)
	}
	if err := writeFileAtomic(jsonPath, raw); err != nil {
		res.Error = fmt.Sprintf("could not keep the model: %v", err)
		return res
	}
	res.Success = true
	return res
}

// countModelFiles is how many models the directory holds now, imports of this
// round included.
func (s *simulatorService) countModelFiles() int {
	entries, err := os.ReadDir(s.modelDir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".json") {
			n++
		}
	}
	return n
}

// SimulatorDeleteModel deletes a custom model and its icon.
//
// A model a device is made from is not deleted: the device would not start
// again, and nothing would say why until somebody tried.
func (a *App) SimulatorDeleteModel(id string) error {
	s := a.sim
	if s == nil {
		return errNoSimulator
	}
	slug, ok := simulator.CustomModelSlug(id)
	if !ok {
		return fmt.Errorf("%q is not a custom model", id)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var users []string
	for _, d := range s.devices {
		if d.Model == id {
			users = append(users, d.Name)
		}
	}
	if len(users) > 0 {
		return fmt.Errorf("%d simulated device(s) are made from this model (%s): delete them first",
			len(users), strings.Join(users, ", "))
	}
	for _, ext := range slices.Concat([]string{".json"}, []string{".png", ".jpg", ".gif"}) {
		if err := os.Remove(filepath.Join(s.modelDir, slug+ext)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("deleting the model: %w", err)
		}
	}
	s.loadModels()
	a.emitSimulatorModelsChanged()
	return nil
}

// readAtMost reads a file, refusing one larger than limit: checked on the open
// file, and again while reading, since it can grow in between.
func readAtMost(name string, limit int64) ([]byte, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if st.IsDir() {
		return nil, fmt.Errorf("%s is a directory", filepath.Base(name))
	}
	if st.Size() > limit {
		return nil, fmt.Errorf("%s is %d KB, and at most %d KB is read", filepath.Base(name), st.Size()>>10, limit>>10)
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s is larger than %d KB", filepath.Base(name), limit>>10)
	}
	return data, nil
}

// writeFileAtomic replaces a file through a temporary one, so that a crash
// mid-write never leaves half of one.
func writeFileAtomic(name string, data []byte) error {
	tmp := name + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, name)
}

// clipText shortens what an archive calls an entry to what a result line can
// show.
func clipText(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
