package notify

import (
	"mime"
	"strings"
	"testing"
)

// What the recipient actually reads. Every case below was measured on the wire
// and then decoded the way a real client decodes it — a test that asserts on
// the raw bytes we sent cannot see a body that was escaped twice or a subject
// that was truncated to nothing.

// RFC 5321 4.5.2: the SENDER adds a dot to any line starting with one, and the
// RECEIVER removes it. net/smtp already does the sender half — client.Data()
// returns a dataCloser wrapping textproto's DotWriter — so doing it again in
// buildMessage put two dots on the wire and left one in the mailbox.
//
// It is not a security property: DotWriter alone is sufficient, and the
// injection test pins that. This is corruption of the body, and it lands on
// exactly the text that comes from outside — a template, or a trap's own
// varbind values.
func TestABodyLineStartingWithADotArrivesUnchanged(t *testing.T) {
	certPEM, keyPEM := selfSigned(t, "localhost")
	srv := newSMTPServer(t, certPEM, keyPEM, true, false)

	cfg := baseConfig(t, srv, certPEM)
	cfg.Encryption = "tls"

	const body = "before\n.\nafter\n.hidden line\n..already doubled\n"
	if err := (EmailSink{Config: cfg}).Send(mailEvent(), "subj", body); err != nil {
		t.Fatal(err)
	}
	sess := srv.waitSession(t)

	_, delivered, ok := strings.Cut(sess.Delivered, "\r\n\r\n")
	if !ok {
		t.Fatalf("no header/body separator in what arrived:\n%q", sess.Delivered)
	}
	want := "before\r\n.\r\nafter\r\n.hidden line\r\n..already doubled\r\n"
	if !strings.HasPrefix(delivered, want) {
		t.Errorf("the body was altered in delivery.\n got: %q\nwant prefix: %q", delivered, want)
	}

	// And the wire form must still be stuffed, or the single "." line would
	// end the message early.
	rawBody := sess.Data[strings.Index(sess.Data, "\r\n\r\n")+4:]
	if !strings.Contains(rawBody, "\r\n..\r\n") {
		t.Errorf("the lone dot was not stuffed on the wire, which would truncate "+
			"the message:\n%q", rawBody)
	}
}

// A subject is capped so it cannot break the header line. Capping it must leave
// as much of the subject as fits — a non-ASCII summary over ~300 characters
// arrived as a bare ellipsis: 21 octets where 900 were available.
//
// Measured before the fix (accented characters in, runes delivered out):
// 100->118, 150->113, 200->72, 250->31, 300->1, 400->1, 2000->1. The fast
// converge computed drop = over/4 with no overshoot check, and the loop exited
// the moment the encoded form fitted, so one oversized cut was never walked
// back.
func TestALongNonASCIISubjectKeepsWhatFits(t *testing.T) {
	dec := new(mime.WordDecoder)

	for _, n := range []int{50, 100, 150, 200, 250, 300, 400, 2000} {
		subject := strings.Repeat("é", n)
		encoded := capEncodedSubject(subject)

		if len(encoded) > maxSubjectOctets {
			t.Errorf("%d characters encoded to %d octets, over the %d cap",
				n, len(encoded), maxSubjectOctets)
		}
		got, err := dec.DecodeHeader(encoded)
		if err != nil {
			t.Fatalf("%d characters: the subject does not decode: %v", n, err)
		}

		runes := len([]rune(got))
		if n <= 100 {
			// Short enough to survive whole.
			if got != subject {
				t.Errorf("%d characters were altered: %d runes delivered", n, runes)
			}
			continue
		}
		// Long enough to be cut, but the cut must use the budget. Each "é" is
		// six octets encoded (=C3=A9), so ~140 runes is what fits.
		if runes < 100 {
			t.Errorf("%d characters collapsed to %d runes (%d octets) — the %d-octet "+
				"budget carries far more", n, runes, len(encoded), maxSubjectOctets)
		}
		if !strings.HasPrefix(got, "éé") {
			t.Errorf("%d characters: the delivered subject does not start with the "+
				"original text: %q", n, got)
		}
	}
}

// ASCII was never affected and must stay that way.
func TestALongASCIISubjectIsStillCapped(t *testing.T) {
	dec := new(mime.WordDecoder)
	encoded := capEncodedSubject(strings.Repeat("a", 5000))
	if len(encoded) > maxSubjectOctets {
		t.Fatalf("%d octets, over the cap", len(encoded))
	}
	got, err := dec.DecodeHeader(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if len([]rune(got)) < 500 {
		t.Errorf("an ASCII subject was cut to %d runes", len([]rune(got)))
	}
}

// RFC 5322 2.1.1 caps a line at 998 octets. A large on-call rota in To: went
// out as one line and broke it — some relays reject the message, others fold it
// themselves in a place of their choosing.
func TestALargeRecipientListIsFolded(t *testing.T) {
	var to []string
	for i := 0; i < 60; i++ {
		to = append(to, "operator-with-a-long-name-"+strings.Repeat("x", 10)+string(rune('a'+i%26))+"@example.com")
	}
	msg := buildMessage(EmailConfig{From: "a@b", To: to}, mailEvent(), "s", "b")

	header, _, _ := strings.Cut(msg, "\r\n\r\n")
	for _, line := range strings.Split(header, "\r\n") {
		if len(line) > 998 {
			t.Errorf("a header line is %d octets, over the RFC 5322 limit of 998:\n%.120s…",
				len(line), line)
		}
	}
	// Every recipient must still be in there, and the header must still parse
	// as one field: a continuation line starts with whitespace.
	for _, addr := range to {
		if !strings.Contains(msg, addr) {
			t.Errorf("%s was dropped from the To header", addr)
		}
	}
	for _, line := range strings.Split(header, "\r\n") {
		if strings.HasPrefix(line, "@") || strings.HasPrefix(line, ",") {
			t.Errorf("a folded line does not begin with whitespace, so it reads as a "+
				"new header: %.80s", line)
		}
	}
}

// A message the relay has already accepted must not be reported as failed.
//
// w.Close() is what reads the 250 after the terminating dot; past that the relay
// owns the message. `return client.Quit()` then turned any QUIT problem into a
// Send failure — the relay dropping the socket, answering 500, or simply the
// last of the connection deadline being spent on a slow DATA acknowledgement.
// The delivery was retried, and the operator got the alert twice.
func TestAQuitFailureDoesNotResendAnAcceptedMessage(t *testing.T) {
	for _, quit := range []string{"drop", "500 go away"} {
		certPEM, keyPEM := selfSigned(t, "localhost")
		srv := newSMTPServer(t, certPEM, keyPEM, true, false)
		srv.quitReply = quit

		cfg := baseConfig(t, srv, certPEM)
		cfg.Encryption = "tls"

		if err := (EmailSink{Config: cfg}).Send(mailEvent(), "subj", "body"); err != nil {
			t.Errorf("QUIT %q turned an accepted message into a failure, so the retry "+
				"delivers a second copy: %v", quit, err)
			continue
		}
		sess := srv.waitSession(t)
		if sess.Data == "" {
			t.Errorf("QUIT %q: the message never reached the relay, so reporting "+
				"success is wrong", quit)
		}
	}
}

// A failure BEFORE the relay accepts the message is still a failure — the
// message did not arrive, and it must be retried.
func TestAFailureBeforeAcceptanceIsStillReported(t *testing.T) {
	certPEM, keyPEM := selfSigned(t, "localhost")
	srv := newSMTPServer(t, certPEM, keyPEM, true, false)
	srv.rejectRcpt = map[string]string{"noc@example.com": "550 5.1.1 rejected"}

	cfg := baseConfig(t, srv, certPEM)
	cfg.Encryption = "tls"
	cfg.To = []string{"noc@example.com"}

	if err := (EmailSink{Config: cfg}).Send(mailEvent(), "subj", "body"); err == nil {
		t.Error("a message that was never accepted was reported as sent")
	}
}
