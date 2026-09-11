package main

import (
	"path/filepath"
	"testing"
	"time"

	"SnmpLens/pkg/simulator"

	"github.com/gosnmp/gosnmp"
)

// Faults are set on a running device without restarting it, kept with it — an
// edit in the editor keeps them — and cleared the same way.
func TestFaultsAreSetWithoutARestart(t *testing.T) {
	a, _ := simApp(t)
	saved := startSimulated(t, a, simDevice())
	boots := listedDevice(a, saved.ID).EngineBoots
	if err := a.SimulatorSetFaults(saved.ID, simulator.Faults{Mute: true}); err != nil {
		t.Fatal(err)
	}
	g := &gosnmp.GoSNMP{Target: saved.Address, Port: uint16(saved.Port), Community: simDevice().Community,
		Version: gosnmp.Version2c, Timeout: 300 * time.Millisecond}
	if err := g.Connect(); err != nil {
		t.Fatal(err)
	}
	defer g.Conn.Close()
	if _, err := g.Get([]string{".1.3.6.1.2.1.1.5.0"}); err == nil {
		t.Error("a mute device answered")
	}
	now := listedDevice(a, saved.ID)
	if !now.Running || now.EngineBoots != boots || !now.Faults.Mute {
		t.Errorf("after the fault: running %v, boots %d after %d, faults %+v", now.Running, now.EngineBoots, boots, now.Faults)
	}
	if again := newSimulatorService(filepath.Dir(a.sim.path)); !again.devices[0].Faults.Mute {
		t.Error("the fault was not kept with the device")
	}

	d := simDevice()
	d.ID, d.Address, d.Port = saved.ID, saved.Address, saved.Port
	d.Traps.Destinations[0].ID = now.Traps.Destinations[0].ID
	if _, err := a.SimulatorSaveDevice(d); err != nil {
		t.Fatal(err)
	}
	if !listedDevice(a, saved.ID).Faults.Mute {
		t.Error("an edit cleared the faults")
	}
	if err := a.SimulatorSetFaults(saved.ID, simulator.Faults{}); err != nil {
		t.Fatal(err)
	}
	if v := simGet(t, listedDevice(a, saved.ID), simDevice().Community, ".1.3.6.1.2.1.1.5.0"); string(v.Value.([]byte)) != "srv-01" {
		t.Errorf("cleared, the device answers %v", v.Value)
	}
	if err := a.SimulatorSetFaults(saved.ID, simulator.Faults{LossPercent: 101}); err == nil {
		t.Error("a loss of 101 % was set")
	}
}

// A device answers with the location and contact it was given, in place of its
// model's; one given none answers its model's.
func TestADeviceHasItsOwnLocationAndContact(t *testing.T) {
	a, _ := simApp(t)
	d := simDevice()
	d.Location, d.Contact = "Lab, rack B4", "noc@lab.example"
	own := startSimulated(t, a, d)
	if v := simGet(t, own, d.Community, ".1.3.6.1.2.1.1.6.0"); string(v.Value.([]byte)) != "Lab, rack B4" {
		t.Errorf("sysLocation: %v", v.Value)
	}
	if v := simGet(t, own, d.Community, ".1.3.6.1.2.1.1.4.0"); string(v.Value.([]byte)) != "noc@lab.example" {
		t.Errorf("sysContact: %v", v.Value)
	}
	models := startSimulated(t, a, simDevice())
	if v := simGet(t, models, d.Community, ".1.3.6.1.2.1.1.6.0"); len(v.Value.([]byte)) == 0 || string(v.Value.([]byte)) == "Lab, rack B4" {
		t.Errorf("a device given no location answers %q", v.Value)
	}
}

// A device set to start with the application starts with it, and one that is
// not does not.
func TestADeviceStartsWithTheApplication(t *testing.T) {
	a, store := simApp(t)
	auto := simDevice()
	auto.AutoStart, auto.Address, auto.Port = true, "127.0.0.1", freeSimPort(t)
	autoSaved, err := a.SimulatorSaveDevice(auto)
	if err != nil {
		t.Fatal(err)
	}
	manual := simDevice()
	manual.Address, manual.Port = "127.0.0.1", freeSimPort(t)
	if manual.Port == auto.Port {
		manual.Port++
	}
	manualSaved, err := a.SimulatorSaveDevice(manual)
	if err != nil {
		t.Fatal(err)
	}

	b := &App{secrets: store, sim: newSimulatorService(filepath.Dir(a.sim.path))}
	t.Cleanup(b.sim.fleet.StopAll)
	b.startAutoSimulated()
	if !listedDevice(b, autoSaved.ID).Running || listedDevice(b, manualSaved.ID).Running {
		t.Errorf("after startup: %+v", b.ListSimulatedDevices())
	}
}
