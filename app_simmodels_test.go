package main

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"fmt"
	"hash/crc32"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"SnmpLens/pkg/simulator"

	"github.com/gosnmp/gosnmp"
)

// The example model the documentation shows, which pkg/simulator's own tests
// read too.
func exampleModel(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("pkg", "simulator", "testdata", "custom-model.json"))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func pngIcon(t *testing.T, side int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, side, side))
	for i := range img.Pix {
		img.Pix[i] = 0x7f
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type zipEntry struct {
	name string
	data []byte
}

func writeZip(t *testing.T, name string, entries ...zipEntry) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		w, err := zw.Create(e.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(e.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return writeModelFile(t, name, buf.Bytes())
}

// lyingZip is an archive whose one entry says it unpacks to claimed octets and
// unpacks to all of content: what a decompression bomb looks like.
func lyingZip(t *testing.T, entry string, content []byte, claimed uint64) string {
	t.Helper()
	var packed bytes.Buffer
	fw, err := flate.NewWriter(&packed, flate.BestCompression)
	if err != nil {
		t.Fatal(err)
	}
	fw.Write(content)
	fw.Close()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.CreateRaw(&zip.FileHeader{Name: entry, Method: zip.Deflate, CRC32: crc32.ChecksumIEEE(content),
		CompressedSize64: uint64(packed.Len()), UncompressedSize64: claimed})
	if err != nil {
		t.Fatal(err)
	}
	w.Write(packed.Bytes())
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return writeModelFile(t, "bomb.zip", buf.Bytes())
}

func writeModelFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// modelApp is simApp, with the custom models put back as they were after.
func modelApp(t *testing.T) *App {
	t.Helper()
	a, _ := simApp(t)
	t.Cleanup(func() { _ = simulator.SetCustomModels(nil) })
	return a
}

func onlyResult(t *testing.T, results []SimulatorModelImportResult) SimulatorModelImportResult {
	t.Helper()
	if len(results) != 1 {
		t.Fatalf("%d results: %+v", len(results), results)
	}
	return results[0]
}

func listedModel(a *App, id string) *simulator.ModelInfo {
	for _, m := range a.ListSimulatorModels() {
		if m.ID == id {
			return &m
		}
	}
	return nil
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

// crac is a device of the example model, answering v2c alone.
func crac() simulator.Device {
	d := simDevice()
	d.Name, d.Model, d.Versions, d.Users, d.Traps = "crac-01", "custom:acme-crac", []string{"v2c"}, nil, simulator.Traps{}
	return d
}

func simGet(t *testing.T, dev SimulatedDevice, community, oid string) gosnmp.SnmpPDU {
	t.Helper()
	g := &gosnmp.GoSNMP{Target: dev.Address, Port: uint16(dev.Port), Community: community,
		Version: gosnmp.Version2c, Timeout: 2 * time.Second, Retries: 1}
	if err := g.Connect(); err != nil {
		t.Fatal(err)
	}
	defer g.Conn.Close()
	res, err := g.Get([]string{oid})
	if err != nil {
		t.Fatal(err)
	}
	return res.Variables[0]
}

// A model and its icon, imported from a ZIP, make a device that answers what
// the model says; the picker is given the icon as the PNG it is; and what is
// kept is the model and its icon under the model's own id, nothing else from
// the archive.
func TestAModelImportedFromAZipMakesADevice(t *testing.T) {
	a := modelApp(t)
	archive := writeZip(t, "acme.zip",
		zipEntry{"acme/custom-model.json", exampleModel(t)},
		zipEntry{"acme/acme-crac.png", pngIcon(t, 64)},
		zipEntry{"acme/README.md", []byte("# Acme")},
		zipEntry{"__MACOSX/acme/._acme-crac.png", []byte("resource fork")},
	)
	res := onlyResult(t, a.importSimulatorModels([]string{archive}))
	if !res.Success || !res.Icon || res.Replaced || res.ModelID != "custom:acme-crac" || len(res.Warnings) != 0 ||
		res.File != "acme.zip › acme/custom-model.json" {
		t.Fatalf("%+v", res)
	}
	if m := listedModel(a, "custom:acme-crac"); m == nil || !m.Custom || m.Name != "Acme CRAC-40 cooling unit" {
		t.Fatalf("listed as %+v", m)
	}
	icons := a.ListSimulatorModelIcons()
	if len(icons) != 1 || icons[0].ID != "custom:acme-crac" || !strings.HasPrefix(icons[0].Icon, "data:image/png;base64,") {
		t.Fatalf("icons: %+v", icons)
	}
	if got := dirNames(t, a.sim.modelDir); !slices.Equal(got, []string{"acme-crac.json", "acme-crac.png"}) {
		t.Errorf("the model directory holds %v", got)
	}

	saved := startSimulated(t, a, crac())
	if v := simGet(t, saved, crac().Community, ".1.3.6.1.4.1.32473.2.2.1.2.2"); string(v.Value.([]byte)) != "Sensor 2" {
		t.Errorf("the device answers %v", v.Value)
	}
}

// The models kept are read back when the application starts, and a file that no
// longer reads is left out rather than taking the others with it.
func TestTheModelsKeptAreReadAtStartup(t *testing.T) {
	a := modelApp(t)
	onlyResult(t, a.importSimulatorModels([]string{writeModelFile(t, "crac.json", exampleModel(t))}))
	if err := os.WriteFile(filepath.Join(a.sim.modelDir, "broken.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := simulator.SetCustomModels(nil); err != nil {
		t.Fatal(err)
	}
	s := newSimulatorService(filepath.Dir(a.sim.path))
	t.Cleanup(s.fleet.StopAll)
	if len(s.models) != 1 || s.models[0].ID() != "custom:acme-crac" {
		t.Fatalf("read back %d model(s)", len(s.models))
	}
	if !slices.ContainsFunc(simulator.Models(), func(m simulator.ModelInfo) bool { return m.ID == "custom:acme-crac" }) {
		t.Error("the model read back is not the simulator's")
	}
}

// A model imported again replaces the one kept, and the devices made from it
// that are running restart and answer as it now is.
func TestAModelImportedAgainRestartsItsDevices(t *testing.T) {
	a := modelApp(t)
	onlyResult(t, a.importSimulatorModels([]string{writeModelFile(t, "crac.json", exampleModel(t))}))
	saved := startSimulated(t, a, crac())
	boots := a.ListSimulatedDevices()[0].EngineBoots

	changed := bytes.Replace(exampleModel(t), []byte(`"value": "CRAC-40"`), []byte(`"value": "CRAC-41"`), 1)
	// A lone file cannot bring the icon the model names, and says so.
	res := onlyResult(t, a.importSimulatorModels([]string{writeModelFile(t, "crac.json", changed)}))
	if !res.Success || !res.Replaced || len(res.Warnings) != 1 || res.Warnings[0].Key != "iconSkipped" {
		t.Fatalf("%+v", res)
	}
	now := a.ListSimulatedDevices()[0]
	if !now.Running || now.EngineBoots != boots+1 {
		t.Errorf("running %v, boots %d after %d", now.Running, now.EngineBoots, boots)
	}
	if v := simGet(t, saved, crac().Community, ".1.3.6.1.4.1.32473.2.1.0"); string(v.Value.([]byte)) != "CRAC-41" {
		t.Errorf("the restarted device answers %v", v.Value)
	}
}

// A model a device is made from is not deleted; once the device is gone, it is,
// and its icon with it. Only a custom model's ID names a file to delete.
func TestAModelInUseIsNotDeleted(t *testing.T) {
	a := modelApp(t)
	onlyResult(t, a.importSimulatorModels([]string{writeZip(t, "acme.zip",
		zipEntry{"custom-model.json", exampleModel(t)}, zipEntry{"acme-crac.png", pngIcon(t, 32)})}))
	saved, err := a.SimulatorSaveDevice(crac())
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SimulatorDeleteModel("custom:acme-crac"); err == nil || !strings.Contains(err.Error(), "crac-01") {
		t.Fatalf("a model in use: %v", err)
	}
	if err := a.SimulatorDeleteDevice(saved.ID); err != nil {
		t.Fatal(err)
	}
	if err := a.SimulatorDeleteModel("custom:acme-crac"); err != nil {
		t.Fatal(err)
	}
	if got := dirNames(t, a.sim.modelDir); len(got) != 0 {
		t.Errorf("left behind: %v", got)
	}
	if listedModel(a, "custom:acme-crac") != nil {
		t.Error("a deleted model is still listed")
	}
	for _, id := range []string{"linux-server", "custom:../../simulator", "custom:", "custom:A"} {
		if err := a.SimulatorDeleteModel(id); err == nil {
			t.Errorf("%q was taken for a custom model", id)
		}
	}
}

// What an archive or a file can do to an import is bounded, and each refusal
// says what it was.
func TestAnImportIsReadWithinBounds(t *testing.T) {
	model := exampleModel(t)
	spaces := bytes.Repeat([]byte(" "), 2<<20)
	many := make([]zipEntry, 65)
	for i := range many {
		many[i] = zipEntry{fmt.Sprintf("f%02d.txt", i), []byte("x")}
	}
	preset := []byte(`{"formatVersion": 1, "name": "Ports", "intervalSec": 60, "widgets": []}`)
	for _, c := range []struct {
		name, file string
		want       string // in the error, or the key of the one warning
		ok         bool
	}{
		{"an entry larger than it may be", writeZip(t, "big.zip", zipEntry{"model.json", spaces}), "unpacks to more than", false},
		{"an entry that lies about its size", lyingZip(t, "model.json", spaces, 100), "cannot be unpacked", false},
		{"too many entries", writeZip(t, "many.zip", many...), "at most 64", false},
		{"an archive with no model", writeZip(t, "icons.zip", zipEntry{"acme-crac.png", pngIcon(t, 32)}), "holds no model", false},
		{"neither JSON nor ZIP", writeModelFile(t, "notes.txt", []byte("hello")), "neither", false},
		{"a preset", writeModelFile(t, "ports.json", preset), "not a simulator model", false},
		{"an icon that is SVG", writeZip(t, "svg.zip", zipEntry{"m.json", model},
			zipEntry{"acme-crac.png", []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)}), "iconRefused", true},
		{"an icon too large to draw small", writeZip(t, "wide.zip", zipEntry{"m.json", model},
			zipEntry{"acme-crac.png", pngIcon(t, 600)}), "iconRefused", true},
		{"an icon the archive does not hold", writeZip(t, "alone.zip", zipEntry{"m.json", model}), "iconMissing", true},
		{"a lone file naming an icon", writeModelFile(t, "m.json", model), "iconSkipped", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			a := modelApp(t)
			res := onlyResult(t, a.importSimulatorModels([]string{c.file}))
			switch {
			case !c.ok && (res.Success || !strings.Contains(res.Error, c.want)):
				t.Errorf("want an error saying %q: %+v", c.want, res)
			case c.ok && (!res.Success || res.Icon || len(res.Warnings) != 1 || res.Warnings[0].Key != c.want):
				t.Errorf("want the model imported without an icon, and %s: %+v", c.want, res)
			}
		})
	}
}

// An archive's names are never where anything is written: an entry called
// ../../x is read as what it holds and kept under the model's own id.
func TestAnArchiveNameIsNeverAPath(t *testing.T) {
	a := modelApp(t)
	res := onlyResult(t, a.importSimulatorModels([]string{writeZip(t, "outside.zip",
		zipEntry{"../../evil.json", exampleModel(t)}, zipEntry{"../../acme-crac.png", pngIcon(t, 32)})}))
	if !res.Success || !res.Icon {
		t.Fatalf("%+v", res)
	}
	if got := dirNames(t, a.sim.modelDir); !slices.Equal(got, []string{"acme-crac.json", "acme-crac.png"}) {
		t.Errorf("the model directory holds %v", got)
	}
	root := filepath.Dir(a.sim.path)
	for _, dir := range []string{root, filepath.Dir(root)} {
		for _, name := range []string{"evil.json", "acme-crac.png"} {
			if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
				t.Errorf("%s was written in %s", name, dir)
			}
		}
	}
}

// A model file saved by Notepad starts with a byte-order mark, and is read.
func TestAModelFileWithAByteOrderMarkIsRead(t *testing.T) {
	a := modelApp(t)
	res := onlyResult(t, a.importSimulatorModels([]string{
		writeModelFile(t, "m.json", append([]byte("\xef\xbb\xbf"), exampleModel(t)...))}))
	if !res.Success {
		t.Fatalf("%+v", res)
	}
}
