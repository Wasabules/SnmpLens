package notify

import (
	"net/smtp"
	"strconv"
	"strings"
	"testing"
	"time"

	"SnmpLens/pkg/events"
)

func timeoutAfter() <-chan time.Time { return time.After(10 * time.Second) }

func mailEvent() events.Event {
	return events.Event{
		ID: "evt-mail-1", Category: events.CategoryThreshold, Kind: events.KindThresholdOpened,
		Severity: "major", Source: "10.0.0.1",
		Ts: time.Now().UTC().Format(time.RFC3339), Summary: "ifInOctets above 900 on 10.0.0.1",
	}
}

// baseConfig points at the test server and trusts its throwaway certificate as
// a private CA — the internal-relay case, NOT verification switched off.
func baseConfig(t *testing.T, s *smtpServer, certPEM []byte) EmailConfig {
	t.Helper()
	host, portStr := s.addr()
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("port: %v", err)
	}
	return EmailConfig{
		Host: host, Port: port,
		From: "snmplens@example.com", To: []string{"noc@example.com"},
		CACert:  string(certPEM),
		Timeout: 5,
	}
}

// Implicit TLS, as port 465 does: encrypted from the first byte.
func TestEmailOverImplicitTLS(t *testing.T) {
	certPEM, keyPEM := selfSigned(t, "localhost")
	srv := newSMTPServer(t, certPEM, keyPEM, true, false)

	cfg := baseConfig(t, srv, certPEM)
	cfg.Encryption = "tls"
	cfg.AuthMethod = "plain"
	cfg.Username = "svc"

	if err := (EmailSink{Config: cfg}).Send(mailEvent(), "Threshold", "ifInOctets above 900"); err != nil {
		t.Fatalf("Send over implicit TLS: %v", err)
	}
	sess := srv.waitSession(t)

	if !sess.ImplicitTLS {
		t.Error("the connection was not TLS from the start")
	}
	if sess.AuthOverPlaintext {
		t.Error("credentials were presented before the connection was encrypted")
	}
	if sess.From != "snmplens@example.com" || len(sess.Rcpt) != 1 || sess.Rcpt[0] != "noc@example.com" {
		t.Errorf("envelope wrong: from=%q rcpt=%v", sess.From, sess.Rcpt)
	}
	if !strings.Contains(sess.Data, "ifInOctets above 900") {
		t.Errorf("body did not arrive: %q", sess.Data)
	}
}

// STARTTLS, as port 587 does: the connection starts in the clear and is
// upgraded BEFORE anything sensitive is sent.
func TestEmailOverSTARTTLS(t *testing.T) {
	certPEM, keyPEM := selfSigned(t, "localhost")
	srv := newSMTPServer(t, certPEM, keyPEM, false, true)

	cfg := baseConfig(t, srv, certPEM)
	cfg.Encryption = "starttls"
	cfg.AuthMethod = "plain"
	cfg.Username = "svc"

	if err := (EmailSink{Config: cfg}).Send(mailEvent(), "", "body"); err != nil {
		t.Fatalf("Send over STARTTLS: %v", err)
	}
	sess := srv.waitSession(t)

	if !sess.STARTTLSUsed {
		t.Fatal("STARTTLS was never negotiated; the session stayed in the clear")
	}
	if sess.AuthOverPlaintext {
		t.Error("credentials were presented before the STARTTLS upgrade")
	}
}

// The credential must actually reach the relay intact — a mangled password is
// indistinguishable from a wrong one when a delivery fails at 03:00.
func TestEmailAuthPlainCarriesTheCredential(t *testing.T) {
	certPEM, keyPEM := selfSigned(t, "localhost")
	srv := newSMTPServer(t, certPEM, keyPEM, true, false)

	cfg := baseConfig(t, srv, certPEM)
	cfg.Encryption = "tls"
	cfg.AuthMethod = "plain"
	cfg.Username = "svc-account"
	cfg.Password = "p@ssw0rd with spaces"

	if err := (EmailSink{Config: cfg}).Send(mailEvent(), "", "body"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	sess := srv.waitSession(t)

	if sess.AuthMechanism != "PLAIN" {
		t.Errorf("mechanism = %q, want PLAIN", sess.AuthMechanism)
	}
	if sess.Username != "svc-account" || sess.Password != "p@ssw0rd with spaces" {
		t.Errorf("credentials did not arrive intact: user=%q pass=%q", sess.Username, sess.Password)
	}
}

// AUTH LOGIN is the non-standard mechanism on-prem Exchange commonly demands,
// and the one net/smtp does not provide, so it is entirely our code.
func TestEmailAuthLoginHandshake(t *testing.T) {
	certPEM, keyPEM := selfSigned(t, "localhost")
	srv := newSMTPServer(t, certPEM, keyPEM, true, false)

	cfg := baseConfig(t, srv, certPEM)
	cfg.Encryption = "tls"
	cfg.AuthMethod = "login"
	cfg.Username = "DOMAIN\\svc"
	cfg.Password = "hunter2"

	if err := (EmailSink{Config: cfg}).Send(mailEvent(), "", "body"); err != nil {
		t.Fatalf("Send with AUTH LOGIN: %v", err)
	}
	sess := srv.waitSession(t)

	if sess.AuthMechanism != "LOGIN" {
		t.Errorf("mechanism = %q, want LOGIN", sess.AuthMechanism)
	}
	if sess.Username != "DOMAIN\\svc" || sess.Password != "hunter2" {
		t.Errorf("credentials did not arrive intact: user=%q pass=%q", sess.Username, sess.Password)
	}
	if sess.AuthOverPlaintext {
		t.Error("AUTH LOGIN credentials were sent before encryption")
	}
}

// A relay presenting an untrusted certificate must be refused, or "encrypted"
// means nothing about who is on the other end.
func TestEmailUntrustedCertificateIsRefused(t *testing.T) {
	certPEM, keyPEM := selfSigned(t, "localhost")
	srv := newSMTPServer(t, certPEM, keyPEM, true, false)

	cfg := baseConfig(t, srv, certPEM)
	cfg.CACert = "" // fall back to the system trust store, which does not know it
	cfg.Encryption = "tls"

	err := (EmailSink{Config: cfg}).Send(mailEvent(), "", "body")
	if err == nil {
		t.Fatal("an unverifiable relay certificate was accepted")
	}
	low := strings.ToLower(err.Error())
	if !strings.Contains(low, "certificate") && !strings.Contains(low, "authority") {
		t.Errorf("the error should name the certificate problem, got: %v", err)
	}
}

// The same, for the STARTTLS upgrade rather than the initial handshake.
func TestEmailSTARTTLSVerifiesTheCertificate(t *testing.T) {
	certPEM, keyPEM := selfSigned(t, "localhost")
	srv := newSMTPServer(t, certPEM, keyPEM, false, true)

	cfg := baseConfig(t, srv, certPEM)
	cfg.CACert = ""
	cfg.Encryption = "starttls"

	if err := (EmailSink{Config: cfg}).Send(mailEvent(), "", "body"); err == nil {
		t.Fatal("STARTTLS accepted an unverifiable certificate")
	}
}

// The escape hatch has to work: a lab relay with a self-signed certificate is
// a real situation.
func TestEmailInsecureSkipVerifyIsHonoured(t *testing.T) {
	certPEM, keyPEM := selfSigned(t, "localhost")
	srv := newSMTPServer(t, certPEM, keyPEM, true, false)

	cfg := baseConfig(t, srv, certPEM)
	cfg.CACert = ""
	cfg.InsecureSkipVerify = true
	cfg.Encryption = "tls"

	if err := (EmailSink{Config: cfg}).Send(mailEvent(), "", "body"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	srv.waitSession(t)
}

// The whole reason CACert was added: an internal relay signed by a private CA
// must be reachable WITHOUT disabling verification.
func TestEmailPrivateCAIsTrustedWithoutDisablingVerification(t *testing.T) {
	certPEM, keyPEM := selfSigned(t, "localhost")
	srv := newSMTPServer(t, certPEM, keyPEM, true, false)

	cfg := baseConfig(t, srv, certPEM) // CACert set, InsecureSkipVerify false
	cfg.Encryption = "tls"

	if cfg.InsecureSkipVerify {
		t.Fatal("this test is meaningless with verification disabled")
	}
	if err := (EmailSink{Config: cfg}).Send(mailEvent(), "", "body"); err != nil {
		t.Fatalf("a private CA should be trustable without disabling verification: %v", err)
	}
	srv.waitSession(t)
}

// Asking for STARTTLS against a relay that does not offer it must fail loudly.
// Silently continuing in the clear would send the password in plaintext to a
// relay the operator explicitly asked to reach over TLS.
func TestEmailSTARTTLSDemandedButNotOffered(t *testing.T) {
	certPEM, keyPEM := selfSigned(t, "localhost")
	srv := newSMTPServer(t, certPEM, keyPEM, false, false) // no STARTTLS advertised

	cfg := baseConfig(t, srv, certPEM)
	cfg.Encryption = "starttls"

	err := (EmailSink{Config: cfg}).Send(mailEvent(), "", "body")
	if err == nil {
		t.Fatal("a relay with no STARTTLS was accepted for an encrypted sink")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "starttls") {
		t.Errorf("the error should say STARTTLS was unavailable, got: %v", err)
	}
}

// Our AUTH LOGIN refuses to hand over credentials on an unencrypted
// connection. This is stricter than the standard library, which exempts
// localhost for PLAIN; for a mechanism we implement ourselves there is no
// reason to carve out that exception.
func TestEmailAuthLoginRefusesPlaintextRelay(t *testing.T) {
	certPEM, keyPEM := selfSigned(t, "localhost")
	srv := newSMTPServer(t, certPEM, keyPEM, false, false)

	cfg := baseConfig(t, srv, certPEM)
	cfg.Encryption = "none"
	cfg.AuthMethod = "login"
	cfg.Username = "svc"
	cfg.Password = "hunter2"

	err := (EmailSink{Config: cfg}).Send(mailEvent(), "", "body")
	if err == nil {
		t.Fatal("credentials were sent over an unencrypted connection")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "unencrypted") {
		t.Errorf("the error should say why, got: %v", err)
	}
}

// The contract we rely on from the standard library: PlainAuth refuses to send
// a password over an unencrypted connection to a non-local relay. Pinned here
// because our own code decides when to offer PLAIN at all.
func TestStdlibPlainAuthRefusesRemotePlaintext(t *testing.T) {
	auth := smtp.PlainAuth("", "svc", "hunter2", "smtp.example.com")
	if _, _, err := auth.Start(&smtp.ServerInfo{Name: "smtp.example.com", TLS: false}); err == nil {
		t.Fatal("PlainAuth sent credentials over an unencrypted connection to a remote relay")
	}
}

// Unauthenticated relaying is legitimate on an internal MTA, and must work.
func TestEmailWithoutAuthentication(t *testing.T) {
	certPEM, keyPEM := selfSigned(t, "localhost")
	srv := newSMTPServer(t, certPEM, keyPEM, true, false)

	cfg := baseConfig(t, srv, certPEM)
	cfg.Encryption = "tls"
	cfg.AuthMethod = "none"

	if err := (EmailSink{Config: cfg}).Send(mailEvent(), "", "body"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	sess := srv.waitSession(t)
	if sess.AuthMechanism != "" {
		t.Errorf("authentication was attempted anyway: %q", sess.AuthMechanism)
	}
}

// Several recipients must all be offered, or half the on-call rota silently
// never hears about an incident.
func TestEmailSendsToEveryRecipient(t *testing.T) {
	certPEM, keyPEM := selfSigned(t, "localhost")
	srv := newSMTPServer(t, certPEM, keyPEM, true, false)

	cfg := baseConfig(t, srv, certPEM)
	cfg.Encryption = "tls"
	cfg.To = []string{"noc@example.com", " oncall@example.com ", "boss@example.com"}

	if err := (EmailSink{Config: cfg}).Send(mailEvent(), "", "body"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	sess := srv.waitSession(t)
	if len(sess.Rcpt) != 3 {
		t.Fatalf("RCPT TO issued %d times, want 3: %v", len(sess.Rcpt), sess.Rcpt)
	}
	// The middle address has surrounding whitespace in the config; an
	// untrimmed one would be rejected by a real relay.
	for _, want := range []string{"noc@example.com", "oncall@example.com", "boss@example.com"} {
		found := false
		for _, got := range sess.Rcpt {
			if got == want {
				found = true
			}
		}
		if !found {
			t.Errorf("%s was never offered: %v", want, sess.Rcpt)
		}
	}
}

// A non-ASCII subject must arrive MIME-encoded rather than as mojibake, and
// the event id must be present so a mail client threads retries.
func TestEmailMessageHeadersSurviveTheWire(t *testing.T) {
	certPEM, keyPEM := selfSigned(t, "localhost")
	srv := newSMTPServer(t, certPEM, keyPEM, true, false)

	cfg := baseConfig(t, srv, certPEM)
	cfg.Encryption = "tls"

	if err := (EmailSink{Config: cfg}).Send(mailEvent(), "Seuil dépassé — 数值过高", "corps"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	sess := srv.waitSession(t)

	if !strings.Contains(sess.Data, "Subject: =?utf-8?") {
		t.Errorf("the subject was not MIME-encoded: %q", firstLines(sess.Data, 8))
	}
	if !strings.Contains(sess.Data, "evt-mail-1") {
		t.Error("the event id is missing; retries would show as separate mails")
	}
	if !strings.Contains(sess.Data, "charset=utf-8") {
		t.Error("the body charset was not declared")
	}
}

// A line consisting of a single dot ends the DATA phase. An unescaped one in a
// device description would truncate the mail and leave the relay parsing the
// remainder as commands.
func TestEmailDotStuffing(t *testing.T) {
	certPEM, keyPEM := selfSigned(t, "localhost")
	srv := newSMTPServer(t, certPEM, keyPEM, true, false)

	cfg := baseConfig(t, srv, certPEM)
	cfg.Encryption = "tls"

	body := "before\r\n.\r\nafter"
	if err := (EmailSink{Config: cfg}).Send(mailEvent(), "", body); err != nil {
		t.Fatalf("Send: %v", err)
	}
	sess := srv.waitSession(t)

	if !strings.Contains(sess.Data, "after") {
		t.Errorf("the message was truncated at the bare dot: %q", sess.Data)
	}
}

func TestEmailRejectsIncompleteConfig(t *testing.T) {
	cases := map[string]EmailConfig{
		"no host":       {From: "a@b.c", To: []string{"d@e.f"}},
		"no sender":     {Host: "smtp", To: []string{"d@e.f"}},
		"no recipients": {Host: "smtp", From: "a@b.c"},
	}
	for name, cfg := range cases {
		if err := (EmailSink{Config: cfg}).Send(mailEvent(), "", ""); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestEmailMalformedCAIsReported(t *testing.T) {
	cfg := EmailConfig{Host: "smtp.example.com", From: "a@b.c", To: []string{"d@e.f"}, CACert: "nonsense"}
	if _, err := cfg.tlsConfig(); err == nil {
		t.Fatal("expected an error for a malformed CA bundle")
	}
}

func TestEmailServerNameDefaultsToHost(t *testing.T) {
	c, err := EmailConfig{Host: "smtp.example.com"}.tlsConfig()
	if err != nil {
		t.Fatal(err)
	}
	if c.ServerName != "smtp.example.com" {
		t.Errorf("ServerName = %q", c.ServerName)
	}
	c, err = EmailConfig{Host: "10.0.0.9", ServerName: "smtp.example.com"}.tlsConfig()
	if err != nil {
		t.Fatal(err)
	}
	if c.ServerName != "smtp.example.com" {
		t.Errorf("the override was ignored: %q", c.ServerName)
	}
	if c.MinVersion != 0x0303 { // TLS 1.2
		t.Errorf("MinVersion = %x, want TLS 1.2", c.MinVersion)
	}
}

func firstLines(s string, n int) string {
	lines := strings.SplitN(s, "\n", n+1)
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}
