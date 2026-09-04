package notify

import (
	"testing"
	"time"

	"SnmpLens/pkg/events"
)

// An OID prefix rule is matched against a DOTTED-DECIMAL identifier, not against
// text. `strings.HasPrefix` reads it as text, and the two disagree exactly where
// it matters: 1.3.6.1.2.1.2 is `interfaces`, and 1.3.6.1.2.1.25 is `host`, which
// is a different subtree of the MIB that happens to start with the same
// characters. A route meant for interface traps also fired for every host,
// printer and disk-storage event on the network.
func TestOIDPrefixIsMatchedBySubIdentifier(t *testing.T) {
	cases := []struct {
		prefix, oid string
		want        bool
		why         string
	}{
		{"1.3.6.1.2.1.2", "1.3.6.1.2.1.2", true, "the prefix itself"},
		{"1.3.6.1.2.1.2", "1.3.6.1.2.1.2.2.1.8.1", true, "inside the subtree"},
		{"1.3.6.1.2.1.2", "1.3.6.1.2.1.25.1.1.0", false, "host, not interfaces"},
		{"1.3.6.1.2.1.2", "1.3.6.1.2.1.23.1.1", false, "rip2, not interfaces"},
		{"1.3.6.1.2.1.2", "1.3.6.1.2.1.28.1", false, "not interfaces"},
		{"1.3.6.1.2.1.1", "1.3.6.1.2.1.1", true, "system itself"},
		{"1.3.6.1.2.1.1", "1.3.6.1.2.1.16.1", false, "rmon, not system"},

		// gosnmp hands every varbind name back with a LEADING DOT, so every
		// trap journalled an OID the user's own prefix could not match. The
		// placeholder in the UI is `1.3.6.1.2.1.2`, without one.
		{"1.3.6.1.6.3.1.1.5", ".1.3.6.1.6.3.1.1.5.3", true, "leading dot on the event"},
		{".1.3.6.1.6.3.1.1.5", "1.3.6.1.6.3.1.1.5.3", true, "leading dot on the prefix"},
		{".1.3.6.1.6.3.1.1.5", ".1.3.6.1.6.3.1.1.5.4", true, "a leading dot on both"},
		{"1.3.6.1.6.3.1.1.5", ".1.3.6.1.6.3.1.1.55", false, "still not a sub-identifier"},

		// A rule cannot match an event that carries no OID at all.
		{"1.3.6.1", "", false, "no OID on the event"},
	}

	for _, c := range cases {
		m := RouteMatch{OIDPrefix: c.prefix}
		e := events.Event{OID: c.oid, Severity: events.SevMinor.String()}
		if got := m.Matches(e, time.Now()); got != c.want {
			t.Errorf("prefix %q vs OID %q = %v, want %v (%s)", c.prefix, c.oid, got, c.want, c.why)
		}
	}
}

// An empty prefix is not a filter.
func TestEmptyOIDPrefixMatchesEverything(t *testing.T) {
	m := RouteMatch{}
	for _, oid := range []string{"", "1.3.6.1.2.1.1.3.0", ".1.2.3"} {
		if !m.Matches(events.Event{OID: oid, Severity: events.SevMinor.String()}, time.Now()) {
			t.Errorf("an empty prefix rejected OID %q", oid)
		}
	}
}
