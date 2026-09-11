package simulator

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/gosnmp/gosnmp"
)

// A recording reads back as what the device sent, type by type: an OCTET
// STRING as text when it is printable and in hex when it is not — a MAC, a
// line break, an accent —, every number at its full width. The agent's own
// subtrees are never written, and what holds no value is passed over.
func TestARecordingReadsBackAsWhatWasSent(t *testing.T) {
	sent := []gosnmp.SnmpPDU{
		{Name: ".1.3.6.1.2.1.1.1.0", Type: gosnmp.OctetString, Value: []byte("Linux router | edge")},
		{Name: ".1.3.6.1.2.1.1.2.0", Type: gosnmp.ObjectIdentifier, Value: ".1.3.6.1.4.1.8072.3.2.10"},
		{Name: ".1.3.6.1.2.1.1.3.0", Type: gosnmp.TimeTicks, Value: uint32(8640000)},
		{Name: ".1.3.6.1.2.1.1.4.0", Type: gosnmp.OctetString, Value: []byte("line one\nline two")},
		{Name: ".1.3.6.1.2.1.1.5.0", Type: gosnmp.OctetString, Value: []byte{}},
		{Name: ".1.3.6.1.2.1.1.6.0", Type: gosnmp.OctetString, Value: []byte("Salle serveur n°2")},
		{Name: ".1.3.6.1.2.1.2.2.1.6.1", Type: gosnmp.OctetString, Value: []byte{0, 0x1a, 0x2b, 0x3c, 0x4d, 0x5e}},
		{Name: ".1.3.6.1.2.1.2.2.1.8.1", Type: gosnmp.Integer, Value: -2},
		{Name: ".1.3.6.1.2.1.2.2.1.10.1", Type: gosnmp.Counter32, Value: uint(4294967295)},
		{Name: ".1.3.6.1.2.1.2.2.1.5.1", Type: gosnmp.Gauge32, Value: uint(1000000000)},
		{Name: ".1.3.6.1.2.1.31.1.1.1.6.1", Type: gosnmp.Counter64, Value: uint64(18446744073709551615)},
		{Name: ".1.3.6.1.2.1.4.20.1.1.10.0.0.1", Type: gosnmp.IPAddress, Value: "10.0.0.1"},
		{Name: ".1.3.6.1.2.1.25.1.5.0", Type: gosnmp.Uinteger32, Value: uint32(3)},
		{Name: ".1.3.6.1.4.1.2021.100.1.0", Type: gosnmp.Opaque, Value: []byte{0x9f, 0x78, 0x04, 0x41}},
		// Never written.
		{Name: ".1.3.6.1.2.1.11.1.0", Type: gosnmp.Counter32, Value: uint(123)},
		{Name: ".1.3.6.1.6.3.18.1.1.1.2.112.117.98.108.105.99", Type: gosnmp.OctetString, Value: []byte("public")},
		{Name: ".1.3.6.1.2.1.25.1.7.0", Type: gosnmp.NoSuchObject},
		{Name: ".1.3.6.1.4.1.2021.100.2.0", Type: gosnmp.OpaqueFloat, Value: float32(1.5)},
	}
	want := map[string]typed{
		".1.3.6.1.2.1.1.1.0":             {gosnmp.OctetString, "Linux router | edge"},
		".1.3.6.1.2.1.1.2.0":             {gosnmp.ObjectIdentifier, ".1.3.6.1.4.1.8072.3.2.10"},
		".1.3.6.1.2.1.1.3.0":             {gosnmp.TimeTicks, uint32(8640000)},
		".1.3.6.1.2.1.1.4.0":             {gosnmp.OctetString, []byte("line one\nline two")},
		".1.3.6.1.2.1.1.5.0":             {gosnmp.OctetString, ""},
		".1.3.6.1.2.1.1.6.0":             {gosnmp.OctetString, []byte("Salle serveur n°2")},
		".1.3.6.1.2.1.2.2.1.6.1":         {gosnmp.OctetString, []byte{0, 0x1a, 0x2b, 0x3c, 0x4d, 0x5e}},
		".1.3.6.1.2.1.2.2.1.8.1":         {gosnmp.Integer, -2},
		".1.3.6.1.2.1.2.2.1.10.1":        {gosnmp.Counter32, uint32(4294967295)},
		".1.3.6.1.2.1.2.2.1.5.1":         {gosnmp.Gauge32, uint32(1000000000)},
		".1.3.6.1.2.1.31.1.1.1.6.1":      {gosnmp.Counter64, uint64(18446744073709551615)},
		".1.3.6.1.2.1.4.20.1.1.10.0.0.1": {gosnmp.IPAddress, "10.0.0.1"},
		".1.3.6.1.2.1.25.1.5.0":          {gosnmp.Gauge32, uint32(3)},
		".1.3.6.1.4.1.2021.100.1.0":      {gosnmp.Opaque, []byte{0x9f, 0x78, 0x04, 0x41}},
	}
	var r Recording
	for _, pdu := range sent {
		if err := r.Add(pdu); err != nil {
			t.Fatal(err)
		}
	}
	if r.Objects() != len(want) || r.LeftOut() != 2 {
		t.Errorf("%d recorded and %d left out, want %d and 2", r.Objects(), r.LeftOut(), len(want))
	}
	w, err := readWalk(r.walk.Bytes())
	if err != nil || w.skipped != 0 {
		t.Fatalf("%v, %d skipped, the first at %s", err, w.skipped, w.first)
	}
	got := map[string]typed{}
	for _, e := range w.entries {
		got[e.name] = typed{e.typ, e.val}
	}
	for name, want := range want {
		if g, ok := got[name]; !ok || !reflect.DeepEqual(g, want) {
			t.Errorf("%s reads back as %#v, want %#v", name, g, want)
		}
	}
	if strings.Contains(r.walk.String(), "public") {
		t.Error("the recorded device's community is in the walk")
	}
}

// A recording makes a package an import would accept: its id from its name,
// its identity from what the device answered.
func TestAModelIsMadeFromARecording(t *testing.T) {
	var r Recording
	for _, pdu := range []gosnmp.SnmpPDU{
		{Name: ".1.3.6.1.2.1.1.1.0", Type: gosnmp.OctetString, Value: []byte("Edge router, recorded")},
		{Name: ".1.3.6.1.2.1.1.2.0", Type: gosnmp.ObjectIdentifier, Value: ".1.3.6.1.4.1.32473.1.7"},
		{Name: ".1.3.6.1.2.1.1.3.0", Type: gosnmp.TimeTicks, Value: uint32(360000)},
		{Name: ".1.3.6.1.4.1.32473.2.1.0", Type: gosnmp.Gauge32, Value: uint(41)},
	} {
		if err := r.Add(pdu); err != nil {
			t.Fatal(err)
		}
	}
	files, m, err := RecordedPackage(RecordedModel{Name: "Cœur de réseau — Bâtiment A", Vendor: "Acme",
		Category: "network", Description: "Recorded in a test."}, &r)
	if err != nil {
		t.Fatal(err)
	}
	if m.ID() != "custom:coeur-de-reseau-batiment-a" || m.Name() != "Cœur de réseau — Bâtiment A" || m.m.enterprise != 32473 {
		t.Errorf("%s, %q, enterprise %d", m.ID(), m.Name(), m.m.enterprise)
	}
	var names []string
	for _, f := range files {
		names = append(names, f.Name)
	}
	if !slices.Equal(names, []string{"model.json", "walks/device.snmprec"}) {
		t.Errorf("the package holds %v", names)
	}
	tr, err := newTree(m.m.build(Identity{Name: "edge-01", Seed: 1}))
	if err != nil {
		t.Fatal(err)
	}
	if got := readAt(tr, "1.3.6.1.2.1.1.1.0", clock{}); got != "Edge router, recorded" {
		t.Errorf("sysDescr answers %#v", got)
	}
	if _, _, err := RecordedPackage(RecordedModel{Name: "Nothing"}, &Recording{}); err == nil {
		t.Error("a device that answered nothing made a model")
	}
}

// A model's id is made from its name, and is always one a model file may give.
func TestAModelSlugIsAnID(t *testing.T) {
	for name, want := range map[string]string{
		"Core switch":                "core-switch",
		"  Édge — Router #2 ":        "edge-router-2",
		"Straße Æther":               "strasse-aether",
		"":                           "recorded-device",
		"!!!":                        "recorded-device",
		strings.Repeat("ab ", 30):    strings.TrimRight(strings.Repeat("ab-", 16), "-"),
		"Catalyst 2960-24TT-L (lab)": "catalyst-2960-24tt-l-lab",
	} {
		got := ModelSlug(name)
		if got != want || !customIDPattern.MatchString(got) {
			t.Errorf("%q makes %q, want %q", name, got, want)
		}
	}
}
