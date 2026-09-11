package simulator

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"
)

// packageFiles hands a folder over as the application hands a package: every
// file, by its name under the folder.
func packageFiles(t testing.TB, dir string) []PackageFile {
	t.Helper()
	var files []PackageFile
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		files = append(files, PackageFile{Name: filepath.ToSlash(rel), Data: data})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// readAt is what the tree answers for an OID at a moment, or nil.
func readAt(tr *tree, name string, c clock) any {
	id, err := parseOID(name)
	if err != nil {
		panic(err)
	}
	if e := tr.get(id); e != nil {
		return e.val.read(c)
	}
	return nil
}

// examplePackage is the package the documentation shows: testdata/package.
func examplePackage(t *testing.T) (CustomModel, *tree) {
	t.Helper()
	m, warnings, err := ParseCustomPackage(packageFiles(t, filepath.Join("testdata", "package")))
	if err != nil {
		t.Fatal(err)
	}
	if want := []PackageWarning{{Key: "walkAgentOwned", Detail: "6"}}; !slices.Equal(warnings, want) {
		t.Errorf("warnings %+v, want %+v", warnings, want)
	}
	tr, err := newTree(m.m.build(Identity{Name: "gw-01", Seed: 3}))
	if err != nil {
		t.Fatal(err)
	}
	return m, tr
}

// The example package's files make one device: the identity its walk
// recorded, with the location model.json gives in its place and the name the
// device has; the tunnels its oids file writes; the notification traps.json
// declares; and what the two walks recorded, each read in its own format.
func TestAPackagePutsItsFilesTogether(t *testing.T) {
	m, tr := examplePackage(t)
	if m.ID() != "custom:acme-gateway" || m.Icon() != "acme-gateway.png" || m.m.enterprise != 32473 {
		t.Errorf("%s, icon %q, enterprise %d", m.ID(), m.Icon(), m.m.enterprise)
	}
	if !slices.ContainsFunc(m.m.notifications, func(n Notification) bool { return n.Name == "acmeTunnelDown" }) {
		t.Errorf("notifications: %+v", m.m.notifications)
	}
	at := clock{started: time.Now(), now: time.Now()}
	for oid, want := range map[string]any{
		"1.3.6.1.2.1.1.1.0":                 "Acme G-200 gateway, firmware 4.1.7",
		"1.3.6.1.2.1.1.2.0":                 ".1.3.6.1.4.1.32473.1.200",
		"1.3.6.1.2.1.1.4.0":                 "noc@example.net",
		"1.3.6.1.2.1.1.5.0":                 "gw-01",
		"1.3.6.1.2.1.1.6.0":                 "Lab rack 2",
		"1.3.6.1.2.1.1.7.0":                 6,
		"1.3.6.1.2.1.1.8.0":                 uint32(12),
		"1.3.6.1.2.1.2.2.1.2.2":             "lan0",
		"1.3.6.1.2.1.4.20.1.1.192.168.10.1": "192.168.10.1",
		"1.3.6.1.4.1.32473.5.1.1.2.2":       "tunnel-2",
		"1.3.6.1.4.1.32473.3.2.0":           "4.1.7",
		"1.0.8802.1.1.2.1.3.1.0":            4,
		"1.0.8802.1.1.2.1.3.4.0":            "Acme G-200 gateway,\nfirmware 4.1.7",
		"1.3.6.1.2.1.25.2.3.1.4.1":          4096,
		"1.3.6.1.2.1.6.4.0":                 -1,
		"1.3.6.1.2.1.1.9.1.4.1":             uint32(12),
		"1.3.6.1.2.1.4.21.1.13.0.0.0.0":     ".0.0",
	} {
		if got := readAt(tr, oid, at); !reflect.DeepEqual(got, want) {
			t.Errorf("%s answers %#v, want %#v", oid, got, want)
		}
	}
	for oid, want := range map[string]string{
		"1.3.6.1.2.1.2.2.1.6.2":           "\x02\x00\x00\x5e\x00\x02",
		"1.0.8802.1.1.2.1.3.2.0":          "\x02\x00\x00\x5e\x00\x01",
		"1.0.8802.1.1.2.1.4.1.1.10.0.2.1": "Access switch, room 2",
	} {
		if got, _ := readAt(tr, oid, at).([]byte); string(got) != want {
			t.Errorf("%s answers %q, want %q", oid, got, want)
		}
	}
	// The recorded device's own agent stays behind: its counters, its engine,
	// its users and its communities.
	for _, oid := range []string{"1.3.6.1.2.1.11.1.0", "1.3.6.1.6.3.10.2.1.1.0",
		"1.3.6.1.6.3.18.1.1.1.2.112.117.98.108.105.99"} {
		if got := readAt(tr, oid, at); got != nil {
			t.Errorf("%s is served: %#v", oid, got)
		}
	}
	if got := readAt(tr, "1.3.6.1.2.1.25.1.7.0", at); got != nil {
		t.Errorf("what snmpwalk printed as No Such Object answers %#v", got)
	}
}

// What was recorded moves where a real device's would: a counter from the
// value recorded, at the rate it had averaged since the device booted, and
// the host's uptime and date, which are the device's own. What an oids file
// writes at a recorded OID is what is answered there.
func TestARecordedDeviceMoves(t *testing.T) {
	_, tr := examplePackage(t)
	start := time.Now()
	after := func(seconds float64) clock {
		return clock{started: start, now: start.Add(time.Duration(seconds * float64(time.Second)))}
	}
	// ifInOctets.1: 3 456 000 000 octets over a day's uptime is 40 000 a second.
	if n := readAt(tr, "1.3.6.1.2.1.2.2.1.10.1", after(0)); n != uint32(3456000000) {
		t.Errorf("ifInOctets.1 starts at %v", n)
	}
	if grew := readAt(tr, "1.3.6.1.2.1.2.2.1.10.1", after(1000)).(uint32) - 3456000000; grew < 36e6 || grew > 44e6 {
		t.Errorf("ifInOctets.1 grew by %d in 1000 s, where about 40 000 000 was due", grew)
	}
	if n := readAt(tr, "1.3.6.1.2.1.2.2.1.16.2", after(1000)); n != uint32(0) {
		t.Errorf("a counter recorded at 0 answers %v", n)
	}
	if n := readAt(tr, "1.3.6.1.2.1.31.1.1.1.6.1", after(1000)).(uint64); n <= 98765432100 {
		t.Errorf("ifHCInOctets.1 answers %d", n)
	}
	if n := readAt(tr, "1.3.6.1.2.1.25.1.1.0", after(1000)); n != uint32(100000) {
		t.Errorf("hrSystemUptime answers %v, and the device has been up 1000 s", n)
	}
	if d, _ := readAt(tr, "1.3.6.1.2.1.25.1.2.0", after(0)).([]byte); !slices.Equal(d, dateAndTimeOf(start)) {
		t.Errorf("hrSystemDate answers %v", d)
	}
	// The load the walk recorded as 12 swings between 5 and 60, as
	// oids/tunnels.json writes it.
	seen := map[uint32]bool{}
	for s := 0.0; s < 600; s += 20 {
		v := readAt(tr, "1.3.6.1.4.1.32473.3.1.0", after(s)).(uint32)
		if v < 5 || v > 60 {
			t.Fatalf("the load answers %d", v)
		}
		seen[v] = true
	}
	if len(seen) < 3 {
		t.Errorf("the load takes %d values over ten minutes", len(seen))
	}
}

// A package goes out over the wire whole, as a built-in model does: every
// recorded value encodes, and a walk visits every object.
func TestAPackageServesAWalk(t *testing.T) {
	m, _ := examplePackage(t)
	a := startAgent(t, Config{Versions: []string{"v2c"}, Community: "public",
		Objects: m.m.build(Identity{Name: "gw-01", Seed: 3})})
	g := connect(t, manager(a, gosnmp.Version2c, "public"))
	n := 0
	for _, root := range []string{".1.0.8802", ".1.3.6.1"} {
		if err := g.BulkWalk(root, func(gosnmp.SnmpPDU) error {
			n++
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if all := len(a.tree.Load().entries); n != all {
		t.Errorf("walked %d objects of %d", n, all)
	}
	if s := a.Stats(); s.Faults != 0 {
		t.Errorf("%d answers failed to encode", s.Faults)
	}
}

type typed struct {
	typ gosnmp.Asn1BER
	val any
}

// Both formats a walk comes in are read, with every type they write.
func TestAWalkIsReadInEitherFormat(t *testing.T) {
	for _, tc := range []struct {
		name string
		walk []string
		want map[string]typed
	}{
		{"snmprec", []string{
			"1.3.6.1.2.1.1.1.0|4|a|b",
			"1.3.6.1.2.1.1.2.0|6|1.3.6.1.4.1.8072.3.2.10",
			"1.3.6.1.2.1.2.2.1.6.1|4x|001a2b3c4d5e",
			"1.3.6.1.2.1.2.2.1.8.1|2|-2",
			"1.3.6.1.2.1.2.2.1.10.1|65|4294967295",
			"1.3.6.1.2.1.2.2.1.5.1|66|10000000",
			"1.3.6.1.2.1.1.3.0|67|100",
			"1.3.6.1.2.1.31.1.1.1.6.1|70|18446744073709551615",
			"1.3.6.1.2.1.4.20.1.1.10.0.0.1|64|10.0.0.1",
			"1.3.6.1.2.1.4.20.1.1.10.0.0.2|64x|0a000002",
			"1.3.6.1.2.1.25.1.5.0|71|3",
			"1.3.6.1.4.1.2021.100.1.0|68x|9f780441",
			"1.3.6.1.2.1.1.8.0|128|",
		}, map[string]typed{
			".1.3.6.1.2.1.1.1.0":             {gosnmp.OctetString, "a|b"},
			".1.3.6.1.2.1.1.2.0":             {gosnmp.ObjectIdentifier, ".1.3.6.1.4.1.8072.3.2.10"},
			".1.3.6.1.2.1.2.2.1.6.1":         {gosnmp.OctetString, []byte{0x00, 0x1a, 0x2b, 0x3c, 0x4d, 0x5e}},
			".1.3.6.1.2.1.2.2.1.8.1":         {gosnmp.Integer, -2},
			".1.3.6.1.2.1.2.2.1.10.1":        {gosnmp.Counter32, uint32(4294967295)},
			".1.3.6.1.2.1.2.2.1.5.1":         {gosnmp.Gauge32, uint32(10000000)},
			".1.3.6.1.2.1.1.3.0":             {gosnmp.TimeTicks, uint32(100)},
			".1.3.6.1.2.1.31.1.1.1.6.1":      {gosnmp.Counter64, uint64(18446744073709551615)},
			".1.3.6.1.2.1.4.20.1.1.10.0.0.1": {gosnmp.IPAddress, "10.0.0.1"},
			".1.3.6.1.2.1.4.20.1.1.10.0.0.2": {gosnmp.IPAddress, "10.0.0.2"},
			".1.3.6.1.2.1.25.1.5.0":          {gosnmp.Gauge32, uint32(3)},
			".1.3.6.1.4.1.2021.100.1.0":      {gosnmp.Opaque, []byte{0x9f, 0x78, 0x04, 0x41}},
		}},
		{"snmpwalk -On", []string{
			`.1.3.6.1.2.1.1.1.0 = STRING: "two`,
			`lines"`,
			`.1.3.6.1.2.1.1.2.0 = OID: iso.3.6.1.4.1.8072.3.2.10`,
			`.1.3.6.1.2.1.1.3.0 = Timeticks: 4242`,
			`.1.3.6.1.2.1.1.4.0 = ""`,
			`iso.3.6.1.2.1.1.5.0 = STRING: "iso-prefixed"`,
			`.1.3.6.1.2.1.2.2.1.6.1 = Hex-STRING: 00 1A 2B 3C 4D 5E `,
			`.1.3.6.1.2.1.2.2.1.7.1 = INTEGER: up(1)`,
			`.1.3.6.1.2.1.2.2.1.9.1 = Timeticks: (4200) 0:00:42.00`,
			`.1.3.6.1.2.1.2.2.1.10.1 = Counter32: 918273`,
			`.1.3.6.1.2.1.2.2.1.5.1 = Gauge32: 1000000000`,
			`.1.3.6.1.2.1.31.1.1.1.6.1 = Counter64: 123456789012`,
			`.1.3.6.1.2.1.25.2.3.1.4.1 = INTEGER: 4096 Bytes`,
			`.1.3.6.1.4.1.9.9.13.1.3.1.3.1 = INTEGER: 23.45`,
			`.1.3.6.1.2.1.4.22.1.3.1.10.0.0.9 = IpAddress: 10.0.0.9`,
			`.1.3.6.1.2.1.3.1.1.3.1.1.10.0.0.8 = Network Address: 0A:00:00:08`,
			`.1.3.6.1.2.1.25.3.2.1.4.1 = OID: .0.0`,
			`.1.3.6.1.2.1.17.7.1.4.3.1.2.1 = BITS: 80 00 `,
			`.1.3.6.1.2.1.25.1.6.0 = Wrong Type (should be Gauge32 or Unsigned32): INTEGER: 7`,
			`.1.3.6.1.2.1.25.1.7.0 = No Such Object available on this agent at this OID`,
			`.1.3.6.1.2.1.25.1.8.0 = No Such Instance currently exists at this OID`,
		}, map[string]typed{
			".1.3.6.1.2.1.1.1.0":                {gosnmp.OctetString, "two\nlines"},
			".1.3.6.1.2.1.1.2.0":                {gosnmp.ObjectIdentifier, ".1.3.6.1.4.1.8072.3.2.10"},
			".1.3.6.1.2.1.1.3.0":                {gosnmp.TimeTicks, uint32(4242)},
			".1.3.6.1.2.1.1.4.0":                {gosnmp.OctetString, ""},
			".1.3.6.1.2.1.1.5.0":                {gosnmp.OctetString, "iso-prefixed"},
			".1.3.6.1.2.1.2.2.1.6.1":            {gosnmp.OctetString, []byte{0x00, 0x1a, 0x2b, 0x3c, 0x4d, 0x5e}},
			".1.3.6.1.2.1.2.2.1.7.1":            {gosnmp.Integer, 1},
			".1.3.6.1.2.1.2.2.1.9.1":            {gosnmp.TimeTicks, uint32(4200)},
			".1.3.6.1.2.1.2.2.1.10.1":           {gosnmp.Counter32, uint32(918273)},
			".1.3.6.1.2.1.2.2.1.5.1":            {gosnmp.Gauge32, uint32(1000000000)},
			".1.3.6.1.2.1.31.1.1.1.6.1":         {gosnmp.Counter64, uint64(123456789012)},
			".1.3.6.1.2.1.25.2.3.1.4.1":         {gosnmp.Integer, 4096},
			".1.3.6.1.4.1.9.9.13.1.3.1.3.1":     {gosnmp.Integer, 2345},
			".1.3.6.1.2.1.4.22.1.3.1.10.0.0.9":  {gosnmp.IPAddress, "10.0.0.9"},
			".1.3.6.1.2.1.3.1.1.3.1.1.10.0.0.8": {gosnmp.IPAddress, "10.0.0.8"},
			".1.3.6.1.2.1.25.3.2.1.4.1":         {gosnmp.ObjectIdentifier, ".0.0"},
			".1.3.6.1.2.1.17.7.1.4.3.1.2.1":     {gosnmp.OctetString, []byte{0x80, 0x00}},
			".1.3.6.1.2.1.25.1.6.0":             {gosnmp.Integer, 7},
		}},
	} {
		w, err := readWalk([]byte(strings.Join(tc.walk, "\r\n")))
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if w.skipped != 0 {
			t.Errorf("%s: %d value(s) skipped, the first at %s", tc.name, w.skipped, w.first)
		}
		got := map[string]typed{}
		for _, e := range w.entries {
			got[e.name] = typed{e.typ, e.val}
		}
		for name, want := range tc.want {
			if g, ok := got[name]; !ok || !reflect.DeepEqual(g, want) {
				t.Errorf("%s: %s is %#v, want %#v", tc.name, name, g, want)
			}
		}
		if len(got) != len(tc.want) {
			t.Errorf("%s: %d entries read, want %d", tc.name, len(got), len(tc.want))
		}
	}
}

// A line whose value cannot be read is left out, counted, and the first one
// located; a file that is not a walk at all is refused, and so is one longer
// than a package keeps.
func TestAWalkIsReadLeniently(t *testing.T) {
	w, err := readWalk([]byte(strings.Join([]string{
		"# recorded by hand",
		`.1.3.6.1.2.1.1.1.0 = STRING: "kept"`,
		`.1.3.6.1.2.1.1.2.0 = Float: 1.5`,
		`SNMPv2-MIB::sysName.0 = STRING: "named"`,
		`.1.3.6.1.2.1.1.3.0 = Timeticks: (42) 0:00:00.42`,
	}, "\n")))
	if err != nil {
		t.Fatal(err)
	}
	if len(w.entries) != 2 || w.skipped != 2 || w.first != `line 3: "Float" is not a type snmpwalk prints` {
		t.Errorf("%d read, %d skipped, the first at %q", len(w.entries), w.skipped, w.first)
	}
	w, err = readWalk([]byte("1.3.6.1.2.1.1.1.0|4|kept\n1.3.6.1.2.1.1.3.0|67:numeric|rate=100\n1.3.6.1.2.1.1.4.0|4x|zz\n"))
	if err != nil || len(w.entries) != 1 || w.skipped != 2 || !strings.HasPrefix(w.first, "line 2: snmpsim's variation modules") {
		t.Errorf("%+v, %v", w, err)
	}
	for _, notAWalk := range []string{`{"objects": []}`, "", "hello\n"} {
		if _, err := readWalk([]byte(notAWalk)); err == nil || !strings.Contains(err.Error(), "not a walk") {
			t.Errorf("%q: %v", notAWalk, err)
		}
	}
	var long strings.Builder
	for i := range MaxRecordedObjects + 1 {
		fmt.Fprintf(&long, "1.3.6.1.4.1.32473.9.%d|2|%d\n", i, i)
	}
	if _, err := readWalk([]byte(long.String())); err == nil || !strings.Contains(err.Error(), "more than") {
		t.Errorf("a walk too long: %v", err)
	}
}

const lonePackageModel = `{"kind": "snmplens-simulator-model", "formatVersion": 1, "id": "p", "name": "P",
  "system": {"descr": "P", "objectId": "1.3.6.1.4.1.32473.1"}}`

// A package is refused naming the file, and in it what is wrong.
func TestAPackageIsRefusedNamingItsFile(t *testing.T) {
	model := PackageFile{Name: "model.json", Data: []byte(lonePackageModel)}
	oids := func(name, objects string) PackageFile {
		return PackageFile{Name: name, Data: []byte(`{"objects": [` + objects + `]}`)}
	}
	for _, tc := range []struct {
		files []PackageFile
		want  string
	}{
		{nil, "has no model.json"},
		{[]PackageFile{model, oids("oids/bad.json", `{"oid": "1.3.6.1.4.1.32473.9.0", "type": "Float", "value": 1}`)},
			`oids/bad.json: objects[0] (1.3.6.1.4.1.32473.9.0): "Float" is not a type`},
		{[]PackageFile{model, {Name: "oids.json", Data: []byte(`{"object": []}`)}}, `oids.json: unknown field "object"`},
		{[]PackageFile{model,
			oids("oids/a.json", `{"oid": "1.3.6.1.4.1.32473.9.0", "type": "Integer32", "value": 1}`),
			oids("oids/b.json", `{"oid": "1.3.6.1.4.1.32473.9.0", "type": "Integer32", "value": 2}`)},
			"declared twice"},
		{[]PackageFile{model, {Name: "traps.json", Data: []byte(`{"notifications": [{"name": "coldStart", "oid": "1.3.6.1.4.1.32473.0.1"}]}`)}},
			"traps.json: notifications[0]: coldStart is already one of the model's"},
		{[]PackageFile{model, {Name: "walks/notes.txt", Data: []byte("to do: record the switch")}},
			"walks/notes.txt: this is not a walk"},
		{[]PackageFile{model, {Name: "walks/named.walk", Data: []byte("SNMPv2-MIB::sysDescr.0 = STRING: \"x\"\n")}},
			"walks/named.walk: no value in it could be read — line 1: the OID is named from a MIB"},
		{[]PackageFile{{Name: "model.json", Data: []byte(`{"kind": "snmplens-simulator-model", "formatVersion": 1, "id": "p", "name": "P"}`)},
			{Name: "walks/device.snmprec", Data: []byte("1.3.6.1.2.1.1.1.0|4|Recorded\n")}},
			"no sysObjectID"},
		{[]PackageFile{model, {Name: "notes.json", Data: []byte(`{}`)}}, "notes.json is not a file a package reads"},
		{[]PackageFile{model, {Name: "Model.json", Data: []byte(lonePackageModel)}}, "one model file"},
	} {
		if _, _, err := ParseCustomPackage(tc.files); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v, want %q", err, tc.want)
		}
	}
}

// When model.json says "interfaces", IF-MIB is made from them and the walks'
// own is left out, rather than mixed in with the rows it makes; the rest of
// the walks is served, and what model.json's "system" gives is the device's.
func TestWrittenInterfacesReplaceTheRecordedOnes(t *testing.T) {
	m, _, err := ParseCustomPackage([]PackageFile{
		{Name: "model.json", Data: []byte(`{"kind": "snmplens-simulator-model", "formatVersion": 1, "id": "p", "name": "P",
			"system": {"descr": "Written"}, "interfaces": [{"descr": "written0", "up": true}]}`)},
		{Name: "walks/device.walk", Data: []byte(strings.Join([]string{
			`.1.3.6.1.2.1.1.1.0 = STRING: "Recorded"`,
			`.1.3.6.1.2.1.1.2.0 = OID: .1.3.6.1.4.1.32473.1`,
			`.1.3.6.1.2.1.2.1.0 = INTEGER: 1`,
			`.1.3.6.1.2.1.2.2.1.2.7 = STRING: "recorded7"`,
			`.1.3.6.1.2.1.31.1.1.1.1.7 = STRING: "rec7"`,
			`.1.3.6.1.2.1.4.20.1.2.10.0.0.1 = INTEGER: 1`,
		}, "\n"))},
	})
	if err != nil {
		t.Fatal(err)
	}
	tr, err := newTree(m.m.build(Identity{Name: "p-01", Seed: 1}))
	if err != nil {
		t.Fatal(err)
	}
	at := clock{}
	for oid, want := range map[string]any{
		"1.3.6.1.2.1.1.1.0":             "Written",
		"1.3.6.1.2.1.1.2.0":             ".1.3.6.1.4.1.32473.1",
		"1.3.6.1.2.1.2.2.1.2.1":         "written0",
		"1.3.6.1.2.1.2.2.1.2.7":         nil,
		"1.3.6.1.2.1.31.1.1.1.1.7":      nil,
		"1.3.6.1.2.1.4.20.1.2.10.0.0.1": 1,
	} {
		if got := readAt(tr, oid, at); !reflect.DeepEqual(got, want) {
			t.Errorf("%s answers %#v, want %#v", oid, got, want)
		}
	}
}

// Whatever a walk holds, what is read from it is a value the tree accepts:
// a walk is a stranger's file, and a value gosnmp cannot encode would fail
// every response that carried it.
func FuzzReadWalk(f *testing.F) {
	for _, name := range []string{"gateway.snmprec", "netsnmp.walk"} {
		data, err := os.ReadFile(filepath.Join("testdata", "package", "walks", name))
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
	}
	f.Add([]byte(".1.3.6.1.2.1.1.1.0 = STRING: \"open\n"))
	f.Add([]byte("1.3.6.1.2.1.1.1.0|4x|zz\n1.3.6.1.2.1.4.20.1.1.1.2.3.4|64x|0102\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		w, err := readWalk(data)
		if err != nil {
			return
		}
		for _, e := range w.entries {
			if err := checkValue(e.typ, e.val); err != nil {
				t.Errorf("%s: %v", e.name, err)
			}
		}
	})
}
