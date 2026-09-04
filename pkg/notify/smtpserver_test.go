package notify

import (
	"bufio"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
)

// A minimal SMTP server, enough to hold a real conversation with net/smtp.
//
// The email sink previously had two tests, neither of which sent a byte: one
// checked a guard in isolation, the other checked string formatting. Nothing
// exercised the actual exchange, and nothing exercised encryption at all —
// which for the one sink that carries a password is the wrong place to be
// taking things on trust. This server makes the whole path observable:
// what was negotiated, what credentials were presented, and over what.

type smtpSession struct {
	// STARTTLSUsed and ImplicitTLS record how the connection was protected.
	STARTTLSUsed bool
	ImplicitTLS  bool
	// AuthMechanism is PLAIN, LOGIN or empty.
	AuthMechanism string
	Username      string
	Password      string
	// AuthOverPlaintext records whether credentials arrived before any TLS.
	// It must never be true.
	AuthOverPlaintext bool
	From              string
	Rcpt              []string
	// Data is the raw wire form, still dot-stuffed.
	Data string
	// Delivered is what a mailbox actually holds: RFC 5321 4.5.2 says the
	// receiver removes the leading dot from any line that begins with one.
	// Asserting on Data alone cannot see a body that was stuffed twice.
	Delivered string
}

// unstuff is the receiver half of RFC 5321 4.5.2.
func unstuff(raw string) string {
	lines := strings.Split(raw, "\r\n")
	for i, l := range lines {
		lines[i] = strings.TrimPrefix(l, ".")
	}
	return strings.Join(lines, "\r\n")
}

type smtpServer struct {
	mu       sync.Mutex
	sessions []smtpSession
	done     chan struct{}

	// offerSTARTTLS can be turned off to test a relay that does not support it.
	offerSTARTTLS bool
	// implicit runs TLS from the first byte, as port 465 does.
	implicit bool
	// rejectRcpt maps a recipient address to the reply it gets instead of
	// "250 ok" — a mailbox that no longer exists, a relay refusing to accept
	// for that domain. Empty means every recipient is accepted.
	rejectRcpt map[string]string
	// authReply overrides the reply to AUTH. Empty means "235 authenticated".
	authReply string
	// quitReply overrides the reply to QUIT; "drop" closes the socket without
	// answering. The message has already been accepted by then.
	quitReply string
	tlsCfg    *tls.Config
	ln        net.Listener
}

func newSMTPServer(t *testing.T, certPEM, keyPEM []byte, implicit, offerSTARTTLS bool) *smtpServer {
	t.Helper()

	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("server keypair: %v", err)
	}
	cfg := &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12}

	s := &smtpServer{
		done:          make(chan struct{}),
		offerSTARTTLS: offerSTARTTLS,
		implicit:      implicit,
		tlsCfg:        cfg,
	}

	if implicit {
		s.ln, err = tls.Listen("tcp", "127.0.0.1:0", cfg)
	} else {
		s.ln, err = net.Listen("tcp", "127.0.0.1:0")
	}
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { s.ln.Close() })

	go s.acceptLoop()
	return s
}

func (s *smtpServer) addr() (host, port string) {
	h, p, _ := net.SplitHostPort(s.ln.Addr().String())
	return h, p
}

func (s *smtpServer) acceptLoop() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

// waitSession blocks until a conversation has finished, or fails the test.
func (s *smtpServer) waitSession(t *testing.T) smtpSession {
	t.Helper()
	select {
	case <-s.done:
	case <-timeoutAfter():
		t.Fatal("no SMTP conversation completed")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.sessions) == 0 {
		t.Fatal("no session recorded")
	}
	return s.sessions[len(s.sessions)-1]
}

func (s *smtpServer) finish(sess smtpSession) {
	s.mu.Lock()
	s.sessions = append(s.sessions, sess)
	s.mu.Unlock()
	select {
	case <-s.done:
	default:
		close(s.done)
	}
}

func (s *smtpServer) handle(conn net.Conn) {
	defer conn.Close()

	sess := smtpSession{ImplicitTLS: s.implicit}
	secured := s.implicit
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)

	say := func(format string, a ...interface{}) {
		fmt.Fprintf(w, format+"\r\n", a...)
		w.Flush()
	}

	say("220 test.smtp ESMTP ready")

	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		upper := strings.ToUpper(line)

		switch {
		case strings.HasPrefix(upper, "EHLO"), strings.HasPrefix(upper, "HELO"):
			// The extension list drives what net/smtp will attempt.
			say("250-test.smtp")
			if s.offerSTARTTLS && !secured {
				say("250-STARTTLS")
			}
			say("250 AUTH PLAIN LOGIN")

		case strings.HasPrefix(upper, "STARTTLS"):
			say("220 go ahead")
			tlsConn := tls.Server(conn, s.tlsCfg)
			if err := tlsConn.Handshake(); err != nil {
				return
			}
			conn = tlsConn
			r = bufio.NewReader(conn)
			w = bufio.NewWriter(conn)
			say = func(format string, a ...interface{}) {
				fmt.Fprintf(w, format+"\r\n", a...)
				w.Flush()
			}
			secured = true
			sess.STARTTLSUsed = true

		case strings.HasPrefix(upper, "AUTH PLAIN"):
			sess.AuthMechanism = "PLAIN"
			sess.AuthOverPlaintext = !secured
			payload := strings.TrimSpace(line[len("AUTH PLAIN"):])
			if payload == "" {
				say("334 ")
				payload, _ = r.ReadString('\n')
				payload = strings.TrimSpace(payload)
			}
			if raw, err := base64.StdEncoding.DecodeString(payload); err == nil {
				// \0user\0pass
				parts := strings.Split(string(raw), "\x00")
				if len(parts) == 3 {
					sess.Username, sess.Password = parts[1], parts[2]
				}
			}
			if s.authReply != "" {
				say(s.authReply)
				break
			}
			say("235 authenticated")

		case strings.HasPrefix(upper, "AUTH LOGIN"):
			sess.AuthMechanism = "LOGIN"
			sess.AuthOverPlaintext = !secured
			// The challenges are base64; net/smtp decodes them before handing
			// them to the Auth implementation.
			say("334 %s", base64.StdEncoding.EncodeToString([]byte("Username:")))
			u, _ := r.ReadString('\n')
			if raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(u)); err == nil {
				sess.Username = string(raw)
			}
			say("334 %s", base64.StdEncoding.EncodeToString([]byte("Password:")))
			p, _ := r.ReadString('\n')
			if raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(p)); err == nil {
				sess.Password = string(raw)
			}
			say("235 authenticated")

		case strings.HasPrefix(upper, "MAIL FROM"):
			sess.From = extractAddress(line)
			say("250 ok")

		case strings.HasPrefix(upper, "RCPT TO"):
			addr := extractAddress(line)
			if reply, refused := s.rejectRcpt[addr]; refused {
				say(reply)
				break
			}
			sess.Rcpt = append(sess.Rcpt, addr)
			say("250 ok")

		case strings.HasPrefix(upper, "DATA"):
			say("354 end with <CRLF>.<CRLF>")
			var b strings.Builder
			for {
				dl, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if dl == ".\r\n" || dl == ".\n" {
					break
				}
				b.WriteString(dl)
			}
			sess.Data = b.String()
			sess.Delivered = unstuff(b.String())
			say("250 queued")

		case strings.HasPrefix(upper, "QUIT"):
			switch s.quitReply {
			case "":
				say("221 bye")
			case "drop":
				s.finish(sess)
				return
			default:
				say(s.quitReply)
			}
			s.finish(sess)
			return

		case strings.HasPrefix(upper, "RSET"), strings.HasPrefix(upper, "NOOP"):
			say("250 ok")

		default:
			say("500 unrecognised")
		}
	}
}

func extractAddress(line string) string {
	start := strings.Index(line, "<")
	end := strings.Index(line, ">")
	if start >= 0 && end > start {
		return line[start+1 : end]
	}
	if i := strings.Index(line, ":"); i >= 0 {
		return strings.TrimSpace(line[i+1:])
	}
	return ""
}
