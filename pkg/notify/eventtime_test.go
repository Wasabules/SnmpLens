package notify

import (
	"testing"
	"time"

	"SnmpLens/pkg/events"
)

// Routing is evaluated against the time the INCIDENT happened, not the time the
// router got round to it. That is what makes routing a pure function of the
// event, which is what makes replaying it after a crash deterministic.
//
// Two things have to be right for that to be an improvement rather than a bug.

// Every producer writes Ts in UTC — pkg/snmp/trap.go, pkg/monitor/breach.go and
// storage.InsertEvent all use time.Now().UTC(). Quiet hours are wall-clock hours
// the operator typed into a form, which are LOCAL. Comparing one against the
// other rotates every window by the machine's UTC offset: alerts fire inside the
// quiet window and are dropped during working hours.
func TestQuietHoursAreEvaluatedInTheOperatorsZone(t *testing.T) {
	// A zone where the difference is unmistakable, whatever the machine is set to.
	zone := time.FixedZone("UTC+9", 9*3600)

	// 23:30 local on the 1st is 14:30 UTC the same day.
	local := time.Date(2026, 3, 1, 23, 30, 0, 0, zone)
	e := events.Event{
		Ts:       local.UTC().Format(time.RFC3339),
		Severity: events.SevMajor.String(),
	}

	night := RouteMatch{QuietHours: &Window{From: "22:00", To: "07:00"}}
	if night.MatchesAt(e, zone) {
		t.Error("23:30 local is inside a 22:00-07:00 quiet window and must not match; " +
			"the UTC form of the same instant is 14:30, which is not")
	}

	// And the middle of the working day must still get through.
	noon := time.Date(2026, 3, 1, 12, 0, 0, 0, zone)
	e.Ts = noon.UTC().Format(time.RFC3339)
	if !night.MatchesAt(e, zone) {
		t.Error("12:00 local is outside the quiet window and must match; the UTC " +
			"form is 03:00, which is inside it")
	}
}

// A Ts that does not parse must not be able to MUTE an alert.
//
// The obvious fallback — the zero time — is 00:00, which is inside every window
// that wraps midnight, and those are exactly the windows people configure. An
// unparseable timestamp would silence the alert permanently and invisibly.
func TestAnUnparseableTimestampDoesNotSuppress(t *testing.T) {
	for _, ts := range []string{"", "not a timestamp", "2026-13-45T99:99:99Z"} {
		e := events.Event{Ts: ts, Severity: events.SevMajor.String()}
		at := RoutingTime(e, time.Local)

		if at.IsZero() {
			t.Errorf("Ts %q gave the zero time, whose hour is 00:00 — inside every "+
				"midnight-wrapping quiet window", ts)
		}
		// It falls back to now, so whether it matches depends on the clock. What
		// must never happen is a silent, permanent suppression.
		if time.Since(at) > time.Minute || time.Until(at) > time.Minute {
			t.Errorf("Ts %q did not fall back to the current time: %v", ts, at)
		}
	}
}

// A timestamp that parses is used as given.
func TestAParseableTimestampIsUsed(t *testing.T) {
	want := time.Date(2026, 3, 1, 14, 30, 0, 0, time.UTC)
	e := events.Event{Ts: want.Format(time.RFC3339)}

	got := RoutingTime(e, time.UTC)
	if !got.Equal(want) {
		t.Errorf("RoutingTime = %v, want %v", got, want)
	}
}

// Everything else in a rule is time-independent, so the change can only affect
// quiet hours. Pinned so a future field that reads the clock is noticed.
func TestOnlyQuietHoursDependOnTheTime(t *testing.T) {
	e := events.Event{
		Ts: "2026-03-01T03:00:00Z", Category: events.CategoryTrap,
		Kind: events.KindTrapReceived, Severity: events.SevMajor.String(),
		Source: "10.0.0.1", OID: "1.3.6.1.6.3.1.1.5.3", Summary: "link down",
	}
	m := RouteMatch{
		Categories:  []string{events.CategoryTrap},
		MinSeverity: events.SevMinor.String(),
		Sources:     []string{"10.0.0.0/8"},
		OIDPrefix:   "1.3.6.1.6.3",
		Contains:    "link",
	}

	morning := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	night := time.Date(2026, 3, 1, 3, 0, 0, 0, time.UTC)

	if m.Matches(e, morning) != m.Matches(e, night) {
		t.Error("a rule with no quiet hours gave a different answer at a different " +
			"time of day")
	}
}
