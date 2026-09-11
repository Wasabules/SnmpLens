package simulator

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/gosnmp/gosnmp"
)

// A device's own values take the place of its model's, or join them, and the
// agent answers them as it answers the model's.
func TestADevicesOwnValuesAreAnswered(t *testing.T) {
	d := Device{Name: "srv", Model: "linux-server", Overrides: []Override{
		{OID: sysContactOID, Type: "OctetString", Value: "ops@example.net"},
		{OID: "1.3.6.1.4.1.32473.1.1.0", Type: "Gauge32", Value: "42"},
		{OID: "1.3.6.1.4.1.32473.1.2.0", Type: "OctetString", Value: "00:1a:2b", Hex: true},
	}}
	objs, err := d.objects()
	if err != nil {
		t.Fatal(err)
	}
	a := startAgent(t, Config{Versions: []string{"v2c"}, Community: "public", Objects: objs})
	g := connect(t, manager(a, gosnmp.Version2c, "public"))
	if v := getOne(t, g, sysContactOID); text(v) != "ops@example.net" {
		t.Errorf("sysContact is %q", text(v))
	}
	if v := getOne(t, g, ".1.3.6.1.4.1.32473.1.1.0"); v.Type != gosnmp.Gauge32 || gosnmp.ToBigInt(v.Value).Int64() != 42 {
		t.Errorf("the new gauge came back as %v %v", v.Type, v.Value)
	}
	if v := getOne(t, g, ".1.3.6.1.4.1.32473.1.2.0"); !bytes.Equal(v.Value.([]byte), []byte{0, 0x1a, 0x2b}) {
		t.Errorf("the octets came back as %v", v.Value)
	}
}

// What a device could not answer, or no model may set, is refused when it is
// saved rather than when it starts.
func TestADevicesOwnValuesAreChecked(t *testing.T) {
	base := Device{Name: "srv", Model: "linux-server", Address: "127.0.0.2", Port: 1161, Versions: []string{"v2c"},
		Community: "public", EngineID: "8000000903aabbccddeeff"}
	for name, ovs := range map[string][]Override{
		"the agent's own":   {{OID: "1.3.6.1.2.1.11.1.0", Type: "Counter32", Value: "5"}},
		"not a type":        {{OID: "1.3.6.1.4.1.32473.1.0", Type: "Integer64", Value: "5"}},
		"not a number":      {{OID: "1.3.6.1.4.1.32473.1.0", Type: "Integer", Value: "five"}},
		"not an address":    {{OID: "1.3.6.1.4.1.32473.1.0", Type: "IpAddress", Value: "::1"}},
		"not hex":           {{OID: "1.3.6.1.4.1.32473.1.0", Type: "OctetString", Value: "zz", Hex: true}},
		"not an OID":        {{OID: "sysContact.0", Type: "OctetString", Value: "x"}},
		"twice":             {{OID: "1.3.6.1.4.1.32473.1.0", Type: "Integer", Value: "1"}, {OID: ".1.3.6.1.4.1.32473.1.0", Type: "Integer", Value: "2"}},
		"under an instance": {{OID: sysContactOID + ".1", Type: "Integer", Value: "1"}},
		"over an instance":  {{OID: "1.3.6.1.2.1.1.4", Type: "Integer", Value: "1"}},
	} {
		d := base
		d.Overrides = ovs
		if err := d.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	d := base
	d.Overrides = make([]Override, MaxOverrides+1)
	for i := range d.Overrides {
		d.Overrides[i] = Override{OID: fmt.Sprintf("1.3.6.1.4.1.32473.2.%d.0", i), Type: "Integer", Value: "1"}
	}
	if d.Validate() == nil {
		t.Errorf("%d values of its own were accepted", len(d.Overrides))
	}
	d.Overrides = d.Overrides[:MaxOverrides]
	if err := d.Validate(); err != nil {
		t.Errorf("%d values of its own: %v", MaxOverrides, err)
	}
}
