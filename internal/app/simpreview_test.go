package app

import (
	"path/filepath"
	"testing"

	"SnmpLens/pkg/simulator"
)

// A device is previewed as the editor holds it — saved or not, and with no MIB
// service to name its objects — filtered by subtree or to what moves; and what
// it was given of its own is listed, kept with it, and refused at save when the
// preview refuses it.
func TestASimulatedDeviceIsPreviewedAndKeepsItsOwnValues(t *testing.T) {
	a, _ := simApp(t)
	d := simDevice()
	d.Params = map[string]int{"cpus": 4}
	d.Overrides = []simulator.Override{{OID: "1.3.6.1.2.1.1.4.0", Type: "OctetString", Value: "ops@example.net"}}

	cpus, err := a.SimulatorPreview(d, SimulatorPreviewQuery{Filter: "1.3.6.1.2.1.25.3.3.1.2"})
	if err != nil || cpus.Total != 4 || len(cpus.Rows) != 4 || cpus.Rows[0].Behaviour != simulator.BehaviourGauge {
		t.Fatalf("four processors: %+v, %v", cpus, err)
	}
	contact, err := a.SimulatorPreview(d, SimulatorPreviewQuery{Filter: ".1.3.6.1.2.1.1.4.0"})
	if err != nil || len(contact.Rows) != 1 || contact.Rows[0].Value != "ops@example.net" {
		t.Errorf("sysContact: %+v, %v", contact, err)
	}
	all, _ := a.SimulatorPreview(d, SimulatorPreviewQuery{})
	moving, err := a.SimulatorPreview(d, SimulatorPreviewQuery{DynamicOnly: true})
	if err != nil || moving.Total == 0 || moving.Total >= all.Total {
		t.Errorf("%d objects move of %d: %v", moving.Total, all.Total, err)
	}
	for _, r := range moving.Rows {
		if r.Behaviour == simulator.BehaviourStatic {
			t.Errorf("%s is static and was shown as moving", r.OID)
		}
	}
	if len(all.Rows) != min(all.Total, simulator.MaxPreviewRows) {
		t.Errorf("a page of %d rows for %d objects", len(all.Rows), all.Total)
	}
	// Text that is not an OID is looked for in the names — none without MIBs.
	if named, err := a.SimulatorPreview(d, SimulatorPreviewQuery{Filter: "ifDescr"}); err != nil || named.Total != 0 {
		t.Errorf("a name searched with no MIB loaded: %+v, %v", named, err)
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
	if _, err := a.SimulatorPreview(d, SimulatorPreviewQuery{}); err == nil {
		t.Error("a value the agent owns was previewed")
	}
	if _, err := a.SimulatorSaveDevice(d); err == nil {
		t.Error("a value the agent owns was saved")
	}
}
