package notify

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"SnmpLens/pkg/events"
)

// SecretPlaceholder can be used in a custom header value, or in the URL, to
// inject the sink's stored credential.
//
// Custom headers are stored with the configuration, in the clear, because they
// are ordinarily routing metadata rather than secrets. A receiver that
// authenticates with something other than a bearer token — X-API-Key is the
// common one — would otherwise force the operator to type a credential into a
// field that gets written to monitoring.db. Writing {{secret}} there keeps it
// in pkg/secrets with everything else.
//
// The URL matters for the same reason and more sharply: Slack, Teams and
// Discord — the exact receivers PayloadMode "template" was built for —
// authenticate by the URL alone, so for those receivers there is no bearer
// token to store and the address itself IS the credential. Without this it went
// into the notify_sinks config blob in the clear, and a copied monitoring.db
// handed over a working post-anything-to-that-channel capability.
const SecretPlaceholder = "{{secret}}"

// WebhookConfig describes an HTTP destination.
type WebhookConfig struct {
	URL     string            `json:"url"`
	Method  string            `json:"method"`  // defaults to POST
	Headers map[string]string `json:"headers"` // extra headers, e.g. a custom auth scheme
	// Token is json:"-": see EmailConfig.Password. It is supplied from
	// pkg/secrets, never persisted alongside the sink configuration.
	Token   string `json:"-"`       // sent as "Authorization: Bearer <token>"
	Timeout int    `json:"timeout"` // seconds; 0 means 10

	// PayloadMode chooses what is actually POSTed:
	//
	//   "envelope" (default) — the fixed JSON object below, with the rendered
	//     text in its body field. Predictable, and what every existing sink
	//     already receives.
	//   "template" — the sink's message template IS the payload, sent as
	//     written. This is how you talk to Slack, Teams or Alertmanager, which
	//     all want their own shape rather than ours.
	PayloadMode string `json:"payloadMode,omitempty"`

	// AllowPlaintextHTTP permits sending the credential over http://.
	// Without it, a sink with a credential refuses a plaintext URL: a bearer
	// token on the wire in the clear is exactly what the rest of this package
	// goes to some length to avoid. Loopback is always allowed.
	AllowPlaintextHTTP bool `json:"allowPlaintextHttp,omitempty"`

	// --- TLS, matching the syslog and email sinks ---

	// CACert is a PEM bundle trusting a private CA, for an internal receiver.
	CACert string `json:"caCert,omitempty"`
	// ServerName overrides the name checked against the certificate.
	ServerName string `json:"serverName,omitempty"`
	// InsecureSkipVerify disables certificate verification entirely.
	InsecureSkipVerify bool `json:"insecureSkipVerify,omitempty"`
}

// WebhookSink posts the event as JSON.
//
// This is also the escape hatch for logic the fixed-field rules cannot express:
// route broadly to a webhook and decide on your own side. It uses the default
// proxy configuration, so it honours HTTP_PROXY/HTTPS_PROXY — which makes it
// the sink most likely to work at all inside a locked-down corporate network.
type WebhookSink struct {
	Config WebhookConfig
}

type webhookBody struct {
	Event   events.Event `json:"event"`
	Subject string       `json:"subject"`
	Body    string       `json:"body"`
	Sender  string       `json:"sender"`
}

// isLoopback reports whether the URL points at this machine, where a plaintext
// credential never leaves the host.
func isLoopback(host string) bool {
	h := host
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		h = parsed
	}
	if strings.EqualFold(h, "localhost") {
		return true
	}
	if ip := net.ParseIP(h); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// resolvedURL is the address to actually request, with {{secret}} expanded.
//
// Substituted BEFORE parsing, or the braces are percent-encoded and the
// placeholder is requested literally — measured, a URL ending in
// "/services/{{secret}}" reached the receiver as "/services/%7B%7Bsecret%7D%7D"
// — and validate would then be checking an address that is not the one sent.
//
// The credential goes in verbatim, not percent-escaped: a Slack token is
// "T000/B000/xxxx" and escaping its slashes would address a path that does not
// exist.
func (w WebhookSink) resolvedURL() string {
	raw := strings.TrimSpace(w.Config.URL)
	if w.Config.Token == "" {
		return raw
	}
	return strings.ReplaceAll(raw, SecretPlaceholder, w.Config.Token)
}

// validate checks the destination before anything is sent.
func (w WebhookSink) validate() (*url.URL, error) {
	raw := w.resolvedURL()
	if raw == "" {
		return nil, fmt.Errorf("webhook URL is empty")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("webhook URL is not valid: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return nil, fmt.Errorf("webhook URL must be http or https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("webhook URL has no host")
	}

	if strings.EqualFold(u.Scheme, "http") && w.carriesCredential() &&
		!w.Config.AllowPlaintextHTTP && !isLoopback(u.Host) {
		return nil, fmt.Errorf(
			"refusing to send the webhook credential over plaintext http to %s; use https, or enable the plaintext option explicitly", u.Host)
	}
	return u, nil
}

// carriesCredential reports whether this request would carry a secret.
func (w WebhookSink) carriesCredential() bool {
	if w.Config.Token != "" {
		return true
	}
	for _, v := range w.Config.Headers {
		if strings.Contains(v, SecretPlaceholder) {
			return true
		}
	}
	return false
}

// tlsConfig builds the transport security settings.
func (w WebhookSink) tlsConfig() (*tls.Config, error) {
	out := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		ServerName:         strings.TrimSpace(w.Config.ServerName),
		InsecureSkipVerify: w.Config.InsecureSkipVerify, // #nosec G402 -- opt-in and named for what it does
	}
	if ca := strings.TrimSpace(w.Config.CACert); ca != "" {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM([]byte(ca)) {
			return nil, fmt.Errorf("the CA certificate is not valid PEM")
		}
		out.RootCAs = pool
	}
	return out, nil
}

// client builds the HTTP client.
//
// Redirects are deliberately NOT followed. Go rewrites a redirected POST as a
// GET and drops the body, so a receiver behind a 302 would answer 200 having
// received nothing at all — and this sink would report the alert as delivered.
// Silent loss is the worst outcome an alerting path can have, so a redirect is
// surfaced as an error instead. It also stops custom auth headers from being
// forwarded to whatever host the redirect names.
func (w WebhookSink) client(timeout time.Duration) (*http.Client, error) {
	tlsCfg, err := w.tlsConfig()
	if err != nil {
		return nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = tlsCfg

	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}, nil
}

// Send posts one event.
func (w WebhookSink) Send(e events.Event, subject, body string) error {
	if _, err := w.validate(); err != nil {
		return err
	}

	payload, err := w.payload(e, subject, body)
	if err != nil {
		return err
	}

	method := strings.ToUpper(strings.TrimSpace(w.Config.Method))
	if method == "" {
		method = http.MethodPost
	}

	timeout := time.Duration(w.Config.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	req, err := http.NewRequest(method, w.resolvedURL(), bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "SnmpLens")
	// The stable event id lets the receiver deduplicate: delivery is
	// at-least-once, never exactly-once.
	req.Header.Set("X-SnmpLens-Event-Id", e.ID)
	for k, v := range w.Config.Headers {
		req.Header.Set(k, strings.ReplaceAll(v, SecretPlaceholder, w.Config.Token))
	}
	if w.Config.Token != "" {
		req.Header.Set("Authorization", "Bearer "+w.Config.Token)
	}

	client, err := w.client(timeout)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return w.scrub(err)
	}
	defer resp.Body.Close()
	// Drain a little of the body so the connection can be reused, and so the
	// error message says something useful.
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))

	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		// Scrubbed like every other branch. The Location header is chosen by
		// the receiver, and this was the one path that returned it raw.
		return w.scrub(fmt.Errorf(
			"webhook returned a redirect (%s) to %q; redirects are not followed because they would drop the request body — point the sink at the final URL",
			resp.Status, resp.Header.Get("Location")))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return w.scrub(&HTTPStatusError{
			Code: resp.StatusCode,
			Msg: fmt.Sprintf("webhook returned %s: %s", resp.Status,
				strings.TrimSpace(string(snippet))),
		})
	}
	return nil
}

// HTTPStatusError carries the status a receiver actually returned.
//
// Whether a delivery is retried used to be decided by searching the error TEXT,
// and that text ends with a snippet of the receiver's response body — which the
// receiver writes. A 503 from a load balancer whose error page says "invalid
// upstream" was classified permanent and the alert was thrown away on the first
// attempt; a proxy quoting "returned 401 from origin" did the same. The code is
// the only part of the answer we can trust to mean what it says.
type HTTPStatusError struct {
	Code int
	Msg  string
}

func (e *HTTPStatusError) Error() string { return e.Msg }

// scrub removes the sink's credential from an error before it is returned.
//
// The error is stored in notify_outbox.last_error and surfaced in the event
// journal, so it outlives the request. A receiver that echoes the request
// headers back in its error body — debug endpoints routinely do — would
// otherwise write the bearer token into monitoring.db in the clear, undoing
// the point of keeping it in pkg/secrets at all.
func (w WebhookSink) scrub(err error) error {
	return scrubSecret(err, w.Config.Token)
}

// PayloadTemplate is the mode where the template writes the whole body.
const PayloadTemplate = "template"

// payload builds the request body.
func (w WebhookSink) payload(e events.Event, subject, body string) ([]byte, error) {
	if strings.EqualFold(strings.TrimSpace(w.Config.PayloadMode), PayloadTemplate) {
		trimmed := strings.TrimSpace(body)
		if trimmed == "" {
			return nil, fmt.Errorf("this webhook sends its message template as the payload, but the template rendered nothing")
		}
		// Refuse to post something that is not JSON rather than let the
		// receiver reject it with a message nobody will see. The template was
		// rendered with values escaped as JSON, so a failure here is the
		// operator's punctuation rather than the event's content.
		if !json.Valid([]byte(trimmed)) {
			return nil, fmt.Errorf("the rendered payload is not valid JSON; check the quotes and commas in the message template")
		}
		return []byte(trimmed), nil
	}
	return json.Marshal(webhookBody{Event: e, Subject: subject, Body: body, Sender: "SnmpLens"})
}

// Describe names the destination for the delivery log.
func (w WebhookSink) Describe() string {
	return "webhook " + w.Config.URL
}

// PreviewPayload returns the exact bytes a webhook would POST.
//
// Exported so the settings editor can show what will be sent rather than an
// approximation of it. In envelope mode that is the fixed object with the
// rendered text inside — showing only the text would hide the shape the
// receiver actually gets, which is the thing being configured.
func PreviewPayload(cfg WebhookConfig, e events.Event, subject, body string) ([]byte, error) {
	return WebhookSink{Config: cfg}.payload(e, subject, body)
}

// ValidatePayloadTemplate checks a webhook whose template IS its payload.
//
// Rendered against a sample event, because the shape can only be wrong once
// values are in it: a template that looks like JSON with the placeholders still
// in place can stop being JSON the moment one of them expands.
func ValidatePayloadTemplate(cfg SinkConfig) error {
	if cfg.Kind != SinkWebhook {
		return nil
	}
	if !strings.EqualFold(strings.TrimSpace(cfg.Webhook.PayloadMode), PayloadTemplate) {
		return nil
	}
	// An empty template is fine here: RenderJSONTemplate falls back to
	// DefaultJSONPayload, so the mode always produces valid JSON.
	for _, kind := range []string{events.CategoryThreshold, events.CategoryTrap, events.CategoryReachability} {
		_, body := RenderJSONTemplate(SampleEvent(kind), cfg.Name, cfg.Template)
		if !json.Valid([]byte(strings.TrimSpace(body))) {
			return fmt.Errorf("the payload is not valid JSON once a %s event is rendered into it", kind)
		}
	}
	return nil
}
