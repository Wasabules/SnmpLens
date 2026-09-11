package simulator

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"
)

func readExampleModel(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/custom-model.json")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func useCustomModels(t *testing.T, ms ...CustomModel) {
	t.Helper()
	if err := SetCustomModels(ms); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = SetCustomModels(nil) })
}

// asText is a value as text, whether it is still the Go value a reading gave or
// what came back over the wire.
func asText(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case []byte:
		return string(s)
	}
	return ""
}

// The example model — the one the documentation shows — is read, joins the
// catalogue under its own prefix, and answers over the wire what it says: an
// index column holding its own instance, a name per row, a gauge in its range,
// a counter from where it starts, octets given in hex. The object it marks as
// accessible-for-notify is not read by a GET, and is carried by its
// notification.
func TestTheExampleModelAnswersWhatItSays(t *testing.T) {
	m, err := ParseCustomModel(readExampleModel(t))
	if err != nil {
		t.Fatal(err)
	}
	if m.ID() != "custom:acme-crac" || m.Slug() != "acme-crac" || m.Icon() != "acme-crac.png" ||
		m.Name() != "Acme CRAC-40 cooling unit" {
		t.Fatalf("read as %q, %q, %q, %q", m.ID(), m.Slug(), m.Icon(), m.Name())
	}
	useCustomModels(t, m)
	var info *ModelInfo
	for _, mi := range Models() {
		if mi.ID == m.ID() {
			info = &mi
		}
	}
	if info == nil || !info.Custom || info.Category != "environment" || info.Vendor != "Acme" || info.Description == "" {
		t.Fatalf("listed as %+v", info)
	}
	var names []string
	for _, n := range info.Notifications {
		names = append(names, n.Name)
	}
	if got := strings.Join(names, ","); got != "coldStart,warmStart,acmeHighTemperature,authenticationFailure" {
		t.Errorf("sends %s", got)
	}

	objects := m.m.build(Identity{Name: "crac-01", Seed: 7})
	a := startAgent(t, Config{Versions: []string{"v2c"}, Community: "public", Objects: objects, Notifications: m.m.catalogue()})
	g := connect(t, manager(a, gosnmp.Version2c, "public"))
	res, err := g.Get([]string{
		".1.3.6.1.2.1.1.5.0", ".1.3.6.1.2.1.1.2.0",
		".1.3.6.1.4.1.32473.2.2.1.1.3", ".1.3.6.1.4.1.32473.2.2.1.2.2", ".1.3.6.1.4.1.32473.2.2.1.3.1",
		".1.3.6.1.4.1.32473.2.4.0", ".1.3.6.1.4.1.32473.2.6.0", ".1.3.6.1.4.1.32473.3.1.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	v := res.Variables
	if asText(v[0].Value) != "crac-01" || asText(v[1].Value) != ".1.3.6.1.4.1.32473.1.40" {
		t.Errorf("system group: %v, %v", v[0].Value, v[1].Value)
	}
	if n := gosnmp.ToBigInt(v[2].Value).Int64(); n != 3 {
		t.Errorf("the index column of row 3 holds %d", n)
	}
	if asText(v[3].Value) != "Sensor 2" {
		t.Errorf("row 2 is named %q", asText(v[3].Value))
	}
	if n := gosnmp.ToBigInt(v[4].Value).Int64(); v[4].Type != gosnmp.Integer || n < 180 || n > 260 {
		t.Errorf("a gauge from 180 to 260 read %v %d", v[4].Type, n)
	}
	if n := gosnmp.ToBigInt(v[5].Value).Uint64(); v[5].Type != gosnmp.Counter64 || n < 31536000 {
		t.Errorf("a counter from 31536000 read %v %d", v[5].Type, n)
	}
	if b, _ := v[6].Value.([]byte); !bytes.Equal(b, []byte{0x00, 0x1a, 0x2b, 0x3c, 0x4d, 0x5e}) {
		t.Errorf("hex octets read %x", b)
	}
	if v[7].Type != gosnmp.NoSuchObject {
		t.Errorf("an accessible-for-notify object answered a GET: %v %v", v[7].Type, v[7].Value)
	}

	for _, n := range m.m.catalogue() {
		if n.Name != "acmeHighTemperature" {
			continue
		}
		vars := a.varbinds(n, clock{started: time.Now(), now: time.Now()})
		if len(vars) != 4 || vars[1].Value != ".1.3.6.1.4.1.32473.0.1" ||
			vars[2].Name != ".1.3.6.1.4.1.32473.2.2.1.3.2" || asText(vars[3].Value) != "Return air above 27 °C" {
			t.Errorf("the notification carries %+v", vars)
		}
	}
}

// A device is made from a custom model once the model is set, with an engine ID
// carrying the vendor its sysObjectID names — and not once the model is gone.
func TestADeviceIsMadeFromACustomModel(t *testing.T) {
	m, err := ParseCustomModel(readExampleModel(t))
	if err != nil {
		t.Fatal(err)
	}
	d := Device{ID: "d1", Name: "crac-01", Model: m.ID(), Address: "127.0.0.1", Port: 16100,
		Versions: []string{"v2c"}, Community: "public"}
	if _, err := NewEngineID(d.Model, d.ID); err == nil {
		t.Fatal("an engine ID was made for a model nobody set")
	}
	useCustomModels(t, m)
	engine, err := NewEngineID(d.Model, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	// 32473, the enterprise number RFC 5612 keeps for documentation, with the
	// high bit RFC 3411 sets.
	if !strings.HasPrefix(engine, "80007ed9") {
		t.Errorf("engine ID %s does not carry the model's vendor", engine)
	}
	d.EngineID = engine
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := SetCustomModels(nil); err != nil {
		t.Fatal(err)
	}
	if err := d.Validate(); err == nil || !strings.Contains(err.Error(), "not a device model") {
		t.Errorf("a device of a model no longer set: %v", err)
	}
}

func TestTwoCustomModelsCannotShareAnID(t *testing.T) {
	m, err := ParseCustomModel(readExampleModel(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = SetCustomModels(nil) })
	if err := SetCustomModels([]CustomModel{m, m}); err == nil {
		t.Error("two models of one ID were set")
	}
	if err := SetCustomModels([]CustomModel{{}}); err == nil {
		t.Error("a model that was never read was set")
	}
}

// minimalModel is the least a model file can say.
func minimalModel() map[string]any {
	return map[string]any{
		"kind": CustomModelKind, "formatVersion": 1, "id": "probe-x", "name": "Probe X",
		"system":  map[string]any{"descr": "Probe X", "objectId": "1.3.6.1.4.1.32473.1"},
		"objects": []any{map[string]any{"oid": "1.3.6.1.4.1.32473.2.1.0", "type": "Integer32", "value": 7}},
	}
}

func object(fields ...any) map[string]any {
	o := map[string]any{}
	for i := 0; i+1 < len(fields); i += 2 {
		o[fields[i].(string)] = fields[i+1]
	}
	return o
}

// Each thing a model file can get wrong is refused, and the refusal names it —
// the field, the OID or the line — because the person reading it is the one who
// wrote the file.
func TestCustomModelsAreRefusedForWhatIsWrong(t *testing.T) {
	if _, err := ParseCustomModel(mustJSON(t, minimalModel())); err != nil {
		t.Fatalf("the minimal model: %v", err)
	}
	withObject := func(o map[string]any) func(map[string]any) {
		return func(m map[string]any) { m["objects"] = append(m["objects"].([]any), o) }
	}
	many := make([]any, 9)
	instances := make([]any, 1000)
	for i := range instances {
		instances[i] = i + 1
	}
	for i := range many {
		many[i] = object("oid", "1.3.6.1.4.1.32473.3."+string(rune('1'+i))+".{#}", "type", "Integer32", "value", 1, "instances", instances)
	}
	cases := []struct {
		name string
		raw  string
		edit func(m map[string]any)
		want string
	}{
		{name: "a file cut short", raw: `{"kind": `, want: "ends before the model does"},
		{name: "a stray character", raw: "{\n  \"kind\": x\n}", want: "line 2, column"},
		{name: "a preset", raw: `{"formatVersion": 1, "name": "Ports", "widgets": []}`, want: "not a simulator model"},
		{name: "a later format", edit: func(m map[string]any) { m["formatVersion"] = 2 }, want: "format version 2"},
		{name: "a misspelt field", edit: func(m map[string]any) {
			m["objects"] = []any{object("oid", "1.3.6.1.4.1.32473.2.1.0", "type", "Integer32", "vaule", 7)}
		}, want: `unknown field "vaule"`},
		{name: "an ID with capitals", edit: func(m map[string]any) { m["id"] = "Probe_X" }, want: `"id"`},
		{name: "a name that is a number", edit: func(m map[string]any) { m["name"] = 5 }, want: `"name" is a JSON number`},
		{name: "an unknown category", edit: func(m map[string]any) { m["category"] = "toaster" }, want: `"category"`},
		{name: "enterprise 0", edit: func(m map[string]any) { m["enterprise"] = 0 }, want: `"enterprise"`},
		{name: "an icon that is a path", edit: func(m map[string]any) { m["icon"] = "../x.png" }, want: `"icon" names a file`},
		{name: "no system group", edit: func(m map[string]any) { delete(m, "system") }, want: `"system" is missing`},
		{name: "a sysObjectID that is not one", edit: func(m map[string]any) {
			m["system"] = map[string]any{"descr": "x", "objectId": "1.3.6.x"}
		}, want: "system.objectId"},
		{name: "a type that is not one", edit: withObject(object("oid", "1.3.6.1.4.1.32473.2.2.0", "type", "Integr32", "value", 1)),
			want: "is not a type"},
		{name: "two behaviours", edit: withObject(object("oid", "1.3.6.1.4.1.32473.2.2.0", "type", "TimeTicks", "value", 1, "uptime", true)),
			want: "exactly one"},
		{name: "a number written as text", edit: withObject(object("oid", "1.3.6.1.4.1.32473.2.2.0", "type", "Integer32", "value", "7")),
			want: "is a number"},
		{name: "an Integer32 too large", edit: withObject(object("oid", "1.3.6.1.4.1.32473.2.2.0", "type", "Integer32", "value", 3000000000)),
			want: "not an Integer32"},
		{name: "a gauge that is a counter", edit: withObject(object("oid", "1.3.6.1.4.1.32473.2.2.0", "type", "Counter32",
			"gauge", object("min", 1, "max", 2))), want: "a gauge is a Gauge32 or an Integer32"},
		{name: "a gauge upside down", edit: withObject(object("oid", "1.3.6.1.4.1.32473.2.2.0", "type", "Gauge32",
			"gauge", object("min", 5, "max", 1))), want: "from a low to a high"},
		{name: "a counter that would go back", edit: withObject(object("oid", "1.3.6.1.4.1.32473.2.2.0", "type", "Counter32",
			"counter", object("perSecond", 1, "swing", 0.99))), want: `"swing"`},
		{name: "a placeholder with no instances", edit: withObject(object("oid", "1.3.6.1.4.1.32473.2.2.{#}", "type", "Integer32", "value", 1)),
			want: "gives none"},
		{name: "instances with no placeholder", edit: withObject(object("oid", "1.3.6.1.4.1.32473.2.2.0", "type", "Integer32", "value", 1,
			"instances", []any{1})), want: "needs {#}"},
		{name: "an instance that is not one", edit: withObject(object("oid", "1.3.6.1.4.1.32473.2.2.{#}", "type", "Integer32", "value", 1,
			"instances", []any{"1..2"})), want: "is not an instance"},
		{name: "an index column past its type", edit: withObject(object("oid", "1.3.6.1.4.1.32473.2.2.{#}", "type", "Integer32", "value", "{#}",
			"instances", []any{"1.2"})), want: "not an Integer32"},
		{name: "a value too long", edit: withObject(object("oid", "1.3.6.1.4.1.32473.2.2.0", "type", "OctetString",
			"value", strings.Repeat("x", 5000))), want: "at most 4096"},
		{name: "an IPv6 IpAddress", edit: withObject(object("oid", "1.3.6.1.4.1.32473.2.2.0", "type", "IpAddress", "value", "::1")),
			want: "IPv4"},
		{name: "a system object given twice", edit: withObject(object("oid", "1.3.6.1.2.1.1.5.0", "type", "OctetString", "value", "x")),
			want: "declared twice"},
		{name: "an object under another", edit: withObject(object("oid", "1.3.6.1.4.1.32473.2.1.0.1", "type", "Integer32", "value", 1)),
			want: "lies under"},
		{name: "an object of the agent's own", edit: withObject(object("oid", "1.3.6.1.6.3.10.2.1.1.0", "type", "OctetString", "value", "x")),
			want: "are the agent's own"},
		{name: "an object of the snmp group", edit: withObject(object("oid", "1.3.6.1.2.1.11.1.0", "type", "Counter32", "value", 1)),
			want: "declared twice"},
		{name: "more objects than a model makes", edit: func(m map[string]any) { m["objects"] = many }, want: "at most 8192"},
		{name: "a notification carrying what is not answered", edit: func(m map[string]any) {
			m["notifications"] = []any{object("name", "probeAlarm", "oid", "1.3.6.1.4.1.32473.0.1", "objects", []any{"1.3.6.1.4.1.32473.9.9.0"})}
		}, want: "does not answer"},
		{name: "a notification taking a generic name", edit: func(m map[string]any) {
			m["notifications"] = []any{object("name", "coldStart", "oid", "1.3.6.1.4.1.32473.0.1")}
		}, want: "already one of"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			raw := []byte(c.raw)
			if c.edit != nil {
				m := minimalModel()
				c.edit(m)
				raw = mustJSON(t, m)
			}
			_, err := ParseCustomModel(raw)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("want an error saying %q, got %v", c.want, err)
			}
		})
	}

	two := append(mustJSON(t, minimalModel()), mustJSON(t, minimalModel())...)
	if _, err := ParseCustomModel(two); err == nil || !strings.Contains(err.Error(), "more after") {
		t.Errorf("two models in one file: %v", err)
	}
	if _, err := ParseCustomModel(bytes.Repeat([]byte(" "), MaxCustomModelBytes+1)); err == nil {
		t.Error("a file past the bound was read")
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// An object only a notification carries is not something a request reads: a
// GET answers noSuchObject and a walk passes it by, as a real iDRAC's do — and
// the alert that names them carries all eleven, in the MIB's order, with the
// device's own service tag and name.
func TestNotifyOnlyObjectsTravelOnlyInNotifications(t *testing.T) {
	a := startAgent(t, Config{Versions: []string{"v2c"}, Community: "public",
		Objects: dellIDRAC9.build(Identity{Name: "idrac-01", Seed: 3}), Notifications: dellIDRAC9.catalogue()})
	g := connect(t, manager(a, gosnmp.Version2c, "public"))
	res, err := g.Get([]string{".1.3.6.1.4.1.674.10892.5.3.1.2.0"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Variables[0].Type != gosnmp.NoSuchObject {
		t.Errorf("alertMessage answered a GET: %v", res.Variables[0].Value)
	}
	vars := a.varbinds(dellIDRAC9.notifications[0], clock{started: time.Now(), now: time.Now()})
	if len(vars) != 2+11 {
		t.Fatalf("the alert carries %d varbinds", len(vars))
	}
	if asText(vars[2].Value) != "TMP0120" || asText(vars[len(vars)-1].Value) != "idrac-01" ||
		asText(vars[5].Value) != serviceTag(3+5) {
		t.Errorf("the alert carries %v, %v, %v", vars[2].Value, vars[5].Value, vars[len(vars)-1].Value)
	}
}
