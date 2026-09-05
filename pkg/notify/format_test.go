package notify

import (
	"strings"
	"testing"

	"SnmpLens/pkg/events"
)

// A body carries values that arrived from the network unauthenticated. Each
// format has one character that ends the document early, and each of these is a
// receiver getting something other than what the operator wrote.
func TestSubstitutedValuesAreEscapedForTheirFormat(t *testing.T) {
	hostile := events.Event{
		Category: events.CategoryTrap,
		Severity: "major",
		Source:   `10.0.0.1"><script>`,
		Summary:  `a & b < c > d " e`,
		OID:      "1.3.6.1",
		Ts:       "2026-09-01T09:12:44Z",
	}

	cases := []struct {
		name     string
		render   func() string
		mustNot  []string
		contains []string
	}{
		{
			name: "json",
			render: func() string {
				_, b := RenderWebhookTemplate(
					WebhookConfig{PayloadMode: PayloadTemplate, BodyFormat: BodyJSON},
					hostile, "s", MessageTemplate{Body: `{"s":"{{source}}"}`})
				return b
			},
			// A bare quote would close the string and make the object invalid.
			mustNot: []string{`"s":"10.0.0.1"><script>"`},
		},
		{
			name: "xml",
			render: func() string {
				_, b := RenderWebhookTemplate(
					WebhookConfig{PayloadMode: PayloadTemplate, BodyFormat: BodyXML},
					hostile, "s", MessageTemplate{Body: `<alert><src>{{source}}</src></alert>`})
				return b
			},
			mustNot:  []string{"<script>"},
			contains: []string{"&lt;script&gt;"},
		},
		{
			name: "form",
			render: func() string {
				_, b := RenderWebhookTemplate(
					WebhookConfig{PayloadMode: PayloadTemplate, BodyFormat: BodyForm},
					hostile, "s", MessageTemplate{Body: `src={{source}}&msg={{summary}}`})
				return b
			},
			// An unescaped & would split one field into two.
			mustNot:  []string{"a & b"},
			contains: []string{"a+%26+b"},
		},
		{
			name: "html mail",
			render: func() string {
				_, b := RenderHTMLTemplate(hostile, "s", MessageTemplate{Body: `<p>{{summary}}</p>`})
				return b
			},
			mustNot:  []string{"a & b < c"},
			contains: []string{"&amp;", "&lt;"},
		},
		{
			name: "plain text is not escaped",
			render: func() string {
				_, b := RenderWebhookTemplate(
					WebhookConfig{PayloadMode: PayloadTemplate, BodyFormat: BodyText},
					hostile, "s", MessageTemplate{Body: `{{summary}}`})
				return b
			},
			contains: []string{`a & b < c > d " e`},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.render()
			for _, bad := range tc.mustNot {
				if strings.Contains(got, bad) {
					t.Errorf("unescaped %q survived into the body: %s", bad, got)
				}
			}
			for _, want := range tc.contains {
				if !strings.Contains(got, want) {
					t.Errorf("expected %q in the body, got: %s", want, got)
				}
			}
		})
	}
}

// The header and the body have to agree. Declaring application/json over a form
// body is the failure this setting exists to prevent, and it is silent: the
// receiver parses nothing and answers 400 to a request nobody is watching.
func TestContentTypeFollowsTheBodyFormat(t *testing.T) {
	for _, tc := range []struct {
		mode, format, want string
	}{
		{PayloadTemplate, BodyJSON, "application/json"},
		{PayloadTemplate, BodyText, "text/plain; charset=utf-8"},
		{PayloadTemplate, BodyXML, "application/xml; charset=utf-8"},
		{PayloadTemplate, BodyForm, "application/x-www-form-urlencoded"},
		{PayloadTemplate, "", "application/json"},
		{PayloadTemplate, "nonsense", "application/json"},
		// Envelope mode is always our object, whatever the format says.
		{"envelope", BodyXML, "application/json"},
		{"", BodyForm, "application/json"},
	} {
		cfg := WebhookConfig{PayloadMode: tc.mode, BodyFormat: tc.format}
		if got := cfg.ContentType(); got != tc.want {
			t.Errorf("mode=%q format=%q: content type = %q, want %q",
				tc.mode, tc.format, got, tc.want)
		}
	}
}

// Well-formedness is checked before the request goes out, per format. Text and
// form have no shape to be wrong; JSON and XML do.
func TestWellFormednessIsCheckedPerFormat(t *testing.T) {
	prose := "ifInOctets reached 912.5, above 900"

	if err := (WebhookConfig{BodyFormat: BodyJSON}).wellFormed(prose); err == nil {
		t.Error("prose passed as JSON")
	}
	if err := (WebhookConfig{BodyFormat: BodyXML}).wellFormed(prose); err == nil {
		t.Error("prose passed as XML")
	}
	if err := (WebhookConfig{BodyFormat: BodyXML}).wellFormed("<a><b/></a>"); err != nil {
		t.Errorf("well-formed XML was rejected: %v", err)
	}
	// An unclosed element is the mistake a template actually makes.
	if err := (WebhookConfig{BodyFormat: BodyXML}).wellFormed("<a><b></a>"); err == nil {
		t.Error("mismatched XML tags passed")
	}
	for _, f := range []string{BodyText, BodyForm} {
		if err := (WebhookConfig{BodyFormat: f}).wellFormed(prose); err != nil {
			t.Errorf("%s rejected plain text: %v", f, err)
		}
	}
}

// An empty template has a usable default in JSON and deliberately none in the
// other formats — where the built-in prose rendering is not well-formed, and is
// refused at save time rather than posted.
func TestAnEmptyTemplateFallsBackPerFormat(t *testing.T) {
	_, body := RenderWebhookTemplate(
		WebhookConfig{PayloadMode: PayloadTemplate, BodyFormat: BodyJSON},
		SampleEvent(events.CategoryThreshold), "s", MessageTemplate{})
	if err := (WebhookConfig{BodyFormat: BodyJSON}).wellFormed(strings.TrimSpace(body)); err != nil {
		t.Errorf("the JSON default is not valid JSON: %v", err)
	}

	err := ValidatePayloadTemplate(SinkConfig{
		Kind:    SinkWebhook,
		Webhook: WebhookConfig{PayloadMode: PayloadTemplate, BodyFormat: BodyXML},
	})
	if err == nil {
		t.Error("an empty XML template saved cleanly; it would post prose as XML")
	}
}

// The mail sink declares what it is sending, and the escaping follows the same
// setting — a text/html header over JSON-escaped values would be the webhook bug
// again, one protocol along.
func TestEmailContentTypeFollowsTheFormat(t *testing.T) {
	ev := SampleEvent(events.CategoryThreshold)

	plain := PreviewMessage(EmailConfig{From: "a@x", To: []string{"b@x"}}, ev, "s", "body")
	if !strings.Contains(plain, "Content-Type: text/plain; charset=utf-8") {
		t.Errorf("default is not text/plain:\n%s", plain)
	}

	html := PreviewMessage(
		EmailConfig{From: "a@x", To: []string{"b@x"}, Format: EmailFormatHTML}, ev, "s", "<p>hi</p>")
	if !strings.Contains(html, "Content-Type: text/html; charset=utf-8") {
		t.Errorf("html format is not declared text/html:\n%s", html)
	}
	// Unrecognised values must not silently become HTML.
	odd := PreviewMessage(
		EmailConfig{From: "a@x", To: []string{"b@x"}, Format: "markdown"}, ev, "s", "body")
	if !strings.Contains(odd, "Content-Type: text/plain") {
		t.Errorf("an unknown format did not fall back to text/plain:\n%s", odd)
	}
}
