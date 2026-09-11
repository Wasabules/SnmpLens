package simulator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosnmp/gosnmp"
)

func linuxObjects() []Object {
	return linuxServer.build(Identity{Name: "srv-01", Seed: 42})
}

// Every OID the bundled "Interfaces" and "Host resources" presets poll is one
// the Linux model answers, for every instance the preset's own discovery walk
// finds on it. Read from the preset files themselves, so a preset that grows a
// widget the model cannot feed fails here rather than as an empty chart.
func TestTheLinuxModelFeedsItsPresets(t *testing.T) {
	tr, err := newTree(linuxObjects())
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"interfaces.json", "host-resources.json"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "presets", file))
		if err != nil {
			t.Fatal(err)
		}
		var p struct {
			Widgets []struct {
				Title    string   `json:"title"`
				OIDs     []string `json:"oids"`
				Discover *struct {
					Walk string `json:"walk"`
				} `json:"discover"`
			} `json:"widgets"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		if len(p.Widgets) == 0 {
			t.Fatalf("%s has no widgets; the test reads the wrong shape", file)
		}
		for _, w := range p.Widgets {
			instances := []string{""}
			if w.Discover != nil {
				column, err := parseOID(w.Discover.Walk)
				if err != nil {
					t.Fatal(err)
				}
				instances = nil
				for e := tr.next(column, nil); e != nil && e.oid.hasPrefix(column); e = tr.next(e.oid, nil) {
					instances = append(instances, strings.TrimPrefix(e.oid[len(column):].String(), "."))
				}
				if len(instances) == 0 {
					t.Errorf("%s, %q: walking %s finds nothing", file, w.Title, w.Discover.Walk)
					continue
				}
			}
			for _, template := range w.OIDs {
				for _, instance := range instances {
					name := strings.ReplaceAll(template, "{#}", instance)
					if id, err := parseOID(name); err != nil || tr.get(id) == nil {
						t.Errorf("%s, %q: %s is not answered", file, w.Title, name)
					}
				}
			}
		}
	}
}

// The whole model goes out over the wire: every value encodes, and a walk
// visits every object. A value gosnmp cannot encode would otherwise fail the one
// response that carries it, as a timeout.
func TestTheLinuxModelServesAWalk(t *testing.T) {
	objects := linuxObjects()
	a := startAgent(t, Config{Versions: []string{"v2c"}, Community: "public", Objects: objects})
	g := connect(t, manager(a, gosnmp.Version2c, "public"))

	n := 0
	err := g.BulkWalk(".1.3.6.1", func(gosnmp.SnmpPDU) error {
		n++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != len(objects) {
		t.Errorf("walked %d objects of %d", n, len(objects))
	}
	res, err := g.Get([]string{".1.3.6.1.2.1.1.5.0"})
	if err != nil || text(res.Variables[0]) != "srv-01" {
		t.Errorf("sysName: %v, %v", res, err)
	}
	if s := a.Stats(); s.Faults != 0 {
		t.Errorf("%d answers failed to encode", s.Faults)
	}
}

// Two servers of one model are two different machines.
func TestTwoDevicesOfOneModelDiffer(t *testing.T) {
	one := linuxServer.build(Identity{Name: "a", Seed: 1})
	two := linuxServer.build(Identity{Name: "b", Seed: 2})
	mac := func(objects []Object) string {
		for _, o := range objects {
			if o.OID == "1.3.6.1.2.1.2.2.1.6.2" {
				return string(o.Value.read(clock{}).([]byte))
			}
		}
		return ""
	}
	if mac(one) == "" || mac(one) == mac(two) {
		t.Error("two simulated servers share eth0's MAC address")
	}
}
