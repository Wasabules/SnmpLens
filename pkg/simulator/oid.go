package simulator

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// oid is an object identifier as its arcs.
//
// Compared arc by arc and never as text (slices.Compare, which is exactly the
// order an agent walks in: arc by arc, a prefix before everything under it).
// "1.3.6.1.2.1.2.2.1.10" follows "1.3.6.1.2.1.2.2.1.2", and a string comparison
// puts it first — a GETNEXT that skips a column or walks in a circle.
type oid []uint32

// maxArcs bounds a parsed OID: RFC 2578 3.5 caps a name at 128 sub-identifiers.
const maxArcs = 128

// parseOID reads the OID of an object a device holds, which must be one BER can
// carry.
func parseOID(s string) (oid, error) {
	out, err := parseArcs(s)
	if err != nil {
		return nil, err
	}
	if len(out) < 2 {
		return nil, fmt.Errorf("OID %q: an OID has at least two arcs", s)
	}
	// BER packs the first two arcs into one sub-identifier as 40*X+Y, which
	// reads back only for X up to 2 and, below 2, Y under 40 (X.690 8.19.4).
	if out[0] > 2 || (out[0] < 2 && out[1] >= 40) {
		return nil, fmt.Errorf("OID %q: no OID starts %d.%d", s, out[0], out[1])
	}
	return out, nil
}

// parseArcs reads a name a manager sent, only to find its place among the
// objects. It is lenient where parseOID is strict: a GETNEXT from ".1" is how a
// walk of everything starts, and a name no object could have still has a place
// in the order.
func parseArcs(s string) (oid, error) {
	t := strings.TrimPrefix(strings.TrimSpace(s), ".")
	if t == "" {
		return nil, fmt.Errorf("empty OID")
	}
	parts := strings.Split(t, ".")
	if len(parts) > maxArcs {
		return nil, fmt.Errorf("OID %q: more than %d arcs", s, maxArcs)
	}
	out := make(oid, len(parts))
	for i, p := range parts {
		v, err := strconv.ParseUint(p, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("OID %q: %q is not a sub-identifier", s, p)
		}
		out[i] = uint32(v)
	}
	return out, nil
}

// String writes the OID the way gosnmp names a varbind, with a leading dot.
func (o oid) String() string {
	var b strings.Builder
	for _, a := range o {
		b.WriteByte('.')
		b.WriteString(strconv.FormatUint(uint64(a), 10))
	}
	return b.String()
}

// hasPrefix reports whether o lies under p, or is p.
func (o oid) hasPrefix(p oid) bool {
	return len(o) >= len(p) && slices.Equal(o[:len(p)], p)
}
