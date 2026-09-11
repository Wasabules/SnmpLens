package simulator

import (
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/gosnmp/gosnmp"
)

// A device being edited can be looked at before it is saved or started: what
// its model, built for it with its parameters and its own values, would
// answer. The preview reads the very objects the agent would be given, so what
// it shows is what a walk would find — less the agent's own counters, which
// count requests and have seen none, and what only a notification carries.
// Each value says how it behaves, since which of them move is half of what a
// person looks at a simulated device for.

// MaxPreviewRows bounds a preview page: a table on a screen, not a walk.
const MaxPreviewRows = 500

// How a value behaves, as the preview says it.
const (
	BehaviourStatic  = "static"  // the same whenever it is read
	BehaviourCounter = "counter" // only goes up, and wraps
	BehaviourGauge   = "gauge"   // swings between two bounds
	BehaviourUptime  = "uptime"  // the time since the device started
	BehaviourClock   = "clock"   // the time of day
	BehaviourDerived = "derived" // computed from other readings at the same instant
)

// PreviewRow is one object as a device answers it: its value written out, and
// how that value behaves.
type PreviewRow struct {
	OID       string `json:"oid"`
	Type      string `json:"type"`
	Value     string `json:"value"`
	Behaviour string `json:"behaviour"`
}

// PreviewRows is every object d answers, in the order a walk finds them, as
// they read once it has run for since. Only what makes the answers is checked:
// the model, its parameters and d's own values.
func PreviewRows(d Device, since time.Duration) ([]PreviewRow, error) {
	objs, err := d.objects()
	if err != nil {
		return nil, err
	}
	t, err := newTree(objs)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	c := clock{started: now.Add(-max(since, 0)), now: now}
	out := make([]PreviewRow, len(t.entries))
	for i, e := range t.entries {
		out[i] = PreviewRow{OID: e.oid.String(), Type: smiTypeName(e.typ), Value: shown(e.typ, e.val.read(c)),
			Behaviour: behaviourOf(e.val)}
	}
	return out, nil
}

// behaviourOf is how a reading moves, by the kind values.go makes it.
func behaviourOf(r Reading) string {
	switch r.(type) {
	case constant:
		return BehaviourStatic
	case counter:
		return BehaviourCounter
	case gauge, loadText:
		return BehaviourGauge
	case uptimeReading, secondsUp, engineSeconds:
		return BehaviourUptime
	case dateAndTime:
		return BehaviourClock
	}
	return BehaviourDerived
}

// smiTypeName is t by the name the SMI gives it, as a model file writes it.
func smiTypeName(t gosnmp.Asn1BER) string {
	switch t {
	case gosnmp.Integer:
		return "Integer"
	case gosnmp.OctetString:
		return "OctetString"
	case gosnmp.ObjectIdentifier:
		return "ObjectIdentifier"
	case gosnmp.IPAddress:
		return "IpAddress"
	case gosnmp.Counter32:
		return "Counter32"
	case gosnmp.Gauge32:
		return "Gauge32"
	case gosnmp.TimeTicks:
		return "TimeTicks"
	case gosnmp.Counter64:
		return "Counter64"
	case gosnmp.Opaque:
		return "Opaque"
	}
	return t.String()
}

// shown is a value as the preview writes it: an OCTET STRING as text when it
// is text and as its octets in hex when it is not — a MAC address, a bit
// string — and anything else as it is.
func shown(t gosnmp.Asn1BER, v any) string {
	var b []byte
	switch x := v.(type) {
	case []byte:
		b = x
	case string:
		if t != gosnmp.OctetString && t != gosnmp.Opaque {
			return x
		}
		b = []byte(x)
	default:
		return fmt.Sprint(v)
	}
	if printable(b) {
		return string(b)
	}
	octets := make([]string, len(b))
	for i, c := range b {
		octets[i] = hex.EncodeToString([]byte{c})
	}
	return strings.Join(octets, ":")
}
