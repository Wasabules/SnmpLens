package notify

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Whether a failed delivery is retried or thrown away is decided by
// `Permanent`, and it used to decide by searching the error TEXT for words. The
// error text contains whatever the peer said — an SMTP greeting, an HTTP
// response body — so the receiver got a vote on whether its own outage was
// worth retrying.

// RFC 5321 4.2.1 is unambiguous: a 4yz reply is transient and the client should
// try again. "454 4.7.0 Temporary authentication failure" is what a relay says
// while it is overloaded or rotating a token — and it contains the word
// "authentication", so the alert was dead-lettered on the first attempt and the
// operator never heard about the incident at all.
func TestTransientSMTPReplyIsRetried(t *testing.T) {
	certPEM, keyPEM := selfSigned(t, "localhost")
	srv := newSMTPServer(t, certPEM, keyPEM, true, false)
	srv.authReply = "454 4.7.0 Temporary authentication failure"

	cfg := baseConfig(t, srv, certPEM)
	cfg.Encryption = "tls"
	cfg.AuthMethod = "plain"
	cfg.Username = "svc"
	cfg.Password = "pw"

	err := (EmailSink{Config: cfg}).Send(mailEvent(), "", "body")
	if err == nil {
		t.Fatal("the relay refused and Send reported success")
	}
	if Permanent(err) {
		t.Errorf("a 454 was treated as permanent, so the alert is dead-lettered "+
			"on the first attempt: %v", err)
	}
}

// And a 5yz reply IS permanent — retrying bad credentials six times just locks
// the account.
func TestPermanentSMTPReplyIsNotRetried(t *testing.T) {
	certPEM, keyPEM := selfSigned(t, "localhost")
	srv := newSMTPServer(t, certPEM, keyPEM, true, false)
	srv.authReply = "535 5.7.8 Authentication credentials invalid"

	cfg := baseConfig(t, srv, certPEM)
	cfg.Encryption = "tls"
	cfg.AuthMethod = "plain"
	cfg.Username = "svc"
	cfg.Password = "pw"

	err := (EmailSink{Config: cfg}).Send(mailEvent(), "", "body")
	if err == nil {
		t.Fatal("the relay refused and Send reported success")
	}
	if !Permanent(err) {
		t.Errorf("a 535 was retried: %v", err)
	}
}

// An HTTP receiver's BODY must not decide anything. A 503 is transient no
// matter what prose the load balancer's error page contains, and "invalid" is
// in a great many error pages.
func TestWebhookRetryIsDecidedByStatusNotBody(t *testing.T) {
	cases := []struct {
		status int
		body   string
		perm   bool
		why    string
	}{
		{503, "invalid upstream: backend returned 404", false, "an outage behind a proxy"},
		{500, "invalid request format", false, "the server broke, not the request"},
		{502, "returned 401 from origin", false, "a proxy quoting its upstream"},
		{429, "slow down", false, "asked to come back later"},
		{408, "request timeout", false, "asked to come back later"},
		{401, "ok", true, "the credential is wrong"},
		{404, "everything is fine", true, "the endpoint is not there"},
		{422, "", true, "the payload will never be accepted"},
	}

	for _, c := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.status)
			_, _ = w.Write([]byte(c.body))
		}))

		err := (WebhookSink{Config: WebhookConfig{URL: srv.URL}}).Send(mailEvent(), "", "body")
		if err == nil {
			srv.Close()
			if c.status < 200 || c.status >= 300 {
				t.Errorf("%d was reported as a success", c.status)
			}
			continue
		}
		if got := Permanent(err); got != c.perm {
			t.Errorf("%d with body %q: Permanent = %v, want %v (%s)\n  %v",
				c.status, c.body, got, c.perm, c.why, err)
		}
		srv.Close()
	}
}

// A configuration error carries no status code at all and must still be
// permanent — retrying an empty host six times helps nobody.
func TestConfigurationErrorsStayPermanent(t *testing.T) {
	for _, msg := range []string{
		"webhook URL is empty",
		"email configuration is incomplete",
		`unknown SMTP auth method "kerberos"`,
	} {
		if !Permanent(errors.New(msg)) {
			t.Errorf("a configuration error is being retried: %q", msg)
		}
	}
}

// One dead mailbox must not silence the alert for everyone else.
//
// The RCPT loop returned on the first refusal, so a `To:` list of the on-call
// team plus one colleague who has left delivered to NOBODY — and 550 is
// permanent, so it was dead-lettered without a single retry.
func TestOneRejectedRecipientStillDeliversToTheRest(t *testing.T) {
	certPEM, keyPEM := selfSigned(t, "localhost")
	srv := newSMTPServer(t, certPEM, keyPEM, true, false)
	srv.rejectRcpt = map[string]string{
		"gone@example.com": "550 5.1.1 <gone@example.com>: Recipient address rejected",
	}

	cfg := baseConfig(t, srv, certPEM)
	cfg.Encryption = "tls"
	cfg.To = []string{"gone@example.com", "noc@example.com", "oncall@example.com"}

	if err := (EmailSink{Config: cfg}).Send(mailEvent(), "subject", "body"); err != nil {
		t.Fatalf("one dead mailbox lost the alert for everyone: %v", err)
	}
	sess := srv.waitSession(t)

	if len(sess.Rcpt) != 2 {
		t.Fatalf("accepted recipients = %v, want the two live ones", sess.Rcpt)
	}
	if sess.Data == "" {
		t.Error("the message was never sent")
	}
}

// If EVERY recipient is refused there is nobody to deliver to, and that is a
// failure — reporting success would hide a mail path that is entirely dead.
func TestAllRecipientsRejectedIsAFailure(t *testing.T) {
	certPEM, keyPEM := selfSigned(t, "localhost")
	srv := newSMTPServer(t, certPEM, keyPEM, true, false)
	srv.rejectRcpt = map[string]string{
		"a@example.com": "550 5.1.1 rejected",
		"b@example.com": "550 5.1.1 rejected",
	}

	cfg := baseConfig(t, srv, certPEM)
	cfg.Encryption = "tls"
	cfg.To = []string{"a@example.com", "b@example.com"}

	err := (EmailSink{Config: cfg}).Send(mailEvent(), "subject", "body")
	if err == nil {
		t.Fatal("every recipient was refused and Send reported success")
	}
	if !strings.Contains(err.Error(), "a@example.com") {
		t.Errorf("the error does not name a refused recipient: %v", err)
	}
	if !Permanent(err) {
		t.Errorf("a 550 for every recipient is being retried: %v", err)
	}
}
