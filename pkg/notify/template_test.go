package notify

import (
	"strings"
	"testing"

	"SnmpLens/pkg/events"
)

func tplEvent() events.Event {
	v := 912.5
	return events.Event{
		Seq: 1042, ID: "evt-1", Ts: "2026-09-01T09:12:44Z",
		Category: events.CategoryThreshold, Kind: events.KindThresholdOpened,
		Severity: "major", State: events.StateOpen,
		Source: "10.0.0.1", OID: "1.3.6.1.2.1.2.2.1.10.1",
		SessionName: "WAN Paris", SessionID: "sess-1",
		DedupKey: "threshold|sess-1|10.0.0.1|1.3.6.1.2.1.2.2.1.10.1",
		Summary:  "ifInOctets reached 912.5, above 900, on 10.0.0.1",
		Value:    &v,
		Params:   map[string]any{"source": "10.0.0.1", "bound": 900, "held": 120},
	}
}

func render(t *testing.T, subject, body string) (string, string) {
	t.Helper()
	return RenderTemplate(tplEvent(), "NOC mail", MessageTemplate{Subject: subject, Body: body})
}

// An unset template must produce exactly what the sink produced before
// templates existed, or every sink silently changes on upgrade.
func TestEmptyTemplateFallsBackToTheBuiltInRendering(t *testing.T) {
	e := tplEvent()
	wantSubject, wantBody := renderDefault(e)
	gotSubject, gotBody := RenderTemplate(e, "NOC mail", MessageTemplate{})

	if gotSubject != wantSubject || gotBody != wantBody {
		t.Errorf("an empty template changed the output:\nsubject %q vs %q\nbody %q vs %q",
			gotSubject, wantSubject, gotBody, wantBody)
	}
	// Only one field set: the other must still fall back.
	gotSubject, gotBody = RenderTemplate(e, "NOC mail", MessageTemplate{Subject: "x"})
	if gotSubject != "x" || gotBody != wantBody {
		t.Errorf("setting only the subject changed the body")
	}
}

// Render(e, true) is now RedactEvent + the default rendering. The output must
// be unchanged; only the masking is broader.
func TestRedactedDefaultRenderingCarriesNoRealAddress(t *testing.T) {
	_, body := Render(tplEvent(), true)
	if strings.Contains(body, "10.0.0.1") {
		t.Errorf("the real address survived the redacted rendering:\n%s", body)
	}
}

func TestSubstitutionAndFallback(t *testing.T) {
	subject, body := render(t,
		"[{{severityUpper}}] {{sessionName}} — {{source}}",
		"value={{value}} bound={{params.bound}} missing={{corrId|none}} empty={{corrId}}")

	if subject != "[MAJOR] WAN Paris — 10.0.0.1" {
		t.Errorf("subject = %q", subject)
	}
	for _, want := range []string{"value=912.5", "bound=900", "missing=none", "empty="} {
		if !strings.Contains(body, want) {
			t.Errorf("body is missing %q: %q", want, body)
		}
	}
}

// A block disappears when its variable is empty and renders when it is not.
func TestBlocks(t *testing.T) {
	_, body := render(t, "", "A{{#sessionName}} session={{sessionName}}{{/sessionName}}B{{#corrId}} corr={{corrId}}{{/corrId}}C")
	if body != "A session=WAN ParisBC" {
		t.Errorf("body = %q", body)
	}
}

// Presence is emptiness, not truth. A threshold bound of 0 is a real bound and
// a false flag is a real answer; hiding either would drop information from an
// alert precisely when the value is interesting.
func TestZeroAndFalseArePresent(t *testing.T) {
	e := tplEvent()
	e.Params = map[string]any{"bound": 0, "acked": false}
	_, body := RenderTemplate(e, "s", MessageTemplate{
		Body: "{{#params.bound}}bound={{params.bound}}{{/params.bound}}|{{#params.acked}}acked={{params.acked}}{{/params.acked}}",
	})
	if body != "bound=0|acked=false" {
		t.Errorf("body = %q; a zero or a false was treated as absent", body)
	}
}

// A block tag alone on its line takes the line with it, so a conditional
// section leaves neither a blank line when it renders nor an orphan one when
// it does not. The trigger is the shape of the template, never the data.
func TestBlockTagOnItsOwnLineEatsTheLine(t *testing.T) {
	tpl := "before\n{{#corrId}}\ncorr={{corrId}}\n{{/corrId}}\nafter"
	_, body := render(t, "", tpl)
	if body != "before\nafter" {
		t.Errorf("body = %q, want \"before\\nafter\"", body)
	}

	// With text beside it, the line is NOT eaten.
	tpl = "x {{#corrId}}y{{/corrId}} z"
	_, body = render(t, "", tpl)
	if body != "x  z" {
		t.Errorf("body = %q, want \"x  z\"", body)
	}
}

// The single most important rule: substituted text is never re-scanned. A trap
// arrives from the network unauthenticated, and its OID is a varbind value.
func TestSubstitutedTextIsNeverRescanned(t *testing.T) {
	e := tplEvent()
	e.OID = "{{secret}} {{#kind}}x{{/kind}} {{summary}}"
	_, body := RenderTemplate(e, "s", MessageTemplate{Body: "oid={{oid}}"})

	if body != "oid={{secret}} {{#kind}}x{{/kind}} {{summary}}" {
		t.Errorf("substituted text was interpreted: %q", body)
	}
	if strings.Contains(body, "ifInOctets") {
		t.Error("a nested {{summary}} was expanded from data")
	}
}

// A template must never become a way to print a credential into
// notify_sinks.config or notify_outbox, both readable in a copied database.
func TestSecretIsReservedEverywhere(t *testing.T) {
	for _, name := range []string{"secret", "password", "token", "community", "params.secret", "params.apiKey"} {
		errs := ValidateTemplate(MessageTemplate{Body: "{{" + name + "}}"})
		if len(errs) == 0 {
			t.Errorf("%q was accepted as a variable", name)
		}
	}
	// And at render time it is inert rather than blank.
	_, body := render(t, "", "{{secret}}")
	if body != "{{secret}}" {
		t.Errorf("body = %q, want the tag verbatim", body)
	}
}

// An unknown variable must be visible in the output, not silently blank: a
// blank is indistinguishable from a field that happened to be empty.
func TestUnknownVariableRendersLiterally(t *testing.T) {
	_, body := render(t, "", "a={{sevrity}} b={{severity}}")
	if body != "a={{sevrity}} b=major" {
		t.Errorf("body = %q", body)
	}
	errs := ValidateTemplate(MessageTemplate{Body: "{{sevrity}}"})
	if len(errs) != 1 || !strings.Contains(errs[0].Message, "unknown variable") {
		t.Errorf("save should have refused it: %+v", errs)
	}
}

// A param holding a map or a slice renders empty, not %v. RedactEvent walks
// only top-level string params, so printing a composite would put an address on
// the wire that the masker never had a chance to see.
func TestCompositeParamsRenderEmpty(t *testing.T) {
	e := tplEvent()
	e.Params = map[string]any{
		"nested":  map[string]any{"target": "10.0.0.1"},
		"list":    []string{"10.0.0.1"},
		"scalar":  "kept",
		"number":  42,
		"boolean": true,
	}
	_, body := RenderTemplate(e, "s", MessageTemplate{
		Body: "n={{params.nested}} l={{params.list}} s={{params.scalar}} i={{params.number}} b={{params.boolean}}",
	})
	if strings.Contains(body, "10.0.0.1") {
		t.Errorf("a composite param leaked an address: %q", body)
	}
	if body != "n= l= s=kept i=42 b=true" {
		t.Errorf("body = %q", body)
	}
}

// A redacting sink must see nothing unmasked, INCLUDING through the fields the
// built-in rendering never showed. Those are exactly what templates unlock.
func TestTemplateOnARedactedEventShowsNoRealAddress(t *testing.T) {
	ev := RedactEvent(tplEvent())
	subject, body := RenderTemplate(ev, "s", MessageTemplate{
		Subject: "{{source}} {{summary}}",
		Body:    "dedup={{dedupKey}} param={{params.source}} summary={{summary}}",
	})
	for _, out := range []string{subject, body} {
		if strings.Contains(out, "10.0.0.1") {
			t.Errorf("an unmasked address reached the output: %q", out)
		}
	}
	if !strings.Contains(body, "10.x.x.1") {
		t.Errorf("the masked form is missing, so nothing was rendered at all: %q", body)
	}
}

func TestValidateRejectsMalformedTemplates(t *testing.T) {
	cases := map[string]string{
		"unclosed braces":     "{{severity",
		"nested blocks":       "{{#severity}}{{#kind}}x{{/kind}}{{/severity}}",
		"unbalanced close":    "{{/severity}}",
		"unclosed block":      "{{#severity}}x",
		"mismatched close":    "{{#severity}}x{{/kind}}",
		"tag inside fallback": "{{corrId|{{severity}}}}",
	}
	for name, src := range cases {
		if errs := ValidateTemplate(MessageTemplate{Body: src}); len(errs) == 0 {
			t.Errorf("%s: %q was accepted", name, src)
		}
	}
	if errs := ValidateTemplate(MessageTemplate{Subject: "line\nbreak"}); len(errs) == 0 {
		t.Error("a line break in the subject was accepted")
	}
	if errs := ValidateTemplate(MessageTemplate{Body: strings.Repeat("x", MaxTemplateLen+1)}); len(errs) == 0 {
		t.Error("an oversized template was accepted")
	}
	// A valid template must produce no errors at all.
	if errs := ValidateTemplate(MessageTemplate{
		Subject: "[{{severityUpper}}] {{source}}",
		Body:    "{{#sessionName}}session={{sessionName}}{{/sessionName}}\nvalue={{value|n/a}}",
	}); len(errs) != 0 {
		t.Errorf("a valid template was refused: %+v", errs)
	}
}

// Errors carry an offset so the editor can point at the problem.
func TestValidationErrorsAreLocated(t *testing.T) {
	errs := ValidateTemplate(MessageTemplate{Body: "hello {{nope}}"})
	if len(errs) != 1 {
		t.Fatalf("got %d errors", len(errs))
	}
	if errs[0].Offset != 6 {
		t.Errorf("offset = %d, want 6", errs[0].Offset)
	}
	if errs[0].Field != "body" {
		t.Errorf("field = %q", errs[0].Field)
	}
}

// {{lb}} is how a template writes a literal brace pair.
func TestLiteralBraceEscape(t *testing.T) {
	_, body := render(t, "", "{{lb}}severity}} is {{severity}}")
	if body != "{{severity}} is major" {
		t.Errorf("body = %q", body)
	}
}

// The vocabulary must be self-consistent: everything offered to the UI must
// resolve, and nothing reserved may appear in it.
func TestEveryAdvertisedVariableResolves(t *testing.T) {
	e := tplEvent()
	for _, v := range TemplateVariables() {
		if _, ok := resolveVariable(e, "sink", v.Name); !ok {
			t.Errorf("%q is offered by the UI but the renderer does not know it", v.Name)
		}
		if reservedNames[strings.ToLower(v.Name)] {
			t.Errorf("%q is advertised and reserved at the same time", v.Name)
		}
		if v.Description != "notify.tplVar."+v.Name {
			t.Errorf("%q has description %q; the UI expects an i18n key", v.Name, v.Description)
		}
	}
}

// A body longer than the cap is truncated on a rune boundary and says so.
func TestBodyIsTruncatedOnARuneBoundary(t *testing.T) {
	e := tplEvent()
	e.Summary = strings.Repeat("é", MaxBodyLen)
	_, body := RenderTemplate(e, "s", MessageTemplate{Body: "{{summary}}"})

	if len(body) > MaxBodyLen+32 {
		t.Errorf("body is %d bytes, past the cap", len(body))
	}
	if !strings.HasSuffix(body, "[truncated]") {
		t.Error("the truncation is silent")
	}
	if !strings.HasPrefix(body, "é") || strings.Contains(body, "�") {
		t.Error("the cut landed mid-rune")
	}
}

// A subject rendered from a template is still one line, whatever was in it.
func TestTemplatedSubjectIsCollapsedToOneLine(t *testing.T) {
	e := tplEvent()
	e.Summary = "line one\r\nline two"
	subject, _ := RenderTemplate(e, "s", MessageTemplate{Subject: "{{summary}}"})
	if strings.ContainsAny(subject, "\r\n") {
		t.Errorf("subject spans lines: %q", subject)
	}
}

// The sink name is available so one template can say which route fired.
func TestSinkNameVariable(t *testing.T) {
	subject, _ := render(t, "{{sinkName}}", "")
	if subject != "NOC mail" {
		t.Errorf("subject = %q", subject)
	}
}

// The preview sample must exercise every variable a real event would, or a
// template that previews cleanly can still render empty in production.
func TestSampleEventsPopulateTheVocabulary(t *testing.T) {
	for _, kind := range []string{events.CategoryThreshold, events.CategoryTrap, events.CategoryReachability} {
		e := SampleEvent(kind)
		if e.Source == "" || e.Summary == "" || e.Kind == "" || len(e.Params) == 0 {
			t.Errorf("%s sample is too thin to preview with: %+v", kind, e)
		}
		if _, body := RenderTemplate(e, "s", MessageTemplate{Body: "{{params.source}}"}); body == "" {
			t.Errorf("%s sample has no params.source, so {{params.*}} previews empty", kind)
		}
	}
}
