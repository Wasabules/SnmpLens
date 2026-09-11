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

// MaxPreviewRows bounds a preview: a table on a screen, not a walk.
const MaxPreviewRows = 500

// PreviewRow is one object as a device answers it, its value written out.
type PreviewRow struct {
	OID   string `json:"oid"`
	Type  string `json:"type"`
	Value string `json:"value"`
}

// Preview is what a device answers under a subtree.
type Preview struct {
	Rows []PreviewRow `json:"rows"`
	// Total is how many objects the subtree holds; Rows are the first of them.
	Total int `json:"total"`
}

// PreviewDevice is what d would answer under subtree — everywhere when it is
// empty — the moment it starts, in at most limit rows. Only what makes the
// answers is checked: the model, its parameters and d's own values.
func PreviewDevice(d Device, subtree string, limit int) (Preview, error) {
	var root oid
	if s := strings.TrimSpace(subtree); s != "" {
		var err error
		if root, err = parseArcs(s); err != nil {
			return Preview{}, err
		}
	}
	objs, err := d.objects()
	if err != nil {
		return Preview{}, err
	}
	t, err := newTree(objs)
	if err != nil {
		return Preview{}, err
	}
	if limit < 1 || limit > MaxPreviewRows {
		limit = MaxPreviewRows
	}
	now := time.Now()
	c := clock{started: now, now: now}
	out := Preview{Rows: []PreviewRow{}}
	for _, e := range t.entries {
		if !e.oid.hasPrefix(root) {
			continue
		}
		out.Total++
		if len(out.Rows) < limit {
			out.Rows = append(out.Rows, PreviewRow{OID: e.oid.String(), Type: smiTypeName(e.typ), Value: shown(e.typ, e.val.read(c))})
		}
	}
	return out, nil
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
