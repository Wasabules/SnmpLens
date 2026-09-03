package mib

import (
	"os"
	"testing"

	"github.com/sleepinggenius2/gosmi"
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
	gosmi.Init()
	gosmi.AppendPath("../../mibs")

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
