package notify

import (
	"strings"
	"testing"
)

// Dot-stuffing is net/smtp's: client.Data() returns a dataCloser wrapping
// textproto's DotWriter, which escapes a leading dot and converts a lone LF to
// CRLF. These tests drive a real connection because that is the only layer
// where the property holds — asserting on buildMessage's output tested code
// that does not do this, and went on passing while the message on the wire was
// escaped twice.

func wireBody(t *testing.T, sess smtpSession) string {
	t.Helper()
	i := strings.Index(sess.Data, "\r\n\r\n")
	if i < 0 {
		t.Fatalf("no header/body separator on the wire:\n%q", sess.Data)
	}
	return sess.Data[i+4:]
}

func deliveredBody(t *testing.T, sess smtpSession) string {
	t.Helper()
	i := strings.Index(sess.Delivered, "\r\n\r\n")
	if i < 0 {
		t.Fatalf("no header/body separator in what arrived:\n%q", sess.Delivered)
	}
	return sess.Delivered[i+4:]
}

// A line beginning with a dot must go out stuffed, or it ends the DATA phase
// and the relay reads the rest of the message as SMTP commands.
func TestMailBodyCannotEndDataEarly(t *testing.T) {
	hostile := "x\n.\nMAIL FROM:<spoofed@example.com>\nRCPT TO:<victim@example.com>\nDATA\nspam"
	e := hostileEvent(hostile)
	_, body := Render(e, false)

	certPEM, keyPEM := selfSigned(t, "localhost")
	srv := newSMTPServer(t, certPEM, keyPEM, true, false)
	cfg := baseConfig(t, srv, certPEM)
	cfg.Encryption = "tls"

	if err := (EmailSink{Config: cfg}).Send(e, "subject", body); err != nil {
		t.Fatal(err)
	}
	sess := srv.waitSession(t)
	wire := wireBody(t, sess)

	for _, line := range strings.Split(wire, "\r\n") {
		if line == "." {
			t.Fatalf("an unstuffed bare dot went out on the wire and would have ended "+
				"DATA there:\n%q", wire)
		}
	}
	// Bare LFs would let a lenient relay find the dot line anyway.
	if strings.Contains(strings.ReplaceAll(wire, "\r\n", ""), "\n") {
		t.Errorf("the body went out with bare LF line endings:\n%q", wire)
	}
	// Escaped, not deleted: the recipient still reads what the event said.
	if !strings.Contains(deliveredBody(t, sess), "MAIL FROM:<spoofed@example.com>") {
		t.Error("the body was mangled rather than escaped")
	}
}

// RFC 5321 4.5.2 is about any line STARTING with a dot, not only a lone dot —
// and the receiver removes exactly one, so what the mailbox holds must equal
// what was sent. Stuffing it a second time ourselves left the extra dot there.
func TestEveryLeadingDotIsStuffedExactlyOnce(t *testing.T) {
	certPEM, keyPEM := selfSigned(t, "localhost")
	srv := newSMTPServer(t, certPEM, keyPEM, true, false)
	cfg := baseConfig(t, srv, certPEM)
	cfg.Encryption = "tls"

	const body = ".hidden\r\n..already\r\nnormal\r\n.\r\n"
	if err := (EmailSink{Config: cfg}).Send(mailEvent(), "subject", body); err != nil {
		t.Fatal(err)
	}
	sess := srv.waitSession(t)

	for _, line := range strings.Split(wireBody(t, sess), "\r\n") {
		if line == "" || line == "normal" {
			continue
		}
		if !strings.HasPrefix(line, "..") {
			t.Errorf("line %q went out unstuffed", line)
		}
	}
	if got := deliveredBody(t, sess); !strings.HasPrefix(got, body) {
		t.Errorf("what the mailbox holds is not what was sent.\n got: %q\nwant prefix: %q",
			got, body)
	}
}

// Line endings are normalised before the body is handed over, so a body built
// from a template with LF endings and one pasted with CRLF go out the same.
func TestBodyLineEndingsAreNormalised(t *testing.T) {
	for _, in := range []string{"a\nb", "a\r\nb", "a\rb"} {
		if out := normaliseLines(in); out != "a\r\nb" {
			t.Errorf("normaliseLines(%q) = %q, want CRLF", in, out)
		}
	}
}
