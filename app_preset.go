package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"SnmpLens/pkg/mib"
	"SnmpLens/pkg/preset"
	"SnmpLens/pkg/snmp"
	"SnmpLens/pkg/storage"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// The preset library: where dashboard presets live on this machine, and how one
// gets in.
//
// A preset is a file somebody else wrote. Everything here exists because of
// that sentence — the directory it lands in, what is allowed to land there, and
// the fact that nothing is bound or polled as a side effect of importing one.

// presetSubdir is a SIBLING of mibs/, never a child.
//
// ListMibFiles enumerates the MIB directory and feeds what it finds to gosmi;
// a JSON file in there would be loaded as a module and reported as a broken
// MIB. mib-backups/, mib-drafts/ and mib-temp/ are siblings for the same
// reason, and this follows them.
const presetSubdir = "presets"

// PresetImportResult is one file's outcome, shaped like MibImportResult because
// it is shown in the same kind of list.
type PresetImportResult struct {
	FileName string `json:"fileName"`
	Success  bool   `json:"success"`
	Skipped  bool   `json:"skipped,omitempty"`
	Error    string `json:"error,omitempty"`
	// Problems is how many validation errors the imported file has. A preset
	// with problems still imports — see importSinglePreset — so this is the
	// difference between "in your library" and "ready to bind".
	Problems int `json:"problems"`
}

// PresetCost is what preset.Estimate says, plus the one number only this layer
// can compute.
type PresetCost struct {
	preset.Cost
	// RequestsPerDay is rounds multiplied by how many requests a round takes,
	// which depends on the transport's chunk size (snmp.MaxVarbindsPerGet).
	// pkg/preset deliberately does not know that number: a copy of it there
	// would stop matching the first time it is tuned.
	RequestsPerDay int `json:"requestsPerDay"`
	// VarbindsPerRequest is carried so the interface can say WHY the request
	// count is what it is rather than presenting it as a magic number.
	VarbindsPerRequest int `json:"varbindsPerRequest"`
}

// PresetDetail is everything one preset screen needs, in one call.
type PresetDetail struct {
	File   string         `json:"file"`
	Preset preset.Preset  `json:"preset"`
	Errors []preset.Error `json:"errors"`
	Cost   PresetCost     `json:"cost"`
}

// presetDir returns the preset directory, creating it on first use.
func (a *App) presetDir() (string, error) {
	if a.persistentMibDir == "" {
		return "", fmt.Errorf("the configuration directory is not ready")
	}
	dir := filepath.Join(filepath.Dir(a.persistentMibDir), presetSubdir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("could not create the preset directory: %w", err)
	}
	return dir, nil
}

// resolvePresetPath turns a name the renderer supplied into a path inside the
// preset directory, or refuses.
//
// The same shape as mib.SafeMibPath, and for the same reason: monitoring.db,
// service.json and the secret store all sit one directory above this one, and
// every method here takes a file name from outside.
func (a *App) resolvePresetPath(name string) (string, error) {
	dir, err := a.presetDir()
	if err != nil {
		return "", err
	}
	clean := filepath.Base(strings.TrimSpace(name))
	if clean == "" || clean == "." || clean == ".." || clean == string(filepath.Separator) {
		return "", fmt.Errorf("invalid preset file name %q", name)
	}
	full := filepath.Join(dir, clean)
	if filepath.Dir(full) != filepath.Clean(dir) {
		return "", fmt.Errorf("refusing a preset path outside the preset directory: %q", name)
	}
	return full, nil
}

// ListPresets returns the preset library.
//
// Named ListPresets rather than PresetList on purpose: tools/genbridge.mjs
// gives anything starting with List an empty ARRAY as its screenshot fixture,
// and a name it does not recognise gets null — which throws on the first .map
// in a generated file nobody reads.
func (a *App) ListPresets() []preset.Info {
	dir, err := a.presetDir()
	if err != nil {
		log.Printf("ListPresets: %v", err)
		return []preset.Info{}
	}
	list, err := preset.List(dir)
	if err != nil {
		log.Printf("ListPresets: %v", err)
		return []preset.Info{}
	}
	return list
}

// ReadPreset opens one preset: what it says, what is wrong with it, and what it
// would cost.
//
// One call rather than three, because they are one screen and three calls would
// let it show a cost computed from a file the error list no longer describes.
func (a *App) ReadPreset(name string) (PresetDetail, error) {
	path, err := a.resolvePresetPath(name)
	if err != nil {
		return PresetDetail{}, err
	}
	p, errs, err := preset.LoadFile(path)
	if err != nil {
		return PresetDetail{}, err
	}
	return PresetDetail{
		File:   filepath.Base(path),
		Preset: p,
		// Never nil. Validate builds its slice with append and returns nil on
		// success, which crosses the bridge as null and throws on .map — on
		// the VALID path, which is the one nobody tests by hand.
		Errors: nonNilErrors(errs),
		Cost:   presetCost(p),
	}, nil
}

// PresetEstimate is the cost alone, for a caller that has not opened the file.
func (a *App) PresetEstimate(name string) (PresetCost, error) {
	path, err := a.resolvePresetPath(name)
	if err != nil {
		return PresetCost{}, err
	}
	p, _, err := preset.LoadFile(path)
	if err != nil {
		return PresetCost{}, err
	}
	return presetCost(p), nil
}

// ListPresetWidgetKinds serves the frozen vocabulary to the interface.
//
// The descriptions are i18n key suffixes, not prose: the renderer looks up
// preset.widget.<kind>. Served from Go rather than mirrored in JavaScript
// because a hand-copied list drifts, and the first symptom would be a kind the
// library offers and the validator rejects.
func (a *App) ListPresetWidgetKinds() []preset.WidgetDoc {
	return preset.WidgetKinds()
}

// ImportPresetFiles copies preset files into the library.
//
// The shape of ImportMibFiles, including the part that matters most: this
// method reads any absolute path the renderer names, and ReadPreset hands back
// the content of anything in the destination — so the two together are an
// arbitrary-file-read primitive over the bridge, and what bounds its worth is
// what is allowed to land. The gate here is POSITIVE rather than a blacklist:
// a file gets in only if it parses as JSON and declares a formatVersion. A
// private key, a password file, a browser profile: none of those do.
//
// A preset that FAILS validation still imports, exactly as a MIB with a syntax
// error does. The error list with its field paths is what the library is for,
// and you cannot fix a file you were not allowed to keep. What it cannot do is
// bind — that check lives where binding happens.
func (a *App) ImportPresetFiles(paths []string) []PresetImportResult {
	results := make([]PresetImportResult, 0, len(paths))
	for _, src := range paths {
		results = append(results, a.importSinglePreset(src))
	}
	return results
}

func (a *App) importSinglePreset(src string) PresetImportResult {
	name := filepath.Base(src)
	fail := func(format string, args ...any) PresetImportResult {
		return PresetImportResult{FileName: name, Success: false, Error: fmt.Sprintf(format, args...)}
	}

	dst, err := a.resolvePresetPath(name)
	if err != nil {
		return fail("%v", err)
	}

	// Bounded before it is read: os.ReadFile on a renderer-named path is an
	// unbounded allocation, so a path to a large file — or to a device that
	// never ends — is a way to take the process down without a single packet.
	if info, err := os.Stat(src); err == nil && info.Size() > preset.MaxFileBytes {
		return fail("this file is %d KB; a preset is a small JSON document and nothing legitimate is over %d KB",
			info.Size()/1024, preset.MaxFileBytes/1024)
	}

	data, err := os.ReadFile(src)
	if err != nil {
		log.Printf("ImportPresetFiles: could not read %s: %v", src, err)
		return fail("read error: %v", err)
	}

	// The gate. Not a validity check — a preset with a bad OID is exactly what
	// the error list exists for — but a check that this is a preset AT ALL.
	var probe struct {
		FormatVersion *int `json:"formatVersion"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		// Not JSON. pkg/mib already recognises the files people download by
		// mistake — an HTML error page, a PDF, a zip, UTF-16 — so the refusal
		// names what the file actually is instead of saying "not JSON".
		if summary, hint, reject := mib.ImportRejection(data); reject && summary != "" {
			msg := summary
			if hint != "" {
				msg += " — " + hint
			}
			return fail("%s", msg)
		}
		return fail("this file is not a preset: it does not parse as JSON (%v)", err)
	}
	if probe.FormatVersion == nil {
		return fail("this file is JSON but not a preset: it declares no formatVersion")
	}

	if existing, err := os.ReadFile(dst); err == nil && bytes.Equal(existing, data) {
		_, errs := preset.Parse(data)
		return PresetImportResult{FileName: name, Success: true, Skipped: true, Problems: len(errs)}
	}

	if err := os.WriteFile(dst, data, 0o600); err != nil {
		log.Printf("ImportPresetFiles: could not write %s: %v", dst, err)
		return fail("write error: %v", err)
	}
	_, errs := preset.Parse(data)
	log.Printf("ImportPresetFiles: imported %s (%d problem(s))", name, len(errs))
	return PresetImportResult{FileName: name, Success: true, Problems: len(errs)}
}

// ImportPresetDialog is the button next to the drop zone.
//
// Drag-and-drop is the path most files take, but a file manager is not always
// the way somebody has the file in front of them — and a dialog is also the
// only route when the window is not focused enough to receive a drop.
func (a *App) ImportPresetDialog() ([]PresetImportResult, error) {
	if a.ctx == nil {
		return nil, fmt.Errorf("no window")
	}
	paths, err := runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Add dashboard presets",
		Filters: []runtime.FileFilter{
			{DisplayName: "Preset files", Pattern: "*.json;*"},
		},
	})
	if err != nil {
		return nil, err
	}
	// Cancelled. An empty ARRAY rather than nil: the caller renders a result
	// list, and null throws on .map.
	return a.ImportPresetFiles(paths), nil
}

// PresetBind is the moment a preset stops being a file and starts being a
// monitoring: it creates one session for one equipment and starts it.
//
// ONE SESSION PER TARGET, never one session across several. The cost was stated
// per equipment, the credentials are per equipment, and the guardrail in
// pkg/monitor is per session — so one slow device backing off must not slow the
// healthy ones down with it.
//
// What to poll is materialised into the session's OWN columns: the OID list and
// the interval are what the poll clock reads, and it never opens the snapshot.
// A session whose layout cannot be decoded keeps polling and loses its
// dashboard; the other way round it would keep its dashboard and poll nothing.
//
// A preset with problems cannot be bound. It can be imported, listed and read —
// that is what the error list is for — but binding is the step that puts
// traffic on somebody's network, and doing that from a file this application
// has already said it does not understand is not a thing to do quietly.
func (a *App) PresetBind(fileName, target, snmpVersion string, conn MonitorConnection) (storage.Session, error) {
	if a.storage == nil || a.scheduler == nil {
		return storage.Session{}, fmt.Errorf("storage not initialized")
	}
	target = strings.TrimSpace(target)
	if target == "" {
		return storage.Session{}, fmt.Errorf("a preset is bound to an equipment; no target was given")
	}

	path, err := a.resolvePresetPath(fileName)
	if err != nil {
		return storage.Session{}, err
	}
	p, errs, err := preset.LoadFile(path)
	if err != nil {
		return storage.Session{}, err
	}
	if len(errs) > 0 {
		return storage.Session{}, fmt.Errorf(
			"%s has %d problem(s) and cannot be bound until they are fixed", filepath.Base(path), len(errs))
	}

	oids := preset.PollOIDs(p)
	if len(oids) == 0 {
		return storage.Session{}, fmt.Errorf("%s polls nothing", filepath.Base(path))
	}

	name := strings.TrimSpace(p.Name)
	if name == "" {
		name = filepath.Base(path)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	sessConn, creds := conn.split()

	id, err := a.storage.CreateSession(
		name, strings.Join(oids, ","), []string{target},
		p.IntervalSec*1000, snmpVersion, now,
		// No thresholds: a preset describes what to WATCH, and what counts as
		// too much is the operator's judgement about their own network.
		nil, sessConn,
		&storage.SessionPreset{
			File:          filepath.Base(path),
			Name:          p.Name,
			FormatVersion: p.FormatVersion,
			BoundAt:       now,
			Widgets:       p.Widgets,
		},
	)
	if err != nil {
		return storage.Session{}, err
	}
	a.saveSessionCreds(id, creds)

	if err := a.MonitorStart(id); err != nil {
		// The session exists and is stored; it simply is not polling. Reported
		// rather than rolled back, because deleting it would throw away the
		// binding the operator just made and leave nothing to retry from.
		log.Printf("PresetBind: %s created but not started: %v", id, err)
	}
	return a.findSession(id)
}

// DeletePreset removes one preset from the library.
//
// Sessions already bound to it are untouched: what they poll and what they draw
// was snapshotted when they were bound, so a preset is a file you started from
// rather than a file they depend on. Deleting one cannot silently stop a
// monitoring.
func (a *App) DeletePreset(name string) error {
	path, err := a.resolvePresetPath(name)
	if err != nil {
		return err
	}
	return os.Remove(path)
}

// presetCost turns a preset into the numbers shown before it is bound.
//
// One target. Binding is per equipment, so multiplying here would state a cost
// for a device nobody named.
func presetCost(p preset.Preset) PresetCost {
	c := preset.Estimate(p)
	out := PresetCost{Cost: c, VarbindsPerRequest: snmp.MaxVarbindsPerGet}
	if c.OIDs == 0 {
		return out
	}
	// Ceiling: thirty-one OIDs is two requests, not one and a bit.
	perRound := (c.OIDs + snmp.MaxVarbindsPerGet - 1) / snmp.MaxVarbindsPerGet
	out.RequestsPerDay = c.PollsPerDay * perRound
	return out
}

func nonNilErrors(errs []preset.Error) []preset.Error {
	if errs == nil {
		return []preset.Error{}
	}
	return errs
}
