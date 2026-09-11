package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"SnmpLens/pkg/secrets"
	"SnmpLens/pkg/simulator"

	"github.com/gosnmp/gosnmp"
)

func simApp(t *testing.T) (*App, *stubStore) {
	t.Helper()
	store := &stubStore{}
	a := &App{secrets: store, sim: newSimulatorService(t.TempDir())}
	t.Cleanup(a.sim.fleet.StopAll)
	return a, store
}

func simDevice() simulator.Device {
	return simulator.Device{
		Name: "srv-01", Model: "linux-server", Address: "127.0.0.2", Port: 16161,
		Versions: []string{"v2c", "v3"}, Community: "s3cr3t-community",
		Users: []simulator.User{{Name: "ops", SecLevel: "AuthPriv",
			AuthProto: "SHA256", AuthPass: "auth-s3cr3t", PrivProto: "AES", PrivPass: "priv-s3cr3t"}},
	}
}

var simSecrets = []string{"s3cr3t-community", "auth-s3cr3t", "priv-s3cr3t"}

// The device file never holds a secret, and neither does the list the renderer
// is given; the credential store holds them all, and gives them back only by
// name.
func TestASimulatedDevicesSecretsStayInTheStore(t *testing.T) {
	a, store := simApp(t)
	saved, err := a.SimulatorSaveDevice(simDevice())
	if err != nil {
		t.Fatal(err)
	}

	file, err := os.ReadFile(a.sim.path)
	if err != nil {
		t.Fatal(err)
	}
	list, err := json.Marshal(a.ListSimulatedDevices())
	if err != nil {
		t.Fatal(err)
	}
	kept := store.values[secrets.SimulatorDeviceRef(saved.ID)]
	for _, secret := range simSecrets {
		if bytes.Contains(file, []byte(secret)) {
			t.Errorf("%s holds %q", simulatorFile, secret)
		}
		if bytes.Contains(list, []byte(secret)) {
			t.Errorf("the device list holds %q", secret)
		}
		if !strings.Contains(kept, secret) {
			t.Errorf("the credential store lacks %q", secret)
		}
	}

	creds, err := a.SimulatorDeviceCredentials(saved.ID)
	if err != nil || creds.Community != "s3cr3t-community" || creds.Users["ops"].PrivPass != "priv-s3cr3t" {
		t.Errorf("credentials: %+v, %v", creds, err)
	}

	again := newSimulatorService(filepath.Dir(a.sim.path))
	if len(again.devices) != 1 || again.devices[0].ID != saved.ID || again.devices[0].Community != "" {
		t.Errorf("read back: %+v", again.devices)
	}
}

// The loopback rule holds when a device is saved, before anything is written.
func TestANetworkAddressIsRefusedAtSave(t *testing.T) {
	a, store := simApp(t)
	d := simDevice()
	d.Address = "0.0.0.0"
	if _, err := a.SimulatorSaveDevice(d); !errors.Is(err, simulator.ErrNotLoopback) {
		t.Fatalf("err = %v, want ErrNotLoopback", err)
	}
	if _, err := os.Stat(a.sim.path); !errors.Is(err, os.ErrNotExist) {
		t.Error("the refused device was written to the file")
	}
	if len(store.values) != 0 {
		t.Error("the refused device's credentials were kept")
	}
}

// The engine ID and the boot count are the backend's to give: a renderer that
// sent its own would otherwise choose the identity managers localise their keys
// to, or rewind the count that keeps old messages out of the time window.
func TestTheEngineIsTheBackendsToGive(t *testing.T) {
	a, _ := simApp(t)
	d := simDevice()
	d.EngineID, d.EngineBoots = "8000000001", 99
	saved, err := a.SimulatorSaveDevice(d)
	if err != nil {
		t.Fatal(err)
	}
	if saved.EngineID == d.EngineID || saved.EngineBoots != 0 {
		t.Errorf("a new device took the renderer's engine: %s, boots %d", saved.EngineID, saved.EngineBoots)
	}
	edit := simDevice()
	edit.ID, edit.Name, edit.EngineID, edit.EngineBoots = saved.ID, "renamed", "8000000002", 0
	a.sim.devices[0].EngineBoots = 5
	updated, err := a.SimulatorSaveDevice(edit)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "renamed" || updated.EngineID != saved.EngineID || updated.EngineBoots != 5 {
		t.Errorf("an edit changed the engine: %+v", updated)
	}
}

func TestTwoSimulatedDevicesCannotShareAnAddress(t *testing.T) {
	a, _ := simApp(t)
	if _, err := a.SimulatorSaveDevice(simDevice()); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SimulatorSaveDevice(simDevice()); err == nil {
		t.Error("a second device was configured on the first one's address")
	}
}

func TestDeletingADeviceForgetsItsSecrets(t *testing.T) {
	a, store := simApp(t)
	saved, err := a.SimulatorSaveDevice(simDevice())
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SimulatorDeleteDevice(saved.ID); err != nil {
		t.Fatal(err)
	}
	if len(a.ListSimulatedDevices()) != 0 {
		t.Error("the deleted device is still listed")
	}
	if _, kept := store.values[secrets.SimulatorDeviceRef(saved.ID)]; kept {
		t.Error("the deleted device's credentials remain in the store")
	}
}

// Every start raises the boot count and keeps it, and the device then answers
// with the community the store holds.
func TestEveryStartRaisesTheBoots(t *testing.T) {
	a, _ := simApp(t)
	var saved SimulatedDevice
	var err error
	// A port found free can be taken before the device binds it, since
	// `go test ./...` runs every package at once; try another.
	for attempt := 0; attempt < 5; attempt++ {
		c, lerr := net.ListenPacket("udp", "127.0.0.1:0")
		if lerr != nil {
			t.Skipf("no UDP socket: %v", lerr)
		}
		d := simDevice()
		d.ID = saved.ID
		d.Address, d.Port = "127.0.0.1", c.LocalAddr().(*net.UDPAddr).Port
		c.Close()
		if saved, err = a.SimulatorSaveDevice(d); err != nil {
			t.Fatal(err)
		}
		if err = a.SimulatorStartDevice(saved.ID); err == nil {
			break
		}
	}
	if err != nil {
		t.Fatalf("the device would not start: %v", err)
	}

	g := &gosnmp.GoSNMP{Target: saved.Address, Port: uint16(saved.Port), Community: "s3cr3t-community",
		Version: gosnmp.Version2c, Timeout: 2 * time.Second}
	if err := g.Connect(); err != nil {
		t.Fatal(err)
	}
	defer g.Conn.Close()
	res, err := g.Get([]string{".1.3.6.1.2.1.1.5.0"})
	if err != nil {
		t.Fatal(err)
	}
	if name, _ := res.Variables[0].Value.([]byte); string(name) != "srv-01" {
		t.Errorf("sysName = %q", name)
	}

	boots := func() uint32 {
		again := newSimulatorService(filepath.Dir(a.sim.path))
		return again.devices[0].EngineBoots
	}
	if b := boots(); b != 1 {
		t.Errorf("after one start the file holds boots %d, want 1", b)
	}
	if err := a.SimulatorStopDevice(saved.ID); err != nil {
		t.Fatal(err)
	}
	if err := a.SimulatorStartDevice(saved.ID); err != nil {
		t.Fatal(err)
	}
	if b := boots(); b != 2 {
		t.Errorf("after two starts the file holds boots %d, want 2", b)
	}
	if !a.ListSimulatedDevices()[0].Running {
		t.Error("a started device is listed as stopped")
	}
}

// A new device goes where nothing else is — and on Windows at port 161, which
// needs no privileges there.
func TestANewDeviceIsOfferedAnAddressOfItsOwn(t *testing.T) {
	free := func(string) bool { return true }
	taken := []string{"127.0.0.2:161", "127.0.0.3:161"}
	if got := suggestAddress("windows", taken, free); got != (SimulatorAddress{"127.0.0.4", 161}) {
		t.Errorf("windows: %+v", got)
	}
	if got := suggestAddress("linux", nil, free); got != (SimulatorAddress{"127.0.0.2", 1161}) {
		t.Errorf("linux: %+v", got)
	}
	// macOS answers on 127.0.0.1 alone unless aliases were added: a bind anywhere
	// else fails, and the device goes to 127.0.0.1 at the next free port...
	onlyLoopbackOne := func(listen string) bool { return strings.HasPrefix(listen, "127.0.0.1:") }
	if got := suggestAddress("darwin", []string{"127.0.0.1:1161"}, onlyLoopbackOne); got != (SimulatorAddress{"127.0.0.1", 1162}) {
		t.Errorf("darwin: %+v", got)
	}
	// ...and with aliases it gets an address of its own, like the others.
	if got := suggestAddress("darwin", nil, free); got != (SimulatorAddress{"127.0.0.2", 1161}) {
		t.Errorf("darwin with aliases: %+v", got)
	}
	busy := func(listen string) bool { return listen != "127.0.0.2:1161" }
	if got := suggestAddress("linux", nil, busy); got.Address != "127.0.0.3" {
		t.Errorf("an address something else holds was offered: %+v", got)
	}
}
