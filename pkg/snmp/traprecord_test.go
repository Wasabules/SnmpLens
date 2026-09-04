package snmp

import (
	"testing"

	"github.com/gosnmp/gosnmp"

	"SnmpLens/pkg/events"
)

type capturingRecorder struct {
	events   []events.Event
	payloads []string
}

func (r *capturingRecorder) Record(e events.Event, payload string) error {
	r.events = append(r.events, e)
	r.payloads = append(r.payloads, payload)
	return nil
}

// The trap OID is the identity of a trap: it is what a notification route's OID
// prefix is matched against, what `{{trapOid}}` renders, and half of the dedup
// key. It was found by looking for the TEXT "snmpTrapOID" in each varbind name
// — and gosnmp does no MIB translation, so a varbind name is always numeric.
// Measured: Name=".1.3.6.1.6.3.1.1.4.1.0" contains snmpTrapOID = false.
//
// So every v2c and v3 trap was journalled with an EMPTY OID. An OID-prefix rule
// could not match one, and every trap from a host shared one dedup key
// regardless of what it reported.
func TestTrapOIDIsRecoveredFromTheNumericVarbind(t *testing.T) {
	const linkDown = ".1.3.6.1.6.3.1.1.5.3"

	for _, version := range []gosnmp.SnmpVersion{gosnmp.Version2c, gosnmp.Version3} {
		rec := &capturingRecorder{}
		c := &Client{recorder: rec}

		vars := []Result{
			{Oid: ".1.3.6.1.2.1.1.3.0", Type: "TimeTicks", Value: uint32(12345)},
			{Oid: ".1.3.6.1.6.3.1.1.4.1.0", Type: "ObjectIdentifier", Value: linkDown},
			{Oid: ".1.3.6.1.2.1.2.2.1.1.3", Type: "Integer", Value: 3},
		}
		packet := &gosnmp.SnmpPacket{Version: version, PDUType: gosnmp.SNMPv2Trap}
		c.recordTrap("10.0.0.1", "2026-01-01T00:00:00Z", "Trap", packet, vars)

		if len(rec.events) != 1 {
			t.Fatalf("%s: %d events journalled", version, len(rec.events))
		}
		ev := rec.events[0]
		if ev.OID != linkDown {
			t.Errorf("%s: OID = %q, want %q — an OID-prefix route cannot match this",
				version, ev.OID, linkDown)
		}
		if ev.Params["trapOid"] != linkDown {
			t.Errorf("%s: trapOid param = %q, want %q — {{trapOid}} renders this",
				version, ev.Params["trapOid"], linkDown)
		}
		if ev.DedupKey != "trap|10.0.0.1|"+linkDown {
			t.Errorf("%s: dedup key = %q — every trap from this host would share it",
				version, ev.DedupKey)
		}
	}
}

// v1 has no snmpTrapOID varbind at all: handleTrap synthesises one from the PDU
// header under the NAME "snmpTrapOID.0". That path worked and must keep working.
func TestV1TrapOIDStillComesFromTheSynthesisedVarbind(t *testing.T) {
	rec := &capturingRecorder{}
	c := &Client{recorder: rec}

	vars := []Result{
		{Oid: "snmpTrapOID.0", Type: "SNMPv1 Trap", Value: ".1.3.6.1.4.1.9"},
		{Oid: "genericTrap", Type: "INTEGER", Value: 6},
	}
	packet := &gosnmp.SnmpPacket{Version: gosnmp.Version1, PDUType: gosnmp.Trap}
	c.recordTrap("10.0.0.2", "2026-01-01T00:00:00Z", "Trap", packet, vars)

	if got := rec.events[0].OID; got != ".1.3.6.1.4.1.9" {
		t.Errorf("v1 trap OID = %q, want the enterprise OID", got)
	}
}

// A trap carrying no snmpTrapOID varbind is malformed, but it arrives from the
// network unauthenticated: it must journal, not panic or invent an OID.
func TestTrapWithNoTrapOIDVarbindStillJournals(t *testing.T) {
	rec := &capturingRecorder{}
	c := &Client{recorder: rec}

	vars := []Result{{Oid: ".1.3.6.1.2.1.1.3.0", Type: "TimeTicks", Value: uint32(1)}}
	packet := &gosnmp.SnmpPacket{Version: gosnmp.Version2c, PDUType: gosnmp.SNMPv2Trap}
	c.recordTrap("10.0.0.3", "2026-01-01T00:00:00Z", "Trap", packet, vars)

	if len(rec.events) != 1 {
		t.Fatalf("%d events journalled", len(rec.events))
	}
	if got := rec.events[0].OID; got != "" {
		t.Errorf("OID = %q, want empty — nothing named one", got)
	}
}
