package simulator

import (
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	"github.com/gosnmp/gosnmp"
)

// A device may be given values of its own, OID by OID: the sysDescr a test
// expects, a counter held at a value, an instance its model does not have.
// Each takes the place of the model's object at its OID, or joins them, and is
// written as a custom model's constant is — a type by the name the SMI gives
// it, and a value — so the two share one vocabulary. The agent's own objects
// are not a model's, and are refused here as they are in a model file.

// MaxOverrides bounds a device's own values: a handful of OIDs a test cares
// about, not a model.
const MaxOverrides = 64

// Override is a value a device answers at one OID in place of its model's.
type Override struct {
	OID string `json:"oid"`
	// Type is the SMI's name for it: Integer, OctetString, Counter32…
	Type string `json:"type"`
	// Value is a number for a numeric type and text for the others — or, with
	// Hex, an OctetString's octets in hex, colons and spaces allowed.
	Value string `json:"value"`
	Hex   bool   `json:"hex,omitempty"`
}

// object is o as an object a device holds, or what is wrong with it.
func (o Override) object() (Object, error) {
	id, err := parseOID(o.OID)
	if err != nil {
		return Object{}, err
	}
	if slices.ContainsFunc(agentSubtrees, id.hasPrefix) {
		return Object{}, fmt.Errorf("%s is the agent's own: it counts it, and no model sets it", id)
	}
	t, ok := customTypes[o.Type]
	if !ok {
		return Object{}, fmt.Errorf("%s: %q is not a type", id, o.Type)
	}
	if len(o.Value) > maxCustomValue {
		return Object{}, fmt.Errorf("%s: a value is at most %d octets", id, maxCustomValue)
	}
	var v any
	switch {
	case o.Hex:
		if t != gosnmp.OctetString && t != gosnmp.Opaque {
			return Object{}, fmt.Errorf("%s: hex is for an OctetString or an Opaque", id)
		}
		b, err := hex.DecodeString(strings.NewReplacer(":", "", " ", "").Replace(o.Value))
		if err != nil {
			return Object{}, fmt.Errorf("%s: %q is not octets in hex", id, o.Value)
		}
		v = b
	case t == gosnmp.Integer || t == gosnmp.Counter32 || t == gosnmp.Gauge32 || t == gosnmp.TimeTicks || t == gosnmp.Counter64:
		if v, err = numberOf(t, strings.TrimSpace(o.Value)); err != nil {
			return Object{}, fmt.Errorf("%s: %w", id, err)
		}
	case t == gosnmp.ObjectIdentifier || t == gosnmp.IPAddress:
		v = strings.TrimSpace(o.Value)
	default:
		v = o.Value
	}
	if err := checkValue(t, v); err != nil {
		return Object{}, fmt.Errorf("%s: %w", id, err)
	}
	return Object{OID: id.String(), Type: t, Value: Const(v)}, nil
}

// compileOverrides is a device's own values as objects, or the first thing
// wrong with them.
func compileOverrides(ovs []Override) ([]Object, error) {
	if len(ovs) > MaxOverrides {
		return nil, fmt.Errorf("a device has at most %d values of its own", MaxOverrides)
	}
	out := make([]Object, 0, len(ovs))
	for _, o := range ovs {
		obj, err := o.object()
		if err != nil {
			return nil, err
		}
		if slices.ContainsFunc(out, func(x Object) bool { return x.OID == obj.OID }) {
			return nil, fmt.Errorf("%s is given twice", obj.OID)
		}
		out = append(out, obj)
	}
	return out, nil
}

// withOverrides is objs with each override in place of the object at its OID,
// or beside them when the model has none there. An object only a notification
// carried stays one.
func withOverrides(objs, ovs []Object) []Object {
	if len(ovs) == 0 {
		return objs
	}
	at := make(map[string]int, len(objs))
	for i, o := range objs {
		if id, err := parseOID(o.OID); err == nil {
			at[id.String()] = i
		}
	}
	out := slices.Clone(objs)
	for _, ov := range ovs {
		if i, ok := at[ov.OID]; ok {
			ov.NotifyOnly = out[i].NotifyOnly
			out[i] = ov
			continue
		}
		out = append(out, ov)
	}
	return out
}
