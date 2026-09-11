package simulator

import (
	"fmt"
	"math"
	"net/netip"
	"slices"
	"time"

	"github.com/gosnmp/gosnmp"
)

// Object is one instance a simulated agent answers for: its full OID, down to
// the instance sub-identifiers, the SMI type it is sent as, and what it reads.
type Object struct {
	OID   string
	Type  gosnmp.Asn1BER
	Value Reading
}

// Reading is what an object answers when it is asked.
//
// It is computed from the agent's clock at the moment of the request and never
// ticked in the background: an idle simulator costs nothing, and a reading is
// the same whether it is polled every second or once a day. The set is closed —
// the methods are unexported — for the reason a preset's widget kinds are: a
// device file will PICK a behaviour by name and cannot describe one.
type Reading interface {
	read(c clock) any
	// check reports whether the reading can be sent as t. It runs when the tree
	// is built, because a value gosnmp cannot encode fails the whole RESPONSE,
	// and an agent with nothing to send says nothing: a timeout, far from the
	// object that caused it.
	check(t gosnmp.Asn1BER) error
}

// clock is what a reading may depend on.
type clock struct {
	started time.Time // when the agent started answering
	now     time.Time
}

// uptime is the time since the agent started, in hundredths of a second.
// TimeTicks is 32 bits of hundredths and wraps after 497 days (RFC 2578
// 7.1.8); the conversion is that modulo.
func (c clock) uptime() uint32 {
	return uint32(c.now.Sub(c.started) / (10 * time.Millisecond))
}

// Const answers the same value every time, in the Go type gosnmp encodes the
// object's type from: int for Integer, uint32 for Counter32, Gauge32 and
// TimeTicks, uint64 for Counter64, string or []byte for OCTET STRING and
// Opaque, a dotted string for an OBJECT IDENTIFIER and an IPv4 literal for an
// IpAddress.
func Const(v any) Reading { return constant{v} }

type constant struct{ v any }

func (k constant) read(clock) any               { return k.v }
func (k constant) check(t gosnmp.Asn1BER) error { return checkValue(t, k.v) }

// Uptime answers the time since the agent started, as TimeTicks: sysUpTime,
// and anything else a device counts from its own boot.
func Uptime() Reading { return uptimeReading{} }

type uptimeReading struct{}

func (uptimeReading) read(c clock) any { return c.uptime() }

func (uptimeReading) check(t gosnmp.Asn1BER) error {
	if t != gosnmp.TimeTicks {
		return fmt.Errorf("an uptime is TimeTicks, not %v", t)
	}
	return nil
}

// checkValue reports whether v can be sent as t.
//
// Stricter than gosnmp on purpose: a value it would accept and then fail on, or
// panic on — it takes a byte for an Integer and then asserts it to an int —
// must be refused here, where the object is named.
func checkValue(t gosnmp.Asn1BER, v any) error {
	switch t {
	case gosnmp.Integer:
		n, ok := v.(int)
		if !ok {
			return fmt.Errorf("an Integer holds an int, not %T", v)
		}
		if n < math.MinInt32 || n > math.MaxInt32 {
			return fmt.Errorf("%d does not fit an Integer32", n)
		}
	case gosnmp.OctetString, gosnmp.Opaque:
		switch v.(type) {
		case string, []byte:
		default:
			return fmt.Errorf("%v holds a string or bytes, not %T", t, v)
		}
	case gosnmp.ObjectIdentifier:
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("an OBJECT IDENTIFIER holds a dotted string, not %T", v)
		}
		if _, err := parseOID(s); err != nil {
			return err
		}
	case gosnmp.IPAddress:
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("an IpAddress holds a dotted string, not %T", v)
		}
		if ip, err := netip.ParseAddr(s); err != nil || !ip.Is4() {
			return fmt.Errorf("an IpAddress is IPv4 (RFC 2578 7.1.5), not %q", s)
		}
	case gosnmp.Counter32, gosnmp.Gauge32, gosnmp.TimeTicks:
		if _, ok := v.(uint32); !ok {
			return fmt.Errorf("%v holds a uint32, not %T", t, v)
		}
	case gosnmp.Counter64:
		if _, ok := v.(uint64); !ok {
			return fmt.Errorf("a Counter64 holds a uint64, not %T", v)
		}
	default:
		return fmt.Errorf("%v is not a type an object is sent as", t)
	}
	return nil
}

// tree is a device's objects in the order an agent walks them.
type tree struct {
	entries []entry
	// objects holds the object each instance belongs to — its OID less the last
	// arc — which is what tells noSuchInstance from noSuchObject.
	objects map[string]bool
}

type entry struct {
	oid  oid
	name string // the OID as gosnmp names a varbind
	typ  gosnmp.Asn1BER
	val  Reading
}

func (e *entry) varbind(c clock) gosnmp.SnmpPDU {
	return gosnmp.SnmpPDU{Name: e.name, Type: e.typ, Value: e.val.read(c)}
}

func newTree(objects []Object) (*tree, error) {
	t := &tree{entries: make([]entry, 0, len(objects)), objects: make(map[string]bool, len(objects))}
	for _, o := range objects {
		id, err := parseOID(o.OID)
		if err != nil {
			return nil, err
		}
		if o.Value == nil {
			return nil, fmt.Errorf("%s: no value", id)
		}
		if err := o.Value.check(o.Type); err != nil {
			return nil, fmt.Errorf("%s: %w", id, err)
		}
		t.entries = append(t.entries, entry{oid: id, name: id.String(), typ: o.Type, val: o.Value})
		t.objects[id[:len(id)-1].String()] = true
	}
	slices.SortFunc(t.entries, func(a, b entry) int { return slices.Compare(a.oid, b.oid) })
	// Sorted, anything lying under an instance comes straight after it.
	for i := 1; i < len(t.entries); i++ {
		prev, cur := t.entries[i-1].oid, t.entries[i].oid
		switch {
		case slices.Equal(prev, cur):
			return nil, fmt.Errorf("%s is declared twice", cur)
		case cur.hasPrefix(prev):
			// An instance is a leaf: one under another is a tree no agent has.
			return nil, fmt.Errorf("%s lies under %s, which is an instance itself", cur, prev)
		}
	}
	return t, nil
}

// find returns where o is, or would be, among the entries.
func (t *tree) find(o oid) (int, bool) {
	return slices.BinarySearchFunc(t.entries, o, func(e entry, o oid) int { return slices.Compare(e.oid, o) })
}

// get returns the object named exactly o: a GET.
func (t *tree) get(o oid) *entry {
	if i, ok := t.find(o); ok {
		return &t.entries[i]
	}
	return nil
}

// next returns the first object after o that skip does not refuse: a GETNEXT.
func (t *tree) next(o oid, skip func(*entry) bool) *entry {
	i, ok := t.find(o)
	if ok {
		i++
	}
	for ; i < len(t.entries); i++ {
		if e := &t.entries[i]; skip == nil || !skip(e) {
			return e
		}
	}
	return nil
}

// missing is the exception a GET for o earns when no object answers it
// (RFC 3416 4.2.1): noSuchInstance when o names an instance of an object this
// agent has, noSuchObject when it names none.
//
// Without the MIB, an instance's object is its OID less the last arc. That is
// exact for scalars and for tables with a one-arc index, and too short for a
// table indexed by several arcs, where a row that does not exist reads as
// noSuchObject rather than noSuchInstance. Both mean there is nothing there.
func (t *tree) missing(o oid) gosnmp.Asn1BER {
	for n := len(o); n >= 2; n-- {
		if t.objects[o[:n].String()] {
			return gosnmp.NoSuchInstance
		}
	}
	return gosnmp.NoSuchObject
}
