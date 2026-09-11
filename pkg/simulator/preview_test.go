package simulator

import (
	"regexp"
	"testing"
	"time"
)

// A preview is every object the device answers, its model built for it with its
// own values, each saying how it behaves; what moves has moved once the device
// has run a while, and an OCTET STRING that is not text is written as octets.
func TestAPreviewShowsWhatTheDeviceAnswers(t *testing.T) {
	d := Device{ID: "0123456789abcdef", Name: "srv-07", Model: "linux-server", Overrides: []Override{
		{OID: sysContactOID, Type: "OctetString", Value: "ops@example.net"}}}
	rows, err := PreviewRows(d, 0)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]PreviewRow{}
	for _, r := range rows {
		by[r.OID] = r
	}
	for oid, want := range map[string]string{
		".1.3.6.1.2.1.1.5.0": "OctetString srv-07 static",
		sysContactOID:        "OctetString ops@example.net static",
		".1.3.6.1.2.1.1.3.0": "TimeTicks 0 uptime",
	} {
		if r := by[oid]; r.Type+" "+r.Value+" "+r.Behaviour != want {
			t.Errorf("%s: %q, want %q", oid, r.Type+" "+r.Value+" "+r.Behaviour, want)
		}
	}
	for oid, want := range map[string]string{
		".1.3.6.1.2.1.25.3.3.1.2.196608": BehaviourGauge, // the first processor's load
		".1.3.6.1.2.1.25.1.2.0":          BehaviourClock, // hrSystemDate
	} {
		if got := by[oid].Behaviour; got != want {
			t.Errorf("%s behaves as %q, want %q", oid, got, want)
		}
	}
	const inOctets = ".1.3.6.1.2.1.31.1.1.1.6.2" // eth0's ifHCInOctets
	if by[inOctets].Behaviour == BehaviourStatic {
		t.Errorf("%s is static", inOctets)
	}

	later, err := PreviewRows(d, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range later {
		switch r.OID {
		case inOctets:
			if r.Value == by[inOctets].Value {
				t.Errorf("a minute on, %s still reads %s", inOctets, r.Value)
			}
		case ".1.3.6.1.2.1.1.5.0":
			if r.Value != "srv-07" {
				t.Errorf("sysName moved to %q", r.Value)
			}
		}
	}
	if mac := by[".1.3.6.1.2.1.2.2.1.6.2"].Value; !regexp.MustCompile(`^([0-9a-f]{2}:){5}[0-9a-f]{2}$`).MatchString(mac) {
		t.Errorf("eth0's MAC address is written %q", mac)
	}
	d.Overrides = []Override{{OID: "1.3.6.1.2.1.11.1.0", Type: "Counter32", Value: "1"}}
	if _, err := PreviewRows(d, 0); err == nil {
		t.Error("a device with a value the agent owns was previewed")
	}
}
