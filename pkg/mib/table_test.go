package mib

import (
	"os"
	"testing"

	"github.com/sleepinggenius2/gosmi"
	"github.com/sleepinggenius2/gosmi/parser"
	"github.com/sleepinggenius2/gosmi/types"
)

// Against the real bundled MIBs, because the whole point of decoding an index
// is that the rules come from the MIB. A hand-written fixture would only test
// that this file agrees with itself.
func loadedService(t *testing.T) *Service {
	t.Helper()
	if _, err := os.Stat("../../mibs"); err != nil {
		t.Skip("no bundled corpus")
	}
	// gosmi is global and needs its search path set before a module resolves;
	// app.startup does this at runtime, so a test that skips it loads nothing
	// and every assertion below turns into "not in any loaded MIB".
	// Exit before Init, the same sequence Rebuild uses: gosmi has no unload,
	// and Init on its own finds the existing handle and returns with every
	// previously loaded module still in it. Without this, a stub SNMPv2-SMI
	// from one test is what the next test's IF-MIB gets built against.
	gosmi.Exit()
	gosmi.Init()
	gosmi.SetPath("../../mibs")

	s := NewService("../../mibs")
	if _, err := s.LoadAll(); err != nil {
		t.Skipf("could not load the corpus: %v", err)
	}
	return s
}

// ifTable: one integer index. The simplest shape, and the one that already
// worked — it has to keep working.
func TestTableIfTable(t *testing.T) {
	s := loadedService(t)

	info, err := s.Table("1.3.6.1.2.1.2.2")
	if err != nil {
		t.Fatalf("ifTable: %v", err)
	}
	if info.Name != "ifTable" || info.RowName != "ifEntry" {
		t.Errorf("resolved to %s / %s", info.Name, info.RowName)
	}
	if len(info.Index) != 1 || info.Index[0].Name != "ifIndex" {
		t.Fatalf("INDEX = %+v", info.Index)
	}
	if len(info.Columns) < 20 {
		t.Errorf("%d columns, expected the full ifEntry", len(info.Columns))
	}

	got := s.DecodeIndexes("1.3.6.1.2.1.2.2", []string{"1", "42"})
	if got[1].Error != "" || len(got[1].Parts) != 1 {
		t.Fatalf("decode: %+v", got[1])
	}
	if got[1].Parts[0].Display != "42" || got[1].Parts[0].Sort != 42 {
		t.Errorf("index 42 decoded as %+v", got[1].Parts[0])
	}
}

// Selecting a column in the tree must find its table: nobody clicks the table
// node, they click ifDescr.
func TestTableResolvesFromAColumnAndARow(t *testing.T) {
	s := loadedService(t)
	for _, oid := range []string{
		"1.3.6.1.2.1.2.2",     // ifTable
		"1.3.6.1.2.1.2.2.1",   // ifEntry
		"1.3.6.1.2.1.2.2.1.2", // ifDescr
	} {
		info, err := s.Table(oid)
		if err != nil {
			t.Errorf("%s: %v", oid, err)
			continue
		}
		if info.Name != "ifTable" {
			t.Errorf("%s resolved to %s", oid, info.Name)
		}
	}
}

// tcpConnTable is the case that motivated this: INDEX { localAddress,
// localPort, remAddress, remPort } — four objects, two of them addresses. It
// used to render as one opaque "10.0.0.5.161.192.168.1.9.50000".
func TestTableDecodesAFourPartIndex(t *testing.T) {
	s := loadedService(t)

	info, err := s.Table("1.3.6.1.2.1.6.13")
	if err != nil {
		t.Skipf("tcpConnTable not in the corpus: %v", err)
	}
	if len(info.Index) != 4 {
		t.Fatalf("INDEX = %+v", info.Index)
	}

	got := s.DecodeIndexes(info.Oid, []string{"10.0.0.5.161.192.168.1.9.50000"})[0]
	if got.Error != "" {
		t.Fatalf("decode failed: %s", got.Error)
	}
	want := []string{"10.0.0.5", "161", "192.168.1.9", "50000"}
	if len(got.Parts) != 4 {
		t.Fatalf("%d parts: %+v", len(got.Parts), got.Parts)
	}
	for i, w := range want {
		if got.Parts[i].Display != w {
			t.Errorf("part %d (%s) = %q, want %q", i, got.Parts[i].Name, got.Parts[i].Display, w)
		}
	}
	// An address is four sub-identifiers with NO length prefix. Reading one as
	// length-prefixed shifts everything after it, which is exactly the failure
	// this guards: the ports would come out as garbage.
	if got.Parts[1].Sort != 161 || !got.Parts[1].Numeric {
		t.Errorf("the port did not decode as a number: %+v", got.Parts[1])
	}
}

// An IpAddress-keyed table: four sub-identifiers rendered as an address, not
// as four numbers.
func TestTableDecodesAnAddressIndex(t *testing.T) {
	s := loadedService(t)

	info, err := s.Table("1.3.6.1.2.1.4.20") // ipAddrTable
	if err != nil {
		t.Skipf("ipAddrTable not in the corpus: %v", err)
	}
	got := s.DecodeIndexes(info.Oid, []string{"192.168.1.1"})[0]
	if got.Error != "" || len(got.Parts) != 1 {
		t.Fatalf("decode: %+v", got)
	}
	if got.Parts[0].Display != "192.168.1.1" {
		t.Errorf("address rendered as %q", got.Parts[0].Display)
	}
}

// ipNetToMediaTable: INDEX { ifIndex, netAddress } — an integer followed by an
// address, so the split point matters.
func TestTableDecodesAMixedIndex(t *testing.T) {
	s := loadedService(t)

	info, err := s.Table("1.3.6.1.2.1.4.22")
	if err != nil {
		t.Skipf("ipNetToMediaTable not in the corpus: %v", err)
	}
	got := s.DecodeIndexes(info.Oid, []string{"3.10.0.0.254"})[0]
	if got.Error != "" || len(got.Parts) != 2 {
		t.Fatalf("decode: %+v", got)
	}
	if got.Parts[0].Display != "3" || got.Parts[1].Display != "10.0.0.254" {
		t.Errorf("decoded as %q / %q", got.Parts[0].Display, got.Parts[1].Display)
	}
}

// A malformed instance must be reported, not silently mis-split. Half a
// decoded index is worse than none: it looks right.
func TestTableReportsUndecodableInstances(t *testing.T) {
	s := loadedService(t)
	info, err := s.Table("1.3.6.1.2.1.6.13")
	if err != nil {
		t.Skip("tcpConnTable not in the corpus")
	}

	for _, bad := range []string{"1", "1.2.3", "10.0.0.5.161.192.168.1.9.50000.7", "", "x"} {
		got := s.DecodeIndexes(info.Oid, []string{bad})[0]
		if got.Error == "" {
			t.Errorf("%q was decoded as %+v with no error", bad, got.Parts)
		}
		if got.Raw != bad {
			t.Errorf("the raw instance was not preserved: %q", got.Raw)
		}
	}
}

// A row that AUGMENTS another is indexed by the other's INDEX. ifXTable
// augments ifEntry, so it must decode with ifIndex and not report "no INDEX".
func TestTableFollowsAugments(t *testing.T) {
	s := loadedService(t)

	info, err := s.Table("1.3.6.1.2.1.31.1.1") // ifXTable
	if err != nil {
		t.Skipf("ifXTable not in the corpus: %v", err)
	}
	if info.Augments == "" {
		t.Errorf("ifXTable should report what it augments, got %+v", info)
	}
	if len(info.Index) != 1 || info.Index[0].Name != "ifIndex" {
		t.Fatalf("INDEX = %+v", info.Index)
	}
	got := s.DecodeIndexes(info.Oid, []string{"7"})[0]
	if got.Error != "" || got.Parts[0].Display != "7" {
		t.Errorf("decode: %+v", got)
	}
}

// A scalar is not a table, and saying so beats rendering an empty grid.
func TestTableRefusesWhatIsNotATable(t *testing.T) {
	s := loadedService(t)
	for _, oid := range []string{"1.3.6.1.2.1.1.1", "1.3.6.1.2.1.1", "9.9.9.9.9"} {
		if info, err := s.Table(oid); err == nil {
			t.Errorf("%s was accepted as table %s", oid, info.Name)
		}
	}
}

// Decoding must survive an unknown table rather than failing the whole render:
// a walk of a vendor OID with no MIB loaded should still show its rows.
func TestDecodeIndexesFallsBackToTheRawInstance(t *testing.T) {
	s := loadedService(t)
	got := s.DecodeIndexes("1.3.6.1.4.1.99999.1", []string{"1.2", "3"})
	if len(got) != 2 {
		t.Fatalf("%d results", len(got))
	}
	for _, g := range got {
		if g.Error == "" {
			t.Errorf("an unknown table decoded silently: %+v", g)
		}
		if g.Raw == "" {
			t.Errorf("the raw instance must survive: %+v", g)
		}
	}
}

// The three things table editing needs to know.
func TestTableReportsWhatCanBeWritten(t *testing.T) {
	s := loadedService(t)

	// ifTable has no RowStatus: rows cannot be created.
	info, err := s.Table("1.3.6.1.2.1.2.2")
	if err != nil {
		t.Fatal(err)
	}
	if info.RowStatusOid != "" {
		t.Errorf("ifTable reported a RowStatus column: %s", info.RowStatusOid)
	}
	// ifAdminStatus is read-write and must be reported as such, or the editor
	// offers nothing on a table that is in fact writable.
	var found bool
	for _, c := range info.Columns {
		if c.Name == "ifAdminStatus" {
			found = true
			if !c.Writable {
				t.Errorf("ifAdminStatus reported as read-only: %+v", c)
			}
			if len(c.EnumValues) == 0 {
				t.Errorf("ifAdminStatus lost its enum: %+v", c)
			}
		}
		if c.Name == "ifIndex" && !c.IsIndex {
			t.Errorf("ifIndex not marked as an index column")
		}
	}
	if !found {
		t.Error("ifAdminStatus missing from the columns")
	}
}

// Encoding and decoding must be the same rules read in both directions. A
// round trip over the awkward shapes is what holds them together: gosmi's own
// encoder writes a length prefix for a fixed-size index, which addresses a row
// that does not exist, and nothing but a round trip catches that.
func TestIndexRoundTrip(t *testing.T) {
	s := loadedService(t)

	cases := []struct {
		table  string
		values []string
		want   string
	}{
		{"1.3.6.1.2.1.2.2", []string{"42"}, "42"},                    // ifTable
		{"1.3.6.1.2.1.4.20", []string{"192.168.1.1"}, "192.168.1.1"}, // ipAddrTable
		{"1.3.6.1.2.1.4.22", []string{"3", "10.0.0.254"}, "3.10.0.0.254"},
		{"1.3.6.1.2.1.6.13", []string{"10.0.0.5", "161", "192.168.1.9", "50000"},
			"10.0.0.5.161.192.168.1.9.50000"},
	}

	for _, c := range cases {
		if _, err := s.Table(c.table); err != nil {
			t.Logf("skipping %s: %v", c.table, err)
			continue
		}
		got, err := s.EncodeIndex(c.table, c.values)
		if err != nil {
			t.Errorf("%s: encode %v: %v", c.table, c.values, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: encoded %v as %q, want %q", c.table, c.values, got, c.want)
			continue
		}
		back := s.DecodeIndexes(c.table, []string{got})[0]
		if back.Error != "" {
			t.Errorf("%s: %q did not decode back: %s", c.table, got, back.Error)
			continue
		}
		for i, want := range c.values {
			if back.Parts[i].Display != want {
				t.Errorf("%s part %d: round-tripped %q as %q", c.table, i, want, back.Parts[i].Display)
			}
		}
	}
}

// The wrong number of values is the mistake a person makes on a four-part
// index; it must be refused, not encoded into a row that does not exist.
func TestEncodeIndexRefusesBadInput(t *testing.T) {
	s := loadedService(t)
	if _, err := s.Table("1.3.6.1.2.1.6.13"); err != nil {
		t.Skip("tcpConnTable not in the corpus")
	}
	for _, values := range [][]string{
		{},
		{"10.0.0.5"},
		{"10.0.0.5", "161", "192.168.1.9"},
		{"10.0.0.5", "161", "192.168.1.9", "50000", "extra"},
		{"not-an-address", "161", "192.168.1.9", "50000"},
		{"10.0.0.5", "not-a-port", "192.168.1.9", "50000"},
		{"10.0.0.5", "161", "192.168.1.9", ""},
	} {
		if got, err := s.EncodeIndex("1.3.6.1.2.1.6.13", values); err == nil {
			t.Errorf("%v was encoded as %q", values, got)
		}
	}
}

// isWritable has no case for read-create, and must not need one.
//
// gosmi folds read-create into ReadWrite while parsing, and types.Access has
// no ReadCreate member — so a test for one by name could never match. That is
// upstream behaviour this package depends on rather than implements, so it is
// pinned here: if a future gosmi stops folding, a creatable table would
// silently show every column as read-only and offer no editing at all.
func TestGosmiFoldsReadCreateIntoReadWrite(t *testing.T) {
	if got := parser.AccessReadCreate.ToSmi(); got != types.AccessReadWrite {
		t.Fatalf("read-create maps to %v, want %v — isWritable now needs a case for it",
			got, types.AccessReadWrite)
	}
	if !isWritable(parser.AccessReadCreate.ToSmi()) {
		t.Error("a read-create column is not reported as writable")
	}
	for _, a := range []types.Access{
		types.AccessReadOnly, types.AccessNotAccessible,
		types.AccessNotify, types.AccessUnknown,
	} {
		if isWritable(a) {
			t.Errorf("%v was reported as writable", a)
		}
	}
}

// A table whose columns are read-create must come back editable, end to end.
func TestReadCreateTableIsReportedWritable(t *testing.T) {
	s := loadedService(t)
	// ipNetToMediaTable's columns are read-create in IP-MIB.
	info, err := s.Table("1.3.6.1.2.1.4.22")
	if err != nil {
		t.Skipf("ipNetToMediaTable not in the corpus: %v", err)
	}
	var writable int
	for _, c := range info.Columns {
		if c.Writable {
			writable++
		}
	}
	if writable == 0 {
		t.Errorf("no column of %s reported as writable: %+v", info.Name, info.Columns)
	}
}

// A row that AUGMENTS one with an IMPLIED index.
//
// No bundled MIB has this shape, and gosmi gets it wrong in a way that only
// shows here: GetIndex follows the augment, GetImplied does not, and the
// builder sets Implied only on a row carrying its own INDEX. So the
// augmenting row claims IMPLIED=false, its last index object is read as
// length-prefixed, and the two halves of one conceptual row disagree —
// EncodeIndex being the damaging half, since it then writes a length prefix
// that addresses a row which does not exist.
const impliedBase = `BASE-MIB DEFINITIONS ::= BEGIN
IMPORTS OBJECT-TYPE, Integer32, enterprises FROM SNMPv2-SMI;

baseTable OBJECT-TYPE
    SYNTAX      SEQUENCE OF BaseEntry
    MAX-ACCESS  not-accessible
    STATUS      current
    DESCRIPTION "a table keyed by a name"
    ::= { enterprises 77 }

baseEntry OBJECT-TYPE
    SYNTAX      BaseEntry
    MAX-ACCESS  not-accessible
    STATUS      current
    DESCRIPTION "one row"
    INDEX       { IMPLIED baseName }
    ::= { baseTable 1 }

BaseEntry ::= SEQUENCE { baseName OCTET STRING, baseValue Integer32 }

baseName OBJECT-TYPE
    SYNTAX      OCTET STRING
    MAX-ACCESS  read-only
    STATUS      current
    DESCRIPTION "the name"
    ::= { baseEntry 1 }

baseValue OBJECT-TYPE
    SYNTAX      Integer32
    MAX-ACCESS  read-write
    STATUS      current
    DESCRIPTION "a value"
    ::= { baseEntry 2 }
END
`

const impliedExt = `EXT-MIB DEFINITIONS ::= BEGIN
IMPORTS OBJECT-TYPE, Integer32, enterprises FROM SNMPv2-SMI
        baseEntry FROM BASE-MIB;

extTable OBJECT-TYPE
    SYNTAX      SEQUENCE OF ExtEntry
    MAX-ACCESS  not-accessible
    STATUS      current
    DESCRIPTION "an extension of baseTable"
    ::= { enterprises 78 }

extEntry OBJECT-TYPE
    SYNTAX      ExtEntry
    MAX-ACCESS  not-accessible
    STATUS      current
    DESCRIPTION "one row, keyed by baseEntry's index"
    AUGMENTS    { baseEntry }
    ::= { extTable 1 }

ExtEntry ::= SEQUENCE { extValue Integer32 }

extValue OBJECT-TYPE
    SYNTAX      Integer32
    MAX-ACCESS  read-write
    STATUS      current
    DESCRIPTION "a value"
    ::= { extEntry 1 }
END
`

func TestAugmentedRowInheritsImplied(t *testing.T) {
	s := diagDir(t, map[string]string{
		"SNMPv2-SMI": smiStub,
		"BASE-MIB":   impliedBase,
		"EXT-MIB":    impliedExt,
	})
	if _, err := s.LoadSpecific([]string{"SNMPv2-SMI", "BASE-MIB", "EXT-MIB"}); err != nil {
		t.Skipf("the fixtures did not load: %v", err)
	}

	base, err := s.Table("1.3.6.1.4.1.77")
	if err != nil {
		t.Skipf("baseTable did not resolve: %v", err)
	}
	ext, err := s.Table("1.3.6.1.4.1.78")
	if err != nil {
		t.Skipf("extTable did not resolve: %v", err)
	}

	if !base.Implied {
		t.Fatalf("the base row lost its own IMPLIED: %+v", base)
	}
	if ext.Augments == "" {
		t.Fatalf("extEntry does not report what it augments: %+v", ext)
	}
	if !ext.Implied {
		t.Errorf("the augmenting row reports IMPLIED=%v; it inherits the base row's index and must inherit its IMPLIED with it", ext.Implied)
	}

	// "abc" as an IMPLIED index is its bytes and nothing else.
	const instance = "97.98.99"
	for _, tbl := range []*TableInfo{base, ext} {
		got := s.DecodeIndexes(tbl.Oid, []string{instance})[0]
		if got.Error != "" {
			t.Errorf("%s: %q did not decode: %s", tbl.Name, instance, got.Error)
			continue
		}
		if len(got.Parts) != 1 || got.Parts[0].Display != "abc" {
			t.Errorf("%s: decoded as %+v, want abc", tbl.Name, got.Parts)
		}

		enc, err := s.EncodeIndex(tbl.Oid, []string{"abc"})
		if err != nil {
			t.Errorf("%s: encode: %v", tbl.Name, err)
			continue
		}
		if enc != instance {
			t.Errorf("%s: encoded %q as %q, want %q — a length prefix here addresses a row that does not exist",
				tbl.Name, "abc", enc, instance)
		}
	}
}

// What the screen shows must be what you can type back.
//
// A MAC-keyed table renders its index as 00:1b:44:11:3a:b7 (renderOctets, for
// anything not printable). Typing that into the New-row dialog used to be
// byte-copied: 17 octets, refused for a SIZE(6) index and — worse — accepted
// for a variable-length one, creating a row at an address nobody asked for.
func TestHexOctetIndexRoundTrips(t *testing.T) {
	cases := map[string]struct {
		want []uint32
		ok   bool
	}{
		"00:1b:44:11:3a:b7": {[]uint32{0x00, 0x1b, 0x44, 0x11, 0x3a, 0xb7}, true},
		"00-1b-44-11-3a-b7": {[]uint32{0x00, 0x1b, 0x44, 0x11, 0x3a, 0xb7}, true},
		"AB:CD":             {[]uint32{0xab, 0xcd}, true},
		// Not hex octets: ordinary strings that merely contain a separator.
		"eth0":        {nil, false},
		"a:b":         {nil, false},
		"de:adbeef":   {nil, false},
		"switch-01":   {nil, false},
		"192.168.1.1": {nil, false},
		"":            {nil, false},
		"zz:zz":       {nil, false},
	}
	for in, want := range cases {
		got, ok := parseHexOctets(in)
		if ok != want.ok {
			t.Errorf("parseHexOctets(%q) ok = %v, want %v (got %v)", in, ok, want.ok, got)
			continue
		}
		if !ok {
			continue
		}
		if len(got) != len(want.want) {
			t.Errorf("%q -> %v, want %v", in, got, want.want)
			continue
		}
		for i := range got {
			if got[i] != want.want[i] {
				t.Errorf("%q -> %v, want %v", in, got, want.want)
				break
			}
		}
	}
}

// The display and the encoder must agree on a fixed-size octet index, end to
// end through a real MIB.
func TestMacIndexRoundTripsThroughATable(t *testing.T) {
	s := diagDir(t, map[string]string{
		"SNMPv2-SMI": smiStub,
		"MAC-MIB": `MAC-MIB DEFINITIONS ::= BEGIN
IMPORTS OBJECT-TYPE, Integer32, enterprises FROM SNMPv2-SMI;

macTable OBJECT-TYPE
    SYNTAX      SEQUENCE OF MacEntry
    MAX-ACCESS  not-accessible
    STATUS      current
    DESCRIPTION "keyed by a six-octet address"
    ::= { enterprises 79 }

macEntry OBJECT-TYPE
    SYNTAX      MacEntry
    MAX-ACCESS  not-accessible
    STATUS      current
    DESCRIPTION "one row"
    INDEX       { macAddress }
    ::= { macTable 1 }

MacEntry ::= SEQUENCE { macAddress OCTET STRING, macPort Integer32 }

macAddress OBJECT-TYPE
    SYNTAX      OCTET STRING (SIZE (6))
    MAX-ACCESS  read-only
    STATUS      current
    DESCRIPTION "the address"
    ::= { macEntry 1 }

macPort OBJECT-TYPE
    SYNTAX      Integer32
    MAX-ACCESS  read-write
    STATUS      current
    DESCRIPTION "a port"
    ::= { macEntry 2 }
END
`})
	if _, err := s.LoadSpecific([]string{"SNMPv2-SMI", "MAC-MIB"}); err != nil {
		t.Skipf("fixture did not load: %v", err)
	}
	info, err := s.Table("1.3.6.1.4.1.79")
	if err != nil {
		t.Skipf("macTable did not resolve: %v", err)
	}

	const instance = "0.27.68.17.58.183"
	shown := s.DecodeIndexes(info.Oid, []string{instance})[0]
	if shown.Error != "" {
		t.Fatalf("decode: %s", shown.Error)
	}
	display := shown.Parts[0].Display
	if display != "00:1b:44:11:3a:b7" {
		t.Fatalf("displayed as %q", display)
	}

	back, err := s.EncodeIndex(info.Oid, []string{display})
	if err != nil {
		t.Fatalf("what the screen shows was refused by the encoder: %v", err)
	}
	if back != instance {
		t.Errorf("round trip gave %q, want %q", back, instance)
	}
}
