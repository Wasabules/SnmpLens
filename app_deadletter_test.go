package main

import (
	"testing"

	"SnmpLens/pkg/events"
	"SnmpLens/pkg/notify"
)

// A dead letter must never be routed back to the sink that produced it.
//
// The failure becomes an event, the event is routed, and a catch-all rule —
// the natural thing to write — selects the sink that just failed. That
// delivery fails, produces another dead letter, and so on: one unreachable
// collector grows the event journal and the outbox without bound, six attempts
// at a time against a machine that is not answering.
func TestDeadLetterIsNotRoutedBackToItsOwnSink(t *testing.T) {
	a := newTestApp(t)

	for _, s := range []notify.SinkConfig{
		{ID: "mail", Name: "mail", Kind: notify.SinkEmail, Enabled: true,
			Email: notify.EmailConfig{Host: "127.0.0.1", Port: 1, From: "a@b", To: []string{"c@d"}}},
		{ID: "log", Name: "log", Kind: notify.SinkSyslog, Enabled: true,
			Syslog: notify.SyslogConfig{Address: "127.0.0.1:1", Protocol: "udp"}},
	} {
		if _, err := a.storage.SaveSink(s); err != nil {
			t.Fatal(err)
		}
	}
	// The catch-all somebody would actually write.
	if _, err := a.storage.SaveRoute(notify.Route{
		ID: "all", Name: "everything", Enabled: true, Priority: 1,
		SinkIDs: []string{"mail", "log"},
	}); err != nil {
		t.Fatal(err)
	}

	before, err := a.storage.ListDeliveries("", 200)
	if err != nil {
		t.Fatal(err)
	}

	a.routeEvent(events.Event{
		ID:       "evt-dead-1",
		Category: events.CategorySystem,
		Kind:     events.KindSystemSinkDeadLetter,
		Severity: events.SevMajor.String(),
		Summary:  "Delivery to mail given up: connection refused",
		Params:   map[string]any{"sink": "mail"},
	})

	after, err := a.storage.ListDeliveries("", 200)
	if err != nil {
		t.Fatal(err)
	}

	var toMail, toLog int
	for _, d := range after[len(before):] {
		switch d.SinkID {
		case "mail":
			toMail++
		case "log":
			toLog++
		}
	}

	if toMail != 0 {
		t.Errorf("the dead letter for 'mail' queued %d deliveries back to 'mail' — that is the loop", toMail)
	}
	// And it must still reach the OTHER sinks: "the mail relay is down" is
	// exactly what you want to hear over syslog.
	if toLog != 1 {
		t.Errorf("%d deliveries to 'log'; the dead letter must still be reported elsewhere", toLog)
	}
}

// An ordinary event is unaffected: the exclusion applies to dead letters only.
func TestAnOrdinaryEventStillReachesEverySink(t *testing.T) {
	a := newTestApp(t)

	for _, id := range []string{"mail", "log"} {
		if _, err := a.storage.SaveSink(notify.SinkConfig{
			ID: id, Name: id, Kind: notify.SinkSyslog, Enabled: true,
			Syslog: notify.SyslogConfig{Address: "127.0.0.1:1", Protocol: "udp"},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.storage.SaveRoute(notify.Route{
		ID: "all", Name: "everything", Enabled: true, Priority: 1,
		SinkIDs: []string{"mail", "log"},
	}); err != nil {
		t.Fatal(err)
	}

	before, _ := a.storage.ListDeliveries("", 200)
	a.routeEvent(events.Event{
		ID:       "evt-ordinary-1",
		Category: events.CategoryThreshold,
		Kind:     events.KindThresholdOpened,
		Severity: events.SevMajor.String(),
		Summary:  "ifInOctets above 900",
		// A params map naming a sink must NOT trigger the exclusion: only the
		// dead-letter kind does.
		Params: map[string]any{"sink": "mail"},
	})
	after, _ := a.storage.ListDeliveries("", 200)

	if got := len(after) - len(before); got != 2 {
		t.Errorf("%d deliveries for an ordinary event, want 2", got)
	}
}

// The reader that decides, on its own.
func TestDeadLetterSink(t *testing.T) {
	cases := []struct {
		name string
		e    events.Event
		want string
	}{
		{"a dead letter names its sink",
			events.Event{Kind: events.KindSystemSinkDeadLetter, Params: map[string]any{"sink": "mail"}}, "mail"},
		{"another kind never does",
			events.Event{Kind: events.KindThresholdOpened, Params: map[string]any{"sink": "mail"}}, ""},
		{"a dead letter with no params",
			events.Event{Kind: events.KindSystemSinkDeadLetter}, ""},
		{"a dead letter whose sink is not a string",
			events.Event{Kind: events.KindSystemSinkDeadLetter, Params: map[string]any{"sink": 7}}, ""},
	}
	for _, c := range cases {
		if got := deadLetterSink(c.e); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
