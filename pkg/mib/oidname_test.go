package mib

import "testing"

// An instance is named by its module and object, the instance after them; an
// OID no loaded MIB describes, or that only reaches a row, is not named.
func TestAnOIDIsNamedFromTheLoadedMIBs(t *testing.T) {
	s := loadedService(t)
	got := s.NameOIDs([]string{".1.3.6.1.2.1.2.2.1.2.3", "1.3.6.1.2.1.1.1.0", ".1.3.6.1.4.1.32473.1.1.0", ".1.3.6.1.2.1.2.2.1"})
	for oid, want := range map[string]string{
		".1.3.6.1.2.1.2.2.1.2.3": "IF-MIB::ifDescr.3",
		"1.3.6.1.2.1.1.1.0":      "SNMPv2-MIB::sysDescr.0",
	} {
		if n := got[oid].String(); n != want {
			t.Errorf("%s is named %q, want %q", oid, n, want)
		}
	}
	for _, not := range []string{".1.3.6.1.4.1.32473.1.1.0", ".1.3.6.1.2.1.2.2.1"} {
		if n, ok := got[not]; ok {
			t.Errorf("%s was named %s", not, n)
		}
	}
}
