package notify

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"SnmpLens/pkg/events"
)

func hookEvent() events.Event {
	return events.Event{
		ID: "evt-hook-1", Category: events.CategoryThreshold, Kind: events.KindThresholdOpened,
		Severity: "major", Source: "10.0.0.1", Summary: "ifInOctets above 900 on 10.0.0.1",
	}
}

type capture struct {
	method  string
	body    string
	headers http.Header
	hits    int
}

func recordingServer(t *testing.T, status int, reply string) (*httptest.Server, *capture) {
	t.Helper()
	c := &capture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.hits++
		c.method = r.Method
		b, _ := io.ReadAll(r.Body)
		c.body = string(b)
		c.headers = r.Header.Clone()
		w.WriteHeader(status)
		io.WriteString(w, reply)
	}))
	t.Cleanup(srv.Close)
	return srv, c
}

// A redirect must NOT be followed.
//
// Go rewrites a redirected POST as a GET and drops the body, so the receiver
// answers 200 having been sent nothing — and the sink would record the alert
// as delivered. Silent loss is the worst outcome an alerting path can have.
func TestWebhookDoesNotFollowRedirects(t *testing.T) {
	final, finalCap := recordingServer(t, 200, "ok")

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, final.URL, http.StatusFound)
	}))
	defer redirector.Close()

	sink := WebhookSink{Config: WebhookConfig{URL: redirector.URL}}
	err := sink.Send(hookEvent(), "subject", "body")

	if err == nil {
		t.Fatal("a redirect was reported as a successful delivery")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "redirect") {
		t.Errorf("the error should say a redirect was refused, got: %v", err)
	}
	if finalCap.hits != 0 {
		t.Errorf("the redirect target was contacted %d time(s); it should not have been", finalCap.hits)
	}
}

// The credential must not be forwarded to whatever host a redirect names.
func TestWebhookDoesNotLeakCredentialsToRedirectTarget(t *testing.T) {
	final, finalCap := recordingServer(t, 200, "ok")
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, final.URL, http.StatusFound)
	}))
	defer redirector.Close()

	sink := WebhookSink{Config: WebhookConfig{
		URL:     redirector.URL,
		Token:   "SECRET-BEARER",
		Headers: map[string]string{"X-Api-Key": "SECRET-CUSTOM"},
	}}
	_ = sink.Send(hookEvent(), "", "")

	if finalCap.hits != 0 {
		t.Fatalf("the redirect target received the request, headers included: %v", finalCap.headers)
	}
}

// A receiver that echoes the request headers in its error body must not put
// the token into notify_outbox.last_error, where it would sit in the clear in
// monitoring.db and undo the point of pkg/secrets.
func TestWebhookScrubsTheTokenFromErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		io.WriteString(w, "rejected: Authorization="+r.Header.Get("Authorization"))
	}))
	defer srv.Close()

	sink := WebhookSink{Config: WebhookConfig{URL: srv.URL, Token: "SECRET-BEARER"}}
	err := sink.Send(hookEvent(), "", "")
	if err == nil {
		t.Fatal("a 500 was reported as success")
	}
	if strings.Contains(err.Error(), "SECRET-BEARER") {
		t.Errorf("the credential was carried into the stored error: %v", err)
	}
	if !strings.Contains(err.Error(), "redacted") {
		t.Errorf("the redaction should be visible so the error stays diagnosable: %v", err)
	}
	// The rest of the diagnosis must survive, or the redaction has made the
	// failure unreadable instead of safe.
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("the status code was lost: %v", err)
	}
}

// A bearer token must not go out over plaintext http to a remote host — the
// same stance the email sink takes for SMTP credentials.
func TestWebhookRefusesCredentialOverPlaintextHTTP(t *testing.T) {
	sink := WebhookSink{Config: WebhookConfig{URL: "http://hooks.example.com/x", Token: "SECRET"}}
	err := sink.Send(hookEvent(), "", "")
	if err == nil {
		t.Fatal("the credential was sent over plaintext http")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "plaintext") {
		t.Errorf("the error should explain why, got: %v", err)
	}
}

// ...but a plaintext URL with no credential is fine, and so is loopback.
func TestWebhookAllowsPlaintextWithoutACredential(t *testing.T) {
	srv, c := recordingServer(t, 200, "ok")
	sink := WebhookSink{Config: WebhookConfig{URL: srv.URL}}
	if err := sink.Send(hookEvent(), "", ""); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if c.hits != 1 {
		t.Error("the request was not delivered")
	}
}

func TestWebhookAllowsCredentialOnLoopback(t *testing.T) {
	// httptest listens on 127.0.0.1, where a plaintext credential never leaves
	// the machine.
	srv, c := recordingServer(t, 200, "ok")
	sink := WebhookSink{Config: WebhookConfig{URL: srv.URL, Token: "SECRET"}}
	if err := sink.Send(hookEvent(), "", ""); err != nil {
		t.Fatalf("Send to loopback: %v", err)
	}
	if c.headers.Get("Authorization") != "Bearer SECRET" {
		t.Errorf("the token was not sent: %q", c.headers.Get("Authorization"))
	}
}

// The escape hatch, for an internal receiver on a trusted network.
func TestWebhookPlaintextOptInIsHonoured(t *testing.T) {
	sink := WebhookSink{Config: WebhookConfig{
		URL: "http://hooks.example.invalid/x", Token: "SECRET", AllowPlaintextHTTP: true, Timeout: 1,
	}}
	err := sink.Send(hookEvent(), "", "")
	// The host does not resolve, so this must fail at the network layer rather
	// than at the plaintext check.
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "plaintext") {
		t.Errorf("the opt-in was ignored: %v", err)
	}
}

// A custom auth header can draw on the stored credential instead of being
// typed into a field that gets written to the database.
func TestWebhookSecretPlaceholderInCustomHeader(t *testing.T) {
	srv, c := recordingServer(t, 200, "ok")
	sink := WebhookSink{Config: WebhookConfig{
		URL:     srv.URL,
		Token:   "SECRET-KEY",
		Headers: map[string]string{"X-Api-Key": SecretPlaceholder},
	}}
	if err := sink.Send(hookEvent(), "", ""); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got := c.headers.Get("X-Api-Key"); got != "SECRET-KEY" {
		t.Errorf("X-Api-Key = %q, want the stored credential", got)
	}
}

// TLS parity with the other two sinks: an internal receiver signed by a
// private CA must be reachable without disabling verification.
func TestWebhookTrustsAPrivateCA(t *testing.T) {
	certPEM, keyPEM := selfSigned(t, "localhost")
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12}
	srv.StartTLS()
	defer srv.Close()

	sink := WebhookSink{Config: WebhookConfig{URL: srv.URL, CACert: string(certPEM)}}
	if sink.Config.InsecureSkipVerify {
		t.Fatal("this test is meaningless with verification disabled")
	}
	if err := sink.Send(hookEvent(), "", ""); err != nil {
		t.Fatalf("a private CA should be trustable without disabling verification: %v", err)
	}
}

func TestWebhookRefusesAnUntrustedCertificate(t *testing.T) {
	certPEM, keyPEM := selfSigned(t, "localhost")
	pair, _ := tls.X509KeyPair(certPEM, keyPEM)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12}
	srv.StartTLS()
	defer srv.Close()

	sink := WebhookSink{Config: WebhookConfig{URL: srv.URL}}
	err := sink.Send(hookEvent(), "", "")
	if err == nil {
		t.Fatal("an unverifiable receiver certificate was accepted")
	}
	low := strings.ToLower(err.Error())
	if !strings.Contains(low, "certificate") && !strings.Contains(low, "authority") {
		t.Errorf("the error should name the certificate problem, got: %v", err)
	}
}

func TestWebhookRejectsNonHTTPSchemes(t *testing.T) {
	for _, raw := range []string{"file:///etc/passwd", "ftp://example.com/x", "ws://example.com"} {
		sink := WebhookSink{Config: WebhookConfig{URL: raw}}
		if err := sink.Send(hookEvent(), "", ""); err == nil {
			t.Errorf("%s was accepted", raw)
		}
	}
}

// The payload is the contract with the receiver; a change here breaks every
// integration silently.
func TestWebhookPayloadShape(t *testing.T) {
	srv, c := recordingServer(t, 200, "ok")
	sink := WebhookSink{Config: WebhookConfig{URL: srv.URL}}
	if err := sink.Send(hookEvent(), "Threshold opened", "ifInOctets above 900"); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if c.method != http.MethodPost {
		t.Errorf("method = %s, want POST", c.method)
	}
	if c.headers.Get("X-SnmpLens-Event-Id") != "evt-hook-1" {
		t.Error("the deduplication header is missing; retries would look like new alerts")
	}
	if c.headers.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q", c.headers.Get("Content-Type"))
	}

	var got webhookBody
	if err := json.Unmarshal([]byte(c.body), &got); err != nil {
		t.Fatalf("the body is not the documented JSON: %v — %s", err, c.body)
	}
	if got.Event.ID != "evt-hook-1" || got.Subject != "Threshold opened" ||
		got.Body != "ifInOctets above 900" || got.Sender != "SnmpLens" {
		t.Errorf("payload = %+v", got)
	}
}

func TestWebhookMalformedCAIsReported(t *testing.T) {
	sink := WebhookSink{Config: WebhookConfig{URL: "https://example.com", CACert: "nonsense"}}
	if err := sink.Send(hookEvent(), "", ""); err == nil {
		t.Fatal("expected an error for a malformed CA bundle")
	}
}

// A pool built from the CA must actually be installed on the transport, not
// silently dropped.
func TestWebhookTLSConfigCarriesTheCA(t *testing.T) {
	certPEM, _ := selfSigned(t, "localhost")
	cfg, err := WebhookSink{Config: WebhookConfig{CACert: string(certPEM)}}.tlsConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RootCAs == nil {
		t.Fatal("RootCAs was not populated")
	}
	if cfg.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %x, want TLS 1.2", cfg.MinVersion)
	}
	// Sanity: the pool really contains the certificate we supplied.
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(certPEM) {
		t.Fatal("test certificate is not valid PEM")
	}
}

func TestIsLoopback(t *testing.T) {
	cases := map[string]bool{
		"localhost": true, "localhost:8080": true,
		"127.0.0.1": true, "127.0.0.1:9000": true,
		"[::1]:80": true, "::1": true,
		"hooks.example.com": false, "10.0.0.1:80": false,
	}
	for in, want := range cases {
		if got := isLoopback(in); got != want {
			t.Errorf("isLoopback(%q) = %v, want %v", in, got, want)
		}
	}
}
