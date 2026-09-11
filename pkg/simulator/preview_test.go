package simulator

import (
	"regexp"
	"testing"
)

// A preview is what the device would answer: its model built for it, with its
// own values, under the subtree asked for, in at most as many rows as asked,
// and an OCTET STRING that is not text written as its octets.
func TestAPreviewShowsWhatTheDeviceAnswers(t *testing.T) {
	d := Device{ID: "0123456789abcdef", Name: "srv-07", Model: "linux-server", Overrides: []Override{
		{OID: sysContactOID, Type: "OctetString", Value: "ops@example.net"}}}
	p, err := PreviewDevice(d, "1.3.6.1.2.1.1", 0)
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	for _, r := range p.Rows {
		values[r.OID] = r.Type + " " + r.Value
	}
	for oid, want := range map[string]string{
		".1.3.6.1.2.1.1.5.0": "OctetString srv-07",
		sysContactOID:        "OctetString ops@example.net",
		".1.3.6.1.2.1.1.3.0": "TimeTicks 0",
	} {
		if values[oid] != want {
			t.Errorf("%s: %q, want %q", oid, values[oid], want)
		}
	}
	if p.Total != len(p.Rows) || p.Total == 0 {
		t.Errorf("%d rows of %d", len(p.Rows), p.Total)
	}

	all, err := PreviewDevice(d, "", 5)
	if err != nil || len(all.Rows) != 5 || all.Total <= 5 {
		t.Errorf("five rows of everything: %d of %d, %v", len(all.Rows), all.Total, err)
	}
	mac, err := PreviewDevice(d, "1.3.6.1.2.1.2.2.1.6.2", 0)
	if err != nil || len(mac.Rows) != 1 || !regexp.MustCompile(`^([0-9a-f]{2}:){5}[0-9a-f]{2}$`).MatchString(mac.Rows[0].Value) {
		t.Errorf("eth0's MAC address: %+v, %v", mac.Rows, err)
	}
	if _, err := PreviewDevice(d, "not an OID", 0); err == nil {
		t.Error("a subtree that is not an OID was previewed")
	}
	d.Overrides = []Override{{OID: "1.3.6.1.2.1.11.1.0", Type: "Counter32", Value: "1"}}
	if _, err := PreviewDevice(d, "", 0); err == nil {
		t.Error("a device with a value the agent owns was previewed")
	}
}
