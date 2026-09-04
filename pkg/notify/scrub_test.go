package notify

import (
	"bufio"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"SnmpLens/pkg/events"
)

// A sink's error is stored in notify_outbox.last_error and shown in the event
// journal, so it OUTLIVES the request — and the receiver chooses what its own
// error text says. A debug endpoint that echoes request headers, or an SMTP
// server that quotes the AUTH line back, writes the credential into
// monitoring.db where it stays.

func TestScrubSecret(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		secret string
		want   string
	}{
		{"removes it", errors.New("rejected: Bearer s3cr3t-token"), "s3cr3t-token",
			"rejected: Bearer [redacted]"},
		{"every occurrence", errors.New("a s3cr3t b s3cr3t"), "s3cr3t", "a [redacted] b [redacted]"},
		{"leaves an unrelated error alone", errors.New("connection refused"), "s3cr3t",
			"connection refused"},
		{"an empty secret matches nothing", errors.New("a b c"), "", "a b c"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := scrubSecret(c.err, c.secret)
			if got.Error() != c.want {
				t.Errorf("got %q, want %q", got.Error(), c.want)
			}
		})
	}
	if scrubSecret(nil, "x") != nil {
		t.Error("a nil error came back non-nil")
	}
}

// The redirect branch was the one webhook path that returned the receiver's
// text unscrubbed.
func TestWebhookRedirectErrorIsScrubbed(t *testing.T) {
	const token = "s3cr3t-bearer-token"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A receiver that puts what it was sent into the Location it answers
		// with. Contrived, and entirely its choice.
		w.Header().Set("Location", "https://elsewhere.example/?auth="+r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()

	sink := WebhookSink{Config: WebhookConfig{URL: srv.URL, Token: token}}
	err := sink.Send(events.Event{ID: "e1", Summary: "x"}, "s", "b")
	if err == nil {
		t.Fatal("a redirect was accepted")
	}
	if strings.Contains(err.Error(), token) {
		t.Errorf("the token reached the error, and from there monitoring.db:\n%s", err)
	}
	if !strings.Contains(err.Error(), "redirect") {
		t.Errorf("the error no longer says what happened: %s", err)
	}
}

// And the non-redirect branch, which has always scrubbed — so a change to the
// shared helper cannot quietly break it.
func TestWebhookBodyErrorIsScrubbed(t *testing.T) {
	const token = "s3cr3t-bearer-token"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "rejected request with headers: Authorization: %s", r.Header.Get("Authorization"))
	}))
	defer srv.Close()

	sink := WebhookSink{Config: WebhookConfig{URL: srv.URL, Token: token}}
	err := sink.Send(events.Event{ID: "e1", Summary: "x"}, "s", "b")
	if err == nil {
		t.Fatal("a 500 was accepted")
	}
	if strings.Contains(err.Error(), token) {
		t.Errorf("the token reached the error:\n%s", err)
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("the status was lost: %s", err)
	}
}

// The email sink had no scrub at all, and the password is in the AUTH
// exchange.
// The email sink had no scrub at all, and the password is in the AUTH
// exchange. A server that quotes the AUTH line back in its refusal — which is
// entirely its choice — put it in monitoring.db.
func TestEmailErrorIsScrubbed(t *testing.T) {
	const password = "hunter2-the-smtp-password"

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("no socket")
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		w := bufio.NewWriter(conn)
		say := func(s string) { w.WriteString(s + "\r\n"); w.Flush() }

		say("220 test ESMTP")
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			cmd := strings.ToUpper(strings.TrimSpace(line))
			switch {
			case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
				say("250-test")
				say("250 AUTH PLAIN LOGIN")
			case strings.HasPrefix(cmd, "AUTH"):
				// Refuse, and quote back exactly what was sent.
				say("535 5.7.8 rejected credentials: " + strings.TrimSpace(line))
			case strings.HasPrefix(cmd, "QUIT"):
				say("221 bye")
				return
			default:
				say("250 ok")
			}
		}
	}()

	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)

	sink := EmailSink{Config: EmailConfig{
		Host: host, Port: port,
		From: "a@example.com", To: []string{"b@example.com"},
		Username: "user", Password: password, AuthMethod: "plain",
		Encryption: "none",
	}}

	err = sink.Send(events.Event{ID: "e1", Summary: "x"}, "s", "b")
	if err == nil {
		t.Fatal("the auth refusal was not reported")
	}
	// The AUTH PLAIN payload is base64, so the password is not literal on the
	// wire — but a server can echo anything, and the guarantee has to hold for
	// the case where it does.
	if strings.Contains(err.Error(), password) {
		t.Errorf("the SMTP password reached the error, and from there monitoring.db:\n%s", err)
	}

	// And in its BASE64 form, which is how SMTP actually sends it — a plain
	// search walks straight past that. Measured before the fix, the error read
	// "AUTH PLAIN AHVzZXIAaHVudGVyMi10aGUtc210cC1wYXNzd29yZA==", which decodes
	// to the password.
	for _, form := range secretForms(password) {
		if strings.Contains(err.Error(), form) {
			t.Errorf("a recoverable form of the password survived (%q):\n%s", form, err)
		}
	}

	// And nothing left in the text may decode to it either.
	for _, field := range strings.Fields(err.Error()) {
		trimmed := strings.Trim(field, `"`)
		if len(trimmed) < 8 {
			continue
		}
		if decoded, decErr := base64.StdEncoding.DecodeString(trimmed); decErr == nil {
			if strings.Contains(string(decoded), password) {
				t.Errorf("a base64 field decodes to the password: %q", trimmed)
			}
		}
	}
	t.Logf("error text: %s", err)
}

// The guarantee where it is literal: whatever the server says comes back
// scrubbed.
func TestEmailScrubsWhateverTheServerEchoes(t *testing.T) {
	const password = "hunter2-the-smtp-password"
	sink := EmailSink{Config: EmailConfig{Password: password}}
	raw := fmt.Errorf("smtp auth: 535 rejected %s for user", password)
	got := scrubSecret(raw, sink.Config.Password)
	if strings.Contains(got.Error(), password) {
		t.Errorf("not scrubbed: %s", got)
	}
	if !strings.Contains(got.Error(), "[redacted]") {
		t.Errorf("nothing marked: %s", got)
	}
}
