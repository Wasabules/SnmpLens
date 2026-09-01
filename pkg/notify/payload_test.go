package notify

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"SnmpLens/pkg/events"
)

// A webhook can send our fixed envelope, or the operator's own JSON. The second
// is how you talk to Slack, Teams or Alertmanager, which all want their own
// shape rather than ours.

func hostileTrap() events.Event {
	return events.Event{
		ID: "evt-1", Category: events.CategoryTrap, Kind: events.KindTrapReceived,
		Severity: "major", Source: "10.0.0.1",
		// A trap arrives from the network. Its OID is a varbind value, so this
		// is text an attacker chooses.
		OID:     `he said "hello" and\then a newline` + "\n" + `and a } brace`,
		Summary: `quote " backslash \ newline` + "\n" + "tab\there",
	}
}

func TestEnvelopeModeIsUnchanged(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = string(b)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	sink := WebhookSink{Config: WebhookConfig{URL: srv.URL}}
	if err := sink.Send(hookEvent(), "subject", "body"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	var parsed webhookBody
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("the default payload is not the documented envelope: %v", err)
	}
	if parsed.Sender != "SnmpLens" || parsed.Body != "body" {
		t.Errorf("envelope = %+v", parsed)
	}
}

// The whole point: the operator writes the payload.
func TestTemplateModeSendsTheOperatorsJSON(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = string(b)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	sink := WebhookSink{Config: WebhookConfig{URL: srv.URL, PayloadMode: PayloadTemplate}}
	body := `{"text":"ifInOctets above 900","severity":"major"}`
	if err := sink.Send(hookEvent(), "ignored", body); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got != body {
		t.Errorf("payload = %q, want the template verbatim", got)
	}
}

// The failure this mode invites: one quote from the network turning a
// hand-written payload into a different document, or into a parse error at the
// receiver. Values are escaped as JSON string fragments.
func TestTemplateValuesAreJSONEscaped(t *testing.T) {
	e := hostileTrap()
	_, body := RenderJSONTemplate(e, "sink", MessageTemplate{
		Body: `{"oid":"{{oid}}","summary":"{{summary}}"}`,
	})

	var parsed map[string]string
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("hostile content broke the payload: %v\n%s", err, body)
	}
	// And the value must survive intact, not be mangled into safety.
	if parsed["oid"] != e.OID {
		t.Errorf("the OID did not round-trip:\n got  %q\n want %q", parsed["oid"], e.OID)
	}
	if parsed["summary"] != e.Summary {
		t.Errorf("the summary did not round-trip: %q", parsed["summary"])
	}
}

// The template's own punctuation must NOT be escaped, or writing JSON would be
// impossible.
func TestTemplateLiteralsAreNotEscaped(t *testing.T) {
	_, body := RenderJSONTemplate(hookEvent(), "sink", MessageTemplate{
		Body: `{"a":"{{severity}}","b":["x","y"]}`,
	})
	if !json.Valid([]byte(body)) {
		t.Fatalf("the template's own JSON was mangled: %s", body)
	}
	if !strings.Contains(body, `"b":["x","y"]`) {
		t.Errorf("literal structure was altered: %s", body)
	}
}

// Plain rendering must stay plain: escaping the ordinary body would put
// backslashes in everyone's email.
func TestPlainRenderingIsNotEscaped(t *testing.T) {
	e := hostileTrap()
	_, body := RenderTemplate(e, "sink", MessageTemplate{Body: "{{summary}}"})
	if strings.Contains(body, `\n`) || strings.Contains(body, `\"`) {
		t.Errorf("the plain rendering was JSON-escaped: %q", body)
	}
}

// Posting something that is not JSON would be rejected by the receiver with a
// message nobody sees. Refuse it here, where the error reaches the outbox.
func TestTemplateModeRefusesInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()

	sink := WebhookSink{Config: WebhookConfig{URL: srv.URL, PayloadMode: PayloadTemplate}}
	err := sink.Send(hookEvent(), "", `{"unclosed": `)
	if err == nil {
		t.Fatal("invalid JSON was posted")
	}
	if !strings.Contains(err.Error(), "valid JSON") {
		t.Errorf("the error should say what is wrong: %v", err)
	}
}

func TestTemplateModeRefusesAnEmptyPayload(t *testing.T) {
	sink := WebhookSink{Config: WebhookConfig{URL: "https://example.com", PayloadMode: PayloadTemplate}}
	if err := sink.Send(hookEvent(), "", "   "); err == nil {
		t.Fatal("an empty payload was accepted")
	}
}

// A realistic Slack payload, end to end.
func TestASlackShapedPayload(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	e := SampleEvent(events.CategoryThreshold)
	_, body := RenderJSONTemplate(e, "NOC", MessageTemplate{
		Body: `{"text":"[{{severityUpper}}] {{summary}}","channel":"#noc"}`,
	})
	sink := WebhookSink{Config: WebhookConfig{URL: srv.URL, PayloadMode: PayloadTemplate}}
	if err := sink.Send(e, "", body); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got["channel"] != "#noc" {
		t.Errorf("channel = %v", got["channel"])
	}
	if text, _ := got["text"].(string); !strings.HasPrefix(text, "[MAJOR] ") {
		t.Errorf("text = %q", text)
	}
}
