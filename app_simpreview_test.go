package main

import (
	"path/filepath"
	"testing"

	"SnmpLens/pkg/simulator"
)

// A device is previewed as the editor holds it — saved or not, and with no MIB
// service to name its objects — and what it was given of its own is listed,
// kept with it, and refused at save when the preview refuses it.
func TestASimulatedDeviceIsPreviewedAndKeepsItsOwnValues(t *testing.T) {
	a, _ := simApp(t)
	d := simDevice()
	d.Params = map[string]int{"cpus": 4}
	d.Overrides = []simulator.Override{{OID: "1.3.6.1.2.1.1.4.0", Type: "OctetString", Value: "ops@example.net"}}

	cpus, err := a.SimulatorPreview(d, "1.3.6.1.2.1.25.3.3.1.2")
	if err != nil || cpus.Total != 4 || len(cpus.Rows) != 4 {
		t.Fatalf("four processors: %+v, %v", cpus, err)
	}
	contact, err := a.SimulatorPreview(d, "1.3.6.1.2.1.1.4.0")
	if err != nil || len(contact.Rows) != 1 || contact.Rows[0].Value != "ops@example.net" {
		t.Errorf("sysContact: %+v, %v", contact, err)
	}

	saved, err := a.SimulatorSaveDevice(d)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Params["cpus"] != 4 || len(saved.Overrides) != 1 {
		t.Errorf("the list shows %v and %v", saved.Params, saved.Overrides)
	}
	again := newSimulatorService(filepath.Dir(a.sim.path))
	if got := again.devices[0]; got.Params["cpus"] != 4 || len(got.Overrides) != 1 || got.Overrides[0].Value != "ops@example.net" {
		t.Errorf("read back as %v and %v", got.Params, got.Overrides)
	}

	d.ID = saved.ID
	d.Overrides = append(d.Overrides, simulator.Override{OID: "1.3.6.1.2.1.11.1.0", Type: "Counter32", Value: "1"})
	if _, err := a.SimulatorPreview(d, ""); err == nil {
		t.Error("a value the agent owns was previewed")
	}
	if _, err := a.SimulatorSaveDevice(d); err == nil {
		t.Error("a value the agent owns was saved")
	}
}
