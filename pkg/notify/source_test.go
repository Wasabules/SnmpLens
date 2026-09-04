package notify

import (
	"strings"
	"testing"
)

// A source pattern that cannot mean what the operator wrote must SAY so, and
// "everything" must mean everything.
//
// matchesSource tried netip.ParsePrefix, then path.Match, then literal equality,
// and every failure path ended at "no match" with no error anywhere. A rule
// meant to cover the whole estate then delivered nothing, and neither the save
// nor the rule list said a word.

// A prefix of length zero is how an operator writes "every device". netip's
// Contains refuses across families, so 0.0.0.0/0 lost every IPv6 trap and ::/0
// lost every IPv4 one — the two patterns that look most like "everything" were
// the two that quietly halved the estate.
func TestAZeroLengthPrefixMatchesEveryFamily(t *testing.T) {
	for _, pattern := range []string{"0.0.0.0/0", "::/0"} {
		for _, source := range []string{"10.0.0.7", "192.168.1.1", "2001:db8::1", "fe80::1"} {
			if !matchesSource([]string{pattern}, source) {
				t.Errorf("%q did not match %q, so that device's events are dropped",
					pattern, source)
			}
		}
	}
}

// A real prefix still only matches its own family and range.
func TestARealPrefixStillDiscriminates(t *testing.T) {
	cases := []struct {
		pattern, source string
		want            bool
	}{
		{"10.0.0.0/8", "10.4.5.6", true},
		{"10.0.0.0/8", "11.4.5.6", false},
		{"10.0.0.0/8", "2001:db8::1", false},
		{"2001:db8::/32", "2001:db8::1", true},
		{"2001:db8::/32", "2001:db9::1", false},
		{"192.168.1.0/24", "192.168.1.255", true},
		{"192.168.1.0/24", "192.168.2.1", false},
	}
	for _, c := range cases {
		if got := matchesSource([]string{c.pattern}, c.source); got != c.want {
			t.Errorf("%q vs %q = %v, want %v", c.pattern, c.source, got, c.want)
		}
	}
}

// A source carrying a port must still be matched against a CIDR. Traps arrive
// as a bare address, but a monitoring session's target is whatever the operator
// typed, and "10.0.0.5:161" is a normal thing to type.
func TestASourceWithAPortStillMatchesACIDR(t *testing.T) {
	cases := []struct {
		pattern, source string
		want            bool
	}{
		{"10.0.0.0/8", "10.0.0.5:161", true},
		{"10.0.0.0/8", "11.0.0.5:161", false},
		{"2001:db8::/32", "[2001:db8::1]:161", true},
		{"2001:db8::/32", "[2001:db9::1]:161", false},
		{"0.0.0.0/0", "10.0.0.5:161", true},
	}
	for _, c := range cases {
		if got := matchesSource([]string{c.pattern}, c.source); got != c.want {
			t.Errorf("%q vs %q = %v, want %v", c.pattern, c.source, got, c.want)
		}
	}
}

// Globs and literals are unaffected.
func TestGlobAndLiteralSourcePatterns(t *testing.T) {
	cases := []struct {
		pattern, source string
		want            bool
	}{
		{"sw-*", "sw-core-1", true},
		{"sw-*", "rtr-core-1", false},
		{"10.0.0.5", "10.0.0.5", true},
		{"10.0.0.5", "10.0.0.6", false},
		{"core.example.com", "core.example.com", true},
	}
	for _, c := range cases {
		if got := matchesSource([]string{c.pattern}, c.source); got != c.want {
			t.Errorf("%q vs %q = %v, want %v", c.pattern, c.source, got, c.want)
		}
	}
}

// A pattern that cannot match anything is a mistake, and the operator must be
// told at save time rather than by an absence of alerts.
//
// Measured: "10.0.0/8" (a plausible typo) vs "10.4.5.6" is false; "10.0.0.0/33"
// is false; "sw-[a" is path.ErrBadPattern and is false. All three look like
// working rules in the list.
func TestAMalformedSourcePatternIsRejected(t *testing.T) {
	bad := []string{
		"10.0.0/8",     // three octets
		"10.0.0.0/33",  // no such prefix length
		"2001:db8::/“", // not a number at all
		"sw-[a",        // path.ErrBadPattern
		"10.0.0.0/-1",
	}
	for _, p := range bad {
		errs := ValidateSourcePatterns([]string{p})
		if len(errs) == 0 {
			t.Errorf("%q was accepted; it matches nothing and nothing says so", p)
			continue
		}
		if !strings.Contains(errs[0], p) {
			t.Errorf("the message for %q does not name it: %s", p, errs[0])
		}
	}
}

func TestAGoodSourcePatternIsAccepted(t *testing.T) {
	good := []string{
		"10.0.0.0/8", "0.0.0.0/0", "::/0", "2001:db8::/32",
		"sw-*", "sw-?", "10.0.0.5", "core.example.com", "*",
	}
	if errs := ValidateSourcePatterns(good); len(errs) != 0 {
		t.Errorf("a correct pattern was refused: %v", errs)
	}
	// An empty list is "everything", not an error.
	if errs := ValidateSourcePatterns(nil); len(errs) != 0 {
		t.Errorf("an empty pattern list was refused: %v", errs)
	}
}

// A system event is routable like any other, and it is the one with the most
// fields EMPTY: a dead letter carries no source, no session and no value. The
// default branch of SampleEvent returned a THRESHOLD event, so a template was
// never checked against the shape system events actually have — and the preview
// showed a threshold alert to an operator who asked for a system one.
func TestASystemSampleIsASystemEvent(t *testing.T) {
	e := SampleEvent("system")
	if e.Category != "system" {
		t.Fatalf("SampleEvent(\"system\").Category = %q", e.Category)
	}
	// The fields that a real dead letter leaves empty must be empty here, or
	// validation is checking a shape that never occurs.
	for name, v := range map[string]string{
		"source": e.Source, "sessionName": e.SessionName,
		"sessionId": e.SessionID, "corrId": e.CorrID, "dedupKey": e.DedupKey,
	} {
		if v != "" {
			t.Errorf("the system sample carries a %s (%q); a real dead letter has none",
				name, v)
		}
	}
	if e.Value != nil {
		t.Error("the system sample carries a value; a real dead letter has none")
	}
}

// A payload template that only produces JSON for a threshold event must be
// refused, not discovered at 03:00 by a receiver rejecting the body.
func TestAPayloadTemplateIsCheckedAgainstASystemEvent(t *testing.T) {
	// `{{source}}` is empty for a dead letter, so an unquoted use of it stops
	// being JSON exactly then.
	cfg := SinkConfig{
		Kind:     SinkWebhook,
		Name:     "NOC",
		Webhook:  WebhookConfig{URL: "https://hooks.example.com/x", PayloadMode: PayloadTemplate},
		Template: MessageTemplate{Body: `{"device": {{source}}, "text": "{{summary}}"}`},
	}
	if err := ValidatePayloadTemplate(cfg); err == nil {
		t.Error("a template that stops being JSON for a system event was accepted")
	}

	// And the same template quoted properly is fine for every kind.
	cfg.Template = MessageTemplate{Body: `{"device": "{{source|unknown}}", "text": "{{summary}}"}`}
	if err := ValidatePayloadTemplate(cfg); err != nil {
		t.Errorf("a correct template was refused: %v", err)
	}
}
