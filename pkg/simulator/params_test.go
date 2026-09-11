package simulator

import (
	"testing"
)

// notificationGaps are the objects m's notifications carry that a device
// built from id lacks.
func notificationGaps(t *testing.T, m model, id Identity) map[string]bool {
	t.Helper()
	tr, err := newTree(m.build(id))
	if err != nil {
		t.Fatalf("%s with %v: %v", m.ID, id.Params, err)
	}
	gaps := map[string]bool{}
	for _, n := range m.notifications {
		for _, o := range n.Objects {
			if id, err := parseOID(o); err == nil && tr.get(id) == nil && tr.notification(id) == nil {
				gaps[n.Name+" "+o] = true
			}
		}
	}
	return gaps
}

// Every parameter of every model builds, at each of its bounds, a device whose
// objects make a tree — and whose notifications carry nothing it lacks that
// the model as catalogued has.
func TestEveryModelParameterBuildsAtItsBounds(t *testing.T) {
	for _, m := range models {
		for _, p := range m.Params {
			if p.Default < p.Min || p.Default > p.Max {
				t.Errorf("%s: %s defaults to %d, outside %d to %d", m.ID, p.Name, p.Default, p.Min, p.Max)
			}
			usual := notificationGaps(t, m, Identity{Name: "dev", Seed: 11})
			for _, n := range []int{p.Min, p.Max} {
				for gap := range notificationGaps(t, m, Identity{Name: "dev", Seed: 11, Params: map[string]int{p.Name: n}}) {
					if !usual[gap] {
						t.Errorf("%s with %s=%d: %s, which the device lacks", m.ID, p.Name, n, gap)
					}
				}
			}
		}
	}
}

// A number given changes what a walk finds — the ports, the disks, the
// outlets, the processors — and a switch given fewer ports keeps its uplinks
// where its model has them.
func TestAParameterSizesItsTable(t *testing.T) {
	for _, c := range []struct {
		model, param string
		n            int
		column       string
		want         int
	}{
		{"cisco-catalyst-24", "ports", 8, "1.3.6.1.2.1.2.2.1.2", 8 + 3}, // two uplinks and VLAN 1 besides
		{"synology-nas", "disks", 2, "1.3.6.1.4.1.6574.2.1.1.2", 2},
		{"apc-rack-pdu", "outlets", 12, "1.3.6.1.4.1.318.1.1.12.3.5.1.1.2", 12},
		{"linux-server", "cpus", 8, "1.3.6.1.2.1.25.3.3.1.2", 8},
	} {
		m, _ := findModel(c.model)
		tr, err := newTree(m.build(Identity{Name: "dev", Seed: 5, Params: map[string]int{c.param: c.n}}))
		if err != nil {
			t.Fatal(err)
		}
		col, _ := parseOID(c.column)
		got := 0
		for _, e := range tr.entries {
			if e.oid.hasPrefix(col) {
				got++
			}
		}
		if got != c.want {
			t.Errorf("%s with %d %s: %d rows under %s, want %d", c.model, c.n, c.param, got, c.column, c.want)
		}
		if c.model == "cisco-catalyst-24" {
			uplink, _ := parseOID("1.3.6.1.2.1.2.2.1.2.25")
			if e := tr.get(uplink); e == nil || e.val.read(clock{}) != "GigabitEthernet0/1" {
				t.Errorf("with %d ports, ifDescr.25 is %v", c.n, e)
			}
		}
	}
}

// A device is refused a number its model does not take, or one out of bounds.
func TestADevicesParametersAreChecked(t *testing.T) {
	d := Device{Name: "sw", Model: "cisco-catalyst-24", Address: "127.0.0.2", Port: 1161, Versions: []string{"v2c"},
		Community: "public", EngineID: "8000000903aabbccddeeff"}
	for _, c := range []struct {
		params map[string]int
		ok     bool
	}{
		{nil, true},
		{map[string]int{"ports": 12}, true},
		{map[string]int{"ports": 1}, false},
		{map[string]int{"ports": 25}, false},
		{map[string]int{"disks": 2}, false},
	} {
		d.Params = c.params
		if err := d.Validate(); (err == nil) != c.ok {
			t.Errorf("%v: %v", c.params, err)
		}
	}
}
