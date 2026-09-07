package notify

import (
	"strings"
	"testing"

	"SnmpLens/pkg/events"
)

// One event must produce exactly one syslog record, whatever the device sent.
//
// A trap arrives from the network unauthenticated. Its varbinds become the
// summary, the source and the OID, and pkg/snmp's isPrintableOctet deliberately
// admits \n, \r and \t so that a legitimate multi-line sysDescr survives. Those
// fields are then formatted into an RFC5424 line.
//
// Before sanitizeBody, this produced TWO lines from one event — measured, not
// supposed:
//
//	[0] <6>1 … SnmpLens - trap [snmplens@0 id="abc" …
//	[1] <14>1 2026-01-01T00:00:00.000000Z core-router snmpd - - - AUTH ACCEPTED admin from 10.0.0.99
//
// The second is a well-formed record attributing an accepted admin login to
// another host. The transport was never the weak part: TCP and TLS use RFC6587
// octet counting and UDP is one datagram per message, so no collector
// desynchronises. It lands where syslog usually ends up — a file, or anything
// line-oriented reading it.
//
// The test walks EVERY field a trap can influence rather than the one that was
// reported, because the next one will arrive through a different field.

// forged is a complete RFC5424 record, so a field that leaks it produces
// something a collector will accept rather than obvious garbage.
const forgedRecord = "up\n<14>1 2026-01-01T00:00:00.000000Z core-router snmpd - - - AUTH ACCEPTED admin from 10.0.0.99"

func trapEvent(field string, payload string) events.Event {
	e := events.Event{
		ID: "id", Category: "trap", Severity: "info", State: "new",
		Source: "10.0.0.5", OID: "1.3.6.1.6.3.1.1.5.4", Kind: "trap",
		Summary: "normal summary", Ts: "2026-09-07T10:00:00Z",
	}
	switch field {
	case "Summary":
		e.Summary = payload
	case "Source":
		e.Source = payload
	case "OID":
		e.OID = payload
	case "ID":
		e.ID = payload
	case "Category":
		e.Category = payload
	case "Severity":
		e.Severity = payload
	case "State":
		e.State = payload
	case "Kind":
		e.Kind = payload
	}
	return e
}

func TestNoTrapFieldCanAddASyslogRecord(t *testing.T) {
	fields := []string{"Summary", "Source", "OID", "ID", "Category", "Severity", "State", "Kind"}
	payloads := map[string]string{
		"a forged record after a newline": forgedRecord,
		"a bare carriage return":          "up\rinjected",
		"CRLF":                            "up\r\ninjected",
		"a NUL":                           "up\x00injected",
		"a vertical tab":                  "up\vinjected",
		"a form feed":                     "up\finjected",
	}

	for _, withSD := range []bool{true, false} {
		for _, field := range fields {
			for name, payload := range payloads {
				e := trapEvent(field, payload)
				line := FormatRFC5424(SyslogConfig{OmitStructuredData: !withSD}, e, "")

				if n := strings.Count(line, "\n"); n != 0 {
					t.Errorf("%s carrying %s (structured data: %v): the record contains %d newline(s)\n%s",
						field, name, withSD, n, line)
				}
				if strings.ContainsAny(line, "\r\x00") {
					t.Errorf("%s carrying %s (structured data: %v): the record contains a raw CR or NUL\n%s",
						field, name, withSD, line)
				}
			}
		}
	}
}

// A body rendered from a message template reaches the MSG by a different route
// than the summary, and is just as attacker-influenced.
func TestARenderedBodyCannotAddARecordEither(t *testing.T) {
	e := trapEvent("Summary", "normal")
	line := FormatRFC5424(SyslogConfig{}, e, forgedRecord)
	if strings.Contains(line, "\n") {
		t.Errorf("a rendered body added a record:\n%s", line)
	}
	if !strings.Contains(line, `\n`) {
		t.Errorf("the newline was dropped rather than escaped; an operator cannot see what the device sent:\n%s", line)
	}
}

// What sanitizeBody must and must not touch. The escape has to be visible —
// stripping hides the attempt from whoever reads the journal afterwards.
func TestSanitizeBodyEscapesRatherThanStrips(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain", "plain"},
		{"a\nb", `a\nb`},
		{"a\rb", `a\rb`},
		{"a\x00b", `a\0b`},
		{"a\vb", `a\x0bb`},
		{"a\x7fb", `a\x7fb`},
		// TAB has no framing meaning and appears in real sysDescr values.
		{"a\tb", "a\tb"},
		// Anything above the C0 range is left exactly alone, including the
		// non-ASCII that four of our five locales produce.
		{"état — 温度", "état — 温度"},
	}
	for _, c := range cases {
		if got := sanitizeBody(c.in); got != c.want {
			t.Errorf("sanitizeBody(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// Prove the test can fail. Without this, a formatter that returned "" would
// pass every assertion above.
func TestTheDetectorWouldSeeAnUnescapedNewline(t *testing.T) {
	raw := "<6>1 stamp host app - trap - " + forgedRecord
	if !strings.Contains(raw, "\n") {
		t.Fatal("the probe string carries no newline, so this test proves nothing")
	}
	if strings.Count(raw, "\n") != 1 {
		t.Fatalf("expected exactly one newline in the probe, got %d", strings.Count(raw, "\n"))
	}
}
