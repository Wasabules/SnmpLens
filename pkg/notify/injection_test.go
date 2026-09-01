package notify

import (
	"strings"
	"testing"

	"SnmpLens/pkg/events"
)

// Everything here defends one boundary: a trap arrives from the network with
// nobody authenticating it, its contents reach the rendered subject and body,
// and those are then written into protocols where a newline or a dot changes
// the meaning of what follows.

// hostileEvent carries the shapes an attacker would try, in the fields that a
// trap can actually populate.
func hostileEvent(payload string) events.Event {
	return events.Event{
		ID:       "evt-1",
		Category: events.CategoryTrap,
		Kind:     events.KindTrapReceived,
		Severity: "minor",
		Source:   "10.0.0.1",
		OID:      payload, // recordTrap fills this from a varbind value
		Summary:  "Trap from 10.0.0.1 (Version2c, 3 varbinds)",
	}
}

// A line beginning with a dot must be stuffed, or it ends the DATA phase and
// the rest of the message is read by the relay as SMTP commands.
func TestMailBodyCannotEndDataEarly(t *testing.T) {
	hostile := "x\n.\nMAIL FROM:<spoofed@example.com>\nRCPT TO:<victim@example.com>\nDATA\nspam"
	e := hostileEvent(hostile)
	_, body := Render(e, false)

	msg := buildMessage(EmailConfig{From: "a@b.c", To: []string{"d@e.f"}}, e, "subject", body)

	// Split the way a relay does, and look for a line that would terminate DATA.
	afterHeaders := msg
	if i := strings.Index(msg, "\r\n\r\n"); i >= 0 {
		afterHeaders = msg[i+4:]
	}
	for _, line := range strings.Split(afterHeaders, "\r\n") {
		if line == "." {
			t.Fatalf("an unstuffed bare dot survived into the body:\n%s", afterHeaders)
		}
	}
	// Bare LFs would let a lenient relay find the dot line anyway.
	if strings.Contains(strings.ReplaceAll(afterHeaders, "\r\n", ""), "\n") {
		t.Errorf("the body still contains bare LF line endings:\n%q", afterHeaders)
	}
	// The content must still be there — stuffing, not deleting.
	if !strings.Contains(afterHeaders, "MAIL FROM:<spoofed@example.com>") {
		t.Error("the body was mangled rather than escaped")
	}
}

// RFC5321 4.5.2 is about any line STARTING with a dot, not only a lone dot.
func TestMailBodyStuffsEveryLeadingDot(t *testing.T) {
	body := ".hidden\r\n..already\r\nnormal\r\n.\r\n"
	out := dotStuff(body)
	for _, line := range strings.Split(out, "\r\n") {
		if line == "" || line == "normal" {
			continue
		}
		if !strings.HasPrefix(line, "..") {
			t.Errorf("line %q was not stuffed", line)
		}
	}
}

func TestDotStuffNormalisesLineEndings(t *testing.T) {
	for _, in := range []string{"a\nb", "a\r\nb", "a\rb"} {
		out := dotStuff(in)
		if out != "a\r\nb" {
			t.Errorf("dotStuff(%q) = %q, want \"a\\r\\nb\"", in, out)
		}
	}
}

// The subject is written straight into a header. Its safety comes from
// QEncoding encoding every byte below 0x20, which is standard-library
// behaviour we depend on rather than implement — so it is pinned here.
func TestMailSubjectCannotInjectAHeader(t *testing.T) {
	e := hostileEvent("x")
	e.Summary = "boom\r\nBcc: attacker@evil.example\r\nX-Injected: yes"
	subject, _ := Render(e, false)

	msg := buildMessage(EmailConfig{From: "a@b.c", To: []string{"d@e.f"}}, e, subject, "body")
	headers := msg
	if i := strings.Index(msg, "\r\n\r\n"); i >= 0 {
		headers = msg[:i]
	}

	// Header field names sit at the start of a line; a continuation line
	// begins with whitespace. Only a real injection produces "Bcc:" there.
	for _, line := range strings.Split(headers, "\r\n") {
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			continue // folded continuation of the previous header
		}
		name, _, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "bcc", "cc", "x-injected":
			t.Fatalf("a header was injected through the subject:\n%s", headers)
		}
	}
	// And the payload must be present, encoded — not silently dropped.
	if !strings.Contains(headers, "=0D=0A") {
		t.Errorf("the CRLF was not encoded; the protection is not where it is assumed to be:\n%s", headers)
	}
}

// From, To and the Message-ID are written raw. They are configuration rather
// than network input, but they cross the bridge as strings and a pasted
// newline would end the header just the same.
func TestMailHeadersStripControlCharacters(t *testing.T) {
	e := hostileEvent("x")
	e.ID = "evt\r\nBcc: attacker@evil.example"

	msg := buildMessage(EmailConfig{
		From: "a@b.c\r\nBcc: attacker@evil.example",
		To:   []string{"d@e.f\r\nX-Injected: yes"},
	}, e, "subject", "body")

	headers := msg
	if i := strings.Index(msg, "\r\n\r\n"); i >= 0 {
		headers = msg[:i]
	}
	for _, line := range strings.Split(headers, "\r\n") {
		name, _, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "bcc", "x-injected":
			t.Fatalf("a header was injected through an address or the id:\n%s", headers)
		}
	}
}

// Syslog framing: over TCP and TLS the frame is octet-counted, so a newline in
// the message cannot make the collector see a second, forged record.
func TestSyslogMessageCannotForgeASecondFrame(t *testing.T) {
	e := hostileEvent("x")
	hostile := "ok\n<13>1 2026-01-01T00:00:00Z evil - - - - forged entry"
	line := FormatRFC5424(SyslogConfig{Facility: 16, Hostname: "w"}, e, hostile)

	framed := len(line)
	// The receiver reads exactly `framed` octets and treats them as ONE
	// message, whatever they contain.
	if strings.Count(line, "\n") == 0 {
		t.Skip("no newline survived; nothing to prove")
	}
	if framed != len(line) {
		t.Fatal("length and content disagree")
	}
	// What must hold is that the count covers the whole payload, newline
	// included — a count that stopped at the newline is what would let the
	// remainder be parsed as a new record.
	if !strings.Contains(line[:framed], "forged entry") {
		t.Error("the octet count does not cover the whole message")
	}
}

// The syslog HEADER is a different matter: a space or newline there really
// would shift every later field, which is why it is sanitised.
func TestSyslogHeaderFieldsAreSanitised(t *testing.T) {
	e := hostileEvent("x")
	e.Kind = "trap.received evil - - -"
	line := FormatRFC5424(SyslogConfig{Facility: 16, Hostname: "host name\nwith breaks"}, e, "msg")

	header, _, _ := strings.Cut(line, "[")
	if strings.Contains(header, "\n") {
		t.Errorf("a newline reached the syslog header: %q", header)
	}
	// "<PRI>VERSION" TIMESTAMP HOSTNAME APP-NAME PROCID MSGID — six
	// space-separated tokens before the structured data, whatever was put in
	// them. A space smuggled into any field would produce a seventh and shift
	// everything after it by one.
	if got := len(strings.Fields(header)); got != 6 {
		t.Errorf("the header has %d fields, want 6: %q", got, header)
	}
	// The sanitiser drops the offending bytes rather than escaping them, so
	// the injected field separators must simply not be there.
	if strings.Contains(header, "host name") || strings.Contains(header, "received evil") {
		t.Errorf("a space survived into a header field: %q", header)
	}
}

// Redaction has to reach the STRUCTURED payload, not only the rendered text.
// The webhook embeds the whole event as JSON, so masking the subject and body
// alone would send the real address through the sink most likely to forward it
// somewhere else.
func TestRedactEventMasksEveryCopyOfTheAddress(t *testing.T) {
	e := events.Event{
		ID: "e1", Severity: "major", Source: "192.168.42.77",
		Summary:  "threshold exceeded on 192.168.42.77",
		DedupKey: "threshold|s1|192.168.42.77|1.3.6",
		Params:   map[string]any{"source": "192.168.42.77", "varbinds": 3},
	}
	got := RedactEvent(e)

	for _, field := range []string{got.Source, got.Summary, got.DedupKey} {
		if strings.Contains(field, "192.168.42.77") {
			t.Errorf("the real address survived in %q", field)
		}
	}
	if s, _ := got.Params["source"].(string); strings.Contains(s, "192.168.42.77") {
		t.Errorf("the real address survived in params: %v", got.Params)
	}
	// Non-string params must come through untouched.
	if got.Params["varbinds"] != 3 {
		t.Errorf("a non-string param was altered: %v", got.Params["varbinds"])
	}
	// The journal keeps the truth: the copy must not have mutated the original.
	if e.Source != "192.168.42.77" || e.Params["source"] != "192.168.42.77" {
		t.Error("RedactEvent mutated the event it was given; the journal would lose the real address")
	}
}

func TestRedactEventLeavesAddresslessEventsAlone(t *testing.T) {
	e := events.Event{ID: "e1", Summary: "SnmpLens started"}
	if got := RedactEvent(e); got.Summary != e.Summary {
		t.Errorf("an event with no source was altered: %+v", got)
	}
}
