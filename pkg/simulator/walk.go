package simulator

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/gosnmp/gosnmp"
)

// A recorded walk is what a real device answered, kept so that a simulated one
// answers the same: snmpsim's .snmprec (OID|tag|value), or what net-snmp's
// snmpwalk prints with -On. Which of the two a file is, is read from its first
// line, never from its name.
//
// A walk is a recording, not a file somebody wrote object by object, so it is
// read leniently where a model file is read strictly: a line whose value cannot
// be read is left out and counted, and the import says how many and where the
// first one was — one odd line in forty thousand must not refuse the rest. What
// is refused is a file that is not a walk at all.

// MaxWalkBytes bounds one walk, and MaxRecordedObjects what the walks of a
// package may record between them: a switch's whole agent is some tens of
// thousands, and every device made from the package holds each one.
const (
	MaxWalkBytes       = 16 << 20
	MaxRecordedObjects = 1 << 16
)

// walkEntry is one object a walk recorded, its value in the Go type Const
// holds for its type.
type walkEntry struct {
	oid  oid
	name string // oid.String(), made once rather than for every device
	typ  gosnmp.Asn1BER
	val  any
}

// walkFile is what one walk gave.
type walkFile struct {
	entries []walkEntry
	// skipped counts the values that could not be read, and first says where
	// the first of them was and why.
	skipped int
	first   string
}

func (w *walkFile) skip(line int, err error) {
	if w.skipped == 0 {
		w.first = fmt.Sprintf("line %d: %v", line, err)
	}
	w.skipped++
}

func (w *walkFile) add(e walkEntry) error {
	if len(w.entries) == MaxRecordedObjects {
		return fmt.Errorf("the walk records more than %d objects", MaxRecordedObjects)
	}
	w.entries = append(w.entries, e)
	return nil
}

var (
	snmprecLine = regexp.MustCompile(`^\.?[0-9]+(?:\.[0-9]+)+\|[0-9]`)
	// A varbind snmpwalk prints begins its line with the OID, however it is
	// written, and " = ".
	varbindLine = regexp.MustCompile(`^[^\s"=]+ =(?: |$)`)
	errNamed    = errors.New("the OID is named from a MIB: record the walk with snmpwalk -On")
)

// readWalk reads a walk in either format.
func readWalk(data []byte) (walkFile, error) {
	if len(data) > MaxWalkBytes {
		return walkFile{}, fmt.Errorf("a walk is at most %d MB", MaxWalkBytes>>20)
	}
	lines := strings.Split(string(bytes.TrimPrefix(data, bom)), "\n")
	for i := range lines {
		lines[i] = strings.TrimSuffix(lines[i], "\r")
	}
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		switch {
		case snmprecLine.MatchString(l):
			return readSnmprec(lines)
		case varbindLine.MatchString(l):
			return readSnmpwalk(lines)
		}
		break
	}
	return walkFile{}, errors.New("this is not a walk: neither snmpsim's .snmprec (OID|tag|value) nor what snmpwalk prints (OID = TYPE: value)")
}

// readSnmprec reads snmpsim's format: OID|tag|value, the tag the ASN.1 one in
// decimal, with an x after it when the value is written in hex.
func readSnmprec(lines []string) (walkFile, error) {
	var w walkFile
	for n, line := range lines {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		e, ok, err := snmprecEntry(line)
		switch {
		case err != nil:
			w.skip(n+1, err)
		case ok:
			if err := w.add(e); err != nil {
				return walkFile{}, err
			}
		}
	}
	return w, nil
}

func snmprecEntry(line string) (walkEntry, bool, error) {
	name, rest, _ := strings.Cut(line, "|")
	tag, value, ok := strings.Cut(rest, "|")
	if !ok {
		return walkEntry{}, false, errors.New("a line of a .snmprec is OID|tag|value")
	}
	id, err := parseOID(name)
	if err != nil {
		return walkEntry{}, false, err
	}
	if strings.Contains(tag, ":") {
		return walkEntry{}, false, errors.New("snmpsim's variation modules are not read: record the value itself")
	}
	hexed := strings.HasSuffix(tag, "x")
	n, err := strconv.ParseUint(strings.TrimSuffix(tag, "x"), 10, 8)
	if err != nil {
		return walkEntry{}, false, fmt.Errorf("%q is not an ASN.1 tag", tag)
	}
	t := gosnmp.Asn1BER(n)
	if noValue(t) {
		return walkEntry{}, false, nil
	}
	var raw []byte
	if hexed {
		if raw, err = hex.DecodeString(value); err != nil {
			return walkEntry{}, false, fmt.Errorf("the value is not hex: %w", err)
		}
	}
	return entryOf(id, t, value, raw, hexed)
}

// noValue is a tag that says there was no value: an exception a GET answers,
// or a NULL.
func noValue(t gosnmp.Asn1BER) bool {
	switch t {
	case gosnmp.Null, gosnmp.NoSuchObject, gosnmp.NoSuchInstance, gosnmp.EndOfMibView:
		return true
	}
	return false
}

// entryOf makes an entry of a value written as text or, when octets is set,
// given as the octets in raw.
func entryOf(id oid, t gosnmp.Asn1BER, text string, raw []byte, octets bool) (walkEntry, bool, error) {
	if t == gosnmp.Uinteger32 {
		t = gosnmp.Gauge32 // SNMPv1's UInteger32, which SNMPv2 sends as a Gauge32
	}
	if octets && t != gosnmp.OctetString && t != gosnmp.Opaque && t != gosnmp.IPAddress {
		return walkEntry{}, false, fmt.Errorf("a %v is not written in hex", t)
	}
	var v any
	var err error
	switch t {
	case gosnmp.Integer:
		var n int64
		if n, err = strconv.ParseInt(text, 10, 32); err == nil {
			v = int(n)
		}
	case gosnmp.OctetString, gosnmp.Opaque:
		v = text
		if octets {
			v = raw
		}
	case gosnmp.ObjectIdentifier:
		var o oid
		if o, err = parseOID(strings.TrimSpace(text)); err == nil {
			v = o.String()
		}
	case gosnmp.IPAddress:
		v, err = ipv4Of(text, raw, octets)
	case gosnmp.Counter32, gosnmp.Gauge32, gosnmp.TimeTicks:
		var n uint64
		if n, err = strconv.ParseUint(text, 10, 32); err == nil {
			v = uint32(n)
		}
	case gosnmp.Counter64:
		v, err = strconv.ParseUint(text, 10, 64)
	default:
		return walkEntry{}, false, fmt.Errorf("%v is not a type an object is sent as", t)
	}
	if err != nil {
		return walkEntry{}, false, fmt.Errorf("%q is not a %v", text, t)
	}
	return walkEntry{oid: id, name: id.String(), typ: t, val: v}, true, nil
}

func ipv4Of(text string, raw []byte, octets bool) (string, error) {
	if octets {
		if len(raw) != 4 {
			return "", errors.New("an IpAddress is four octets")
		}
		return netip.AddrFrom4([4]byte(raw)).String(), nil
	}
	a, err := netip.ParseAddr(strings.TrimSpace(text))
	if err != nil || !a.Is4() {
		return "", errors.New("an IpAddress is IPv4")
	}
	return a.String(), nil
}

// readSnmpwalk reads what net-snmp's snmpwalk prints: OID = TYPE: value, a
// varbind a line, except a string holding a line break and a Hex-STRING long
// enough to wrap, which carry on over the lines after.
func readSnmpwalk(lines []string) (walkFile, error) {
	var w walkFile
	var entry []string
	start, open := 0, false
	flush := func() error {
		if len(entry) == 0 {
			return nil
		}
		e, ok, err := snmpwalkEntry(strings.Join(entry, "\n"))
		switch {
		case err != nil:
			w.skip(start, err)
		case ok:
			return w.add(e)
		}
		return nil
	}
	for n, line := range lines {
		switch {
		case open:
			entry = append(entry, line)
			open = !strings.HasSuffix(strings.TrimRight(line, " \t"), `"`)
		case varbindLine.MatchString(line):
			if err := flush(); err != nil {
				return walkFile{}, err
			}
			entry, start, open = []string{line}, n+1, opensString(line)
		case strings.TrimSpace(line) == "", strings.HasPrefix(line, "#"):
		case len(entry) > 0:
			entry = append(entry, line) // a Hex-STRING that wrapped
		}
	}
	if err := flush(); err != nil {
		return walkFile{}, err
	}
	return w, nil
}

// opensString reports whether a varbind's line opens a string it does not
// close: one that holds a line break.
func opensString(line string) bool {
	_, value, _ := strings.Cut(line, " = ")
	s, ok := strings.CutPrefix(value, `STRING: "`)
	return ok && !strings.HasSuffix(strings.TrimRight(s, " \t"), `"`)
}

func snmpwalkEntry(entry string) (walkEntry, bool, error) {
	name, value, _ := strings.Cut(entry, " =")
	value = strings.TrimPrefix(value, " ")
	// iso is how snmpwalk writes 1 without -On, the rest of the OID numeric.
	if rest, ok := strings.CutPrefix(name, "iso"); ok && (rest == "" || rest[0] == '.') {
		name = "1" + rest
	}
	id, err := parseOID(name)
	if err != nil {
		if strings.ContainsFunc(name, unicode.IsLetter) {
			return walkEntry{}, false, errNamed
		}
		return walkEntry{}, false, err
	}
	switch {
	case value == `""`:
		return walkEntry{oid: id, name: id.String(), typ: gosnmp.OctetString, val: ""}, true, nil
	case value == "NULL", strings.HasPrefix(value, "No Such "), strings.HasPrefix(value, "No more variables"):
		return walkEntry{}, false, nil
	}
	// What an agent sent under a type its MIB does not give the object.
	if rest, ok := strings.CutPrefix(value, "Wrong Type"); ok {
		if _, after, ok := strings.Cut(rest, "): "); ok {
			value = after
		}
	}
	kind, text, _ := strings.Cut(value, ": ")
	kind = strings.TrimSuffix(kind, ":")
	switch kind {
	case "STRING":
		return entryOf(id, gosnmp.OctetString, unquote(text), nil, false)
	case "Hex-STRING", "BITS":
		b, err := octetsOf(text)
		if err != nil {
			return walkEntry{}, false, err
		}
		return entryOf(id, gosnmp.OctetString, "", b, true)
	case "Opaque":
		b, err := octetsOf(text)
		if err != nil {
			return walkEntry{}, false, errors.New("an Opaque is read when snmpwalk prints its octets")
		}
		return entryOf(id, gosnmp.Opaque, "", b, true)
	case "INTEGER":
		return entryOf(id, gosnmp.Integer, numberIn(text), nil, false)
	case "Gauge32", "Unsigned32", "UInteger32":
		return entryOf(id, gosnmp.Gauge32, numberIn(text), nil, false)
	case "Counter32":
		return entryOf(id, gosnmp.Counter32, numberIn(text), nil, false)
	case "Counter64":
		return entryOf(id, gosnmp.Counter64, numberIn(text), nil, false)
	case "Timeticks":
		return entryOf(id, gosnmp.TimeTicks, numberIn(text), nil, false)
	case "OID":
		o := strings.TrimSpace(text)
		if rest, ok := strings.CutPrefix(o, "iso"); ok && (rest == "" || rest[0] == '.') {
			o = "1" + rest
		}
		return entryOf(id, gosnmp.ObjectIdentifier, o, nil, false)
	case "IpAddress":
		return entryOf(id, gosnmp.IPAddress, text, nil, false)
	case "Network Address":
		b, err := hex.DecodeString(strings.ReplaceAll(strings.TrimSpace(text), ":", ""))
		if err != nil {
			return walkEntry{}, false, errors.New("a Network Address is four octets in hex")
		}
		return entryOf(id, gosnmp.IPAddress, "", b, true)
	}
	return walkEntry{}, false, fmt.Errorf("%q is not a type snmpwalk prints", kind)
}

// unquote is a string without the quotes snmpwalk prints around it. The
// quotes inside it are not escaped, so only the outer two are taken off.
func unquote(text string) string {
	text = strings.TrimRight(text, " \t")
	if len(text) >= 2 && text[0] == '"' && text[len(text)-1] == '"' {
		return text[1 : len(text)-1]
	}
	return text
}

// octetsOf reads the octets snmpwalk prints in hex, two digits each, over as
// many lines as they take. What BITS prints after them — the names of the bits
// that are set — is not octets, and ends them.
func octetsOf(text string) ([]byte, error) {
	out := []byte{}
	for _, f := range strings.Fields(text) {
		b, err := hex.DecodeString(f)
		if err != nil || len(b) != 1 {
			break
		}
		out = append(out, b[0])
	}
	if len(out) == 0 && strings.TrimSpace(text) != "" {
		return nil, errors.New("no octets in hex")
	}
	return out, nil
}

// numberIn is the number snmpwalk prints a value as, without what it prints
// around it: an enumeration's label — up(1) —, the units a MIB gives, the time
// a TimeTicks spells out after it, or the point a DISPLAY-HINT such as d-2
// puts in it, which the number on the wire does not have.
func numberIn(text string) string {
	text = strings.TrimSpace(text)
	if open := strings.IndexByte(text, '('); open >= 0 {
		if end := strings.IndexByte(text[open:], ')'); end > 0 {
			return text[open+1 : open+end]
		}
	}
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return ""
	}
	n := fields[0]
	if whole, frac, ok := strings.Cut(n, "."); ok && digits(strings.TrimPrefix(whole, "-")) && digits(frac) {
		n = whole + frac
	}
	return n
}

func digits(s string) bool {
	return s != "" && strings.Trim(s, "0123456789") == ""
}
