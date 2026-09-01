package notify

import (
	"testing"
	"time"

	"SnmpLens/pkg/events"
)

func ev(category, kind, severity, source, oid, summary string) events.Event {
	return events.Event{
		Category: category,
		Kind:     kind,
		Severity: severity,
		Source:   source,
		OID:      oid,
		Summary:  summary,
		State:    events.StateOneshot,
	}
}

var noon = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

func TestEmptyMatchMatchesEverything(t *testing.T) {
	var m RouteMatch
	if !m.Matches(ev("trap", "trap.received", "info", "10.0.0.1", "", "hello"), noon) {
		t.Fatal("an empty rule must match everything")
	}
}

// Severity is an ordered ladder, not a set: "at least major" must include
// critical. Comparing the names as text would put 'critical' before 'info'.
func TestMinSeverityIsOrdered(t *testing.T) {
	m := RouteMatch{MinSeverity: "major"}
	cases := map[string]bool{
		"critical": true,
		"major":    true,
		"minor":    false,
		"warning":  false,
		"info":     false,
	}
	for sev, want := range cases {
		if got := m.Matches(ev("system", "system.poll_failed", sev, "", "", ""), noon); got != want {
			t.Errorf("severity %q: matched=%v, want %v", sev, got, want)
		}
	}
}

func TestSourceAcceptsCIDRAndGlob(t *testing.T) {
	cidr := RouteMatch{Sources: []string{"10.0.0.0/8"}}
	if !cidr.Matches(ev("trap", "trap.received", "info", "10.4.5.6", "", ""), noon) {
		t.Error("10.4.5.6 should be inside 10.0.0.0/8")
	}
	if cidr.Matches(ev("trap", "trap.received", "info", "192.168.1.1", "", ""), noon) {
		t.Error("192.168.1.1 should be outside 10.0.0.0/8")
	}

	glob := RouteMatch{Sources: []string{"sw-*"}}
	if !glob.Matches(ev("trap", "trap.received", "info", "sw-paris-01", "", ""), noon) {
		t.Error("glob should match sw-paris-01")
	}
	if glob.Matches(ev("trap", "trap.received", "info", "rtr-paris-01", "", ""), noon) {
		t.Error("glob should not match rtr-paris-01")
	}

	// A hostname must not blow up the CIDR path.
	if !glob.Matches(ev("trap", "trap.received", "info", "sw-x", "", ""), noon) {
		t.Error("a non-IP source must still be matchable by glob")
	}
}

func TestOIDPrefixAndContains(t *testing.T) {
	m := RouteMatch{OIDPrefix: "1.3.6.1.2.1.2", Contains: "saturat"}
	yes := ev("threshold", "threshold.opened", "major", "10.0.0.1", "1.3.6.1.2.1.2.2.1.10.1", "Link saturated on WAN")
	if !m.Matches(yes, noon) {
		t.Error("expected a match on prefix + substring")
	}
	no := ev("threshold", "threshold.opened", "major", "10.0.0.1", "1.3.6.1.2.1.1.3.0", "Link saturated")
	if m.Matches(no, noon) {
		t.Error("a different OID subtree must not match")
	}
}

// Quiet hours suppress: inside the window the route deliberately does not match.
func TestQuietHoursSuppressAndWrapMidnight(t *testing.T) {
	m := RouteMatch{QuietHours: &Window{From: "22:00", To: "07:00"}}
	e := ev("trap", "trap.received", "major", "10.0.0.1", "", "")

	at := func(h, min int) time.Time { return time.Date(2026, 9, 1, h, min, 0, 0, time.UTC) }

	if m.Matches(e, at(23, 30)) {
		t.Error("23:30 is inside 22:00-07:00 and must be suppressed")
	}
	if m.Matches(e, at(3, 0)) {
		t.Error("03:00 is inside a window wrapping midnight and must be suppressed")
	}
	if !m.Matches(e, at(12, 0)) {
		t.Error("12:00 is outside the quiet window and must deliver")
	}
	if !m.Matches(e, at(7, 0)) {
		t.Error("the window end is exclusive: 07:00 must deliver")
	}
}

func TestSelectDeduplicatesSinksAcrossRoutes(t *testing.T) {
	routes := []Route{
		{ID: "a", Enabled: true, Priority: 1, SinkIDs: []string{"syslog", "mail"}},
		{ID: "b", Enabled: true, Priority: 2, SinkIDs: []string{"mail", "webhook"}},
	}
	got := Select(routes, ev("trap", "trap.received", "info", "10.0.0.1", "", ""), noon)
	want := []string{"syslog", "mail", "webhook"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v (order matters: priority then declaration)", got, want)
		}
	}
}

func TestSelectHonoursStopAndPriority(t *testing.T) {
	routes := []Route{
		{ID: "catchall", Enabled: true, Priority: 99, SinkIDs: []string{"mail"}},
		{ID: "specific", Enabled: true, Priority: 1, Stop: true, SinkIDs: []string{"syslog"},
			Match: RouteMatch{Categories: []string{"trap"}}},
	}
	got := Select(routes, ev("trap", "trap.received", "info", "10.0.0.1", "", ""), noon)
	if len(got) != 1 || got[0] != "syslog" {
		t.Fatalf("stop did not end evaluation: %v", got)
	}

	// A non-trap falls through to the catch-all.
	got = Select(routes, ev("system", "system.poll_failed", "warning", "", "", ""), noon)
	if len(got) != 1 || got[0] != "mail" {
		t.Fatalf("catch-all not reached: %v", got)
	}
}

func TestSelectSkipsDisabledRoutes(t *testing.T) {
	routes := []Route{{ID: "a", Enabled: false, SinkIDs: []string{"syslog"}}}
	if got := Select(routes, ev("trap", "trap.received", "info", "", "", ""), noon); len(got) != 0 {
		t.Fatalf("a disabled route delivered: %v", got)
	}
}
