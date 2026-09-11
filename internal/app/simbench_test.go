package app

import (
	"bytes"
	"fmt"
	"net"
	"slices"
	"strings"
	"testing"
)

// freeSimPort is a UDP port on 127.0.0.1 that nothing holds right now.
func freeSimPort(t *testing.T) int {
	t.Helper()
	c, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("no UDP socket: %v", err)
	}
	defer c.Close()
	return c.LocalAddr().(*net.UDPAddr).Port
}

func listedDevice(a *App, id string) SimulatedDevice {
	for _, d := range a.ListSimulatedDevices() {
		if d.ID == id {
			return d
		}
	}
	return SimulatedDevice{}
}

// A bench exported without its passwords holds none of them and says so;
// exported with them it holds them and says that — and neither holds what is
// this machine's: the device's ID, its engine, its boots.
func TestABenchIsExportedWithOrWithoutItsPasswords(t *testing.T) {
	a, _ := simApp(t)
	saved, err := a.SimulatorSaveDevice(simDevice())
	if err != nil {
		t.Fatal(err)
	}
	bare, name, err := a.exportDevices(nil, false)
	if err != nil || name != "srv-01.json" {
		t.Fatalf("%q, %v", name, err)
	}
	full, _, err := a.exportDevices([]string{saved.ID}, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range simSecrets {
		if bytes.Contains(bare, []byte(secret)) {
			t.Errorf("exported without passwords, the file holds %q", secret)
		}
		if !bytes.Contains(full, []byte(secret)) {
			t.Errorf("exported with passwords, the file lacks %q", secret)
		}
	}
	if !bytes.Contains(bare, []byte(`"secrets": "omitted"`)) || !bytes.Contains(full, []byte(`"secrets": "included"`)) {
		t.Error("a file does not say whether it holds the passwords")
	}
	for _, mine := range []string{saved.ID, saved.EngineID, "engineBoots"} {
		if bytes.Contains(full, []byte(mine)) {
			t.Errorf("the file holds %s", mine)
		}
	}
	if _, _, err := a.exportDevices([]string{"gone"}, false); err == nil {
		t.Error("a device that is not there was exported")
	}
}

// Imported, a bench is new devices of this machine: IDs and engines of their
// own, another address for one whose own is taken, their passwords in the
// store — and they answer.
func TestABenchIsImportedAsNewDevices(t *testing.T) {
	a, _ := simApp(t)
	original := startSimulated(t, a, simDevice())
	full, _, err := a.exportDevices(nil, true)
	if err != nil {
		t.Fatal(err)
	}
	res := a.importDevices("bench.json", full)
	if len(res) != 1 || !res[0].Success || len(res[0].Warnings) != 1 || res[0].Warnings[0].Key != "addressMoved" {
		t.Fatalf("%+v", res)
	}
	imported := listedDevice(a, res[0].ID)
	if imported.ID == "" || imported.ID == original.ID || imported.EngineID == original.EngineID || imported.EngineBoots != 0 ||
		(imported.Address == original.Address && imported.Port == original.Port) {
		t.Fatalf("imported as %+v, from %+v", imported, original)
	}
	creds, err := a.SimulatorDeviceCredentials(imported.ID)
	if err != nil || creds.Community != "s3cr3t-community" || creds.Users["ops"].AuthPass != "auth-s3cr3t" {
		t.Fatalf("credentials: %+v, %v", creds, err)
	}
	if err := a.SimulatorStartDevice(imported.ID); err != nil {
		t.Fatal(err)
	}
	if v := simGet(t, listedDevice(a, imported.ID), "s3cr3t-community", ".1.3.6.1.2.1.1.5.0"); string(v.Value.([]byte)) != "srv-01" {
		t.Errorf("the imported device answers sysName %v", v.Value)
	}
}

// A bench exported without its passwords is imported all the same, and says
// so; a device of it starts once it is given them.
func TestABenchWithoutPasswordsIsImportedToBeCompleted(t *testing.T) {
	a, _ := simApp(t)
	saved, err := a.SimulatorSaveDevice(simDevice())
	if err != nil {
		t.Fatal(err)
	}
	bare, _, err := a.exportDevices(nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SimulatorDeleteDevice(saved.ID); err != nil {
		t.Fatal(err)
	}
	res := a.importDevices("bench.json", bare)
	if len(res) != 1 || !res[0].Success ||
		!slices.ContainsFunc(res[0].Warnings, func(w SimulatorModelWarning) bool { return w.Key == "noSecrets" }) {
		t.Fatalf("%+v", res)
	}
	id := res[0].ID
	if err := a.SimulatorStartDevice(id); err == nil || !strings.Contains(err.Error(), "community") {
		t.Fatalf("a device with no community started, or said %v", err)
	}
	d := simDevice()
	d.ID, d.Address, d.Port = id, "127.0.0.1", freeSimPort(t)
	d.Traps.Destinations[0].ID = listedDevice(a, id).Traps.Destinations[0].ID
	if _, err := a.SimulatorSaveDevice(d); err != nil {
		t.Fatal(err)
	}
	if err := a.SimulatorStartDevice(id); err != nil {
		t.Fatal(err)
	}
}

// A file is refused as a whole when it is not one of devices, and a device of
// it on its own when it cannot be made here; the others are kept.
func TestADeviceFileIsReadWithCare(t *testing.T) {
	a, _ := simApp(t)
	for _, c := range []struct{ raw, want string }{
		{`{"kind": "snmplens-simulator-model", "formatVersion": 1}`, "not a file of simulated devices"},
		{`{"kind": "snmplens-simulated-devices", "formatVersion": 2}`, "format version 2"},
		{`{"kind": "snmplens-simulated-devices", "formatVersion": 1, "secrets": "omitted", "devices": [], "extra": 1}`, `unknown field "extra"`},
		{`{"kind": "snmplens-simulated-devices", "formatVersion": 1, "secrets": "maybe", "devices": [{}]}`, `"secrets"`},
		{`{"kind": "snmplens-simulated-devices", "formatVersion": 1, "secrets": "omitted", "devices": []}`, "no device"},
		{`not json`, "line 1"},
	} {
		res := a.importDevices("f.json", []byte(c.raw))
		if len(res) != 1 || res[0].Success || res[0].Name != "f.json" || !strings.Contains(res[0].Error, c.want) {
			t.Errorf("%s: %+v, want %q", c.raw, res, c.want)
		}
	}
	port := freeSimPort(t)
	raw := fmt.Sprintf(`{"kind": "snmplens-simulated-devices", "formatVersion": 1, "secrets": "included", "devices": [
		{"name": "gone", "model": "custom:not-here", "address": "127.0.0.1", "port": %[1]d, "versions": ["v2c"], "community": "public", "traps": {}},
		{"name": "outside", "model": "linux-server", "address": "192.0.2.1", "port": 161, "versions": ["v2c"], "community": "public", "traps": {}},
		{"name": "kept", "model": "linux-server", "address": "127.0.0.1", "port": %[1]d, "versions": ["v2c"], "community": "public", "traps": {}}]}`, port)
	res := a.importDevices("bench.json", []byte(raw))
	if len(res) != 3 || res[0].Success || !strings.Contains(res[0].Error, "not a device model") ||
		res[1].Success || res[1].Error == "" || res[2].Name != "kept" || !res[2].Success {
		t.Fatalf("%+v", res)
	}
	if got := a.ListSimulatedDevices(); len(got) != 1 || got[0].Name != "kept" {
		t.Errorf("the devices are %+v", got)
	}
}

// A device duplicated is a device of its own — ID, engine and address — with
// the passwords of the one it copies; a running device restarted comes back
// with one boot more, and a stopped one is not restarted.
func TestADeviceIsDuplicatedAndRestarted(t *testing.T) {
	a, _ := simApp(t)
	src := startSimulated(t, a, simDevice())
	dup, err := a.SimulatorDuplicateDevice(src.ID, "srv-01 (copy)")
	if err != nil {
		t.Fatal(err)
	}
	if dup.ID == src.ID || dup.EngineID == src.EngineID || dup.Running || dup.Name != "srv-01 (copy)" ||
		(dup.Address == src.Address && dup.Port == src.Port) {
		t.Fatalf("duplicated as %+v", dup)
	}
	creds, err := a.SimulatorDeviceCredentials(dup.ID)
	if err != nil || creds.Community != "s3cr3t-community" || creds.Users["ops"].PrivPass != "priv-s3cr3t" {
		t.Errorf("the copy's credentials: %+v, %v", creds, err)
	}
	boots := listedDevice(a, src.ID).EngineBoots
	if err := a.SimulatorRestartDevice(src.ID); err != nil {
		t.Fatal(err)
	}
	if now := listedDevice(a, src.ID); !now.Running || now.EngineBoots != boots+1 {
		t.Errorf("restarted: running %v, boots %d after %d", now.Running, now.EngineBoots, boots)
	}
	if err := a.SimulatorRestartDevice(dup.ID); err == nil {
		t.Error("a stopped device was restarted")
	}
}
