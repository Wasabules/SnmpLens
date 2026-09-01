package notify

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"strings"
	"time"

	"SnmpLens/pkg/events"
)

// EmailConfig describes an SMTP destination.
type EmailConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	// Password is json:"-" ON PURPOSE. SaveSink marshals the whole SinkConfig
	// into the notify_sinks row, so any serialisable field lands in
	// monitoring.db in the clear. The credential travels in SinkConfig.Secret
	// (write-only) and is stored by pkg/secrets instead.
	Password string   `json:"-"`
	From     string   `json:"from"`
	To       []string `json:"to"`
	// Encryption: "starttls" (587, the usual), "tls" (465, implicit) or "none".
	Encryption string `json:"encryption"`
	// AuthMethod: "plain", "login" (on-prem Exchange) or "none".
	AuthMethod string `json:"authMethod"`
	// CACert is a PEM bundle trusting a private CA, which is what an internal
	// relay actually needs. Without it the only way to reach such a relay was
	// to disable verification altogether — the insecure setting, for a
	// situation that has a secure answer.
	CACert string `json:"caCert,omitempty"`
	// ServerName overrides the name checked against the certificate, for a
	// relay reached by IP or through a load balancer.
	ServerName         string `json:"serverName,omitempty"`
	InsecureSkipVerify bool   `json:"insecureSkipVerify"`
	Timeout            int    `json:"timeout"` // seconds; 0 means 20
}

// tlsConfig builds the client TLS settings for the relay.
func (cfg EmailConfig) tlsConfig() (*tls.Config, error) {
	serverName := strings.TrimSpace(cfg.ServerName)
	if serverName == "" {
		serverName = cfg.Host
	}
	out := &tls.Config{
		ServerName:         serverName,
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: cfg.InsecureSkipVerify, // #nosec G402 -- opt-in and named for what it does
	}
	if ca := strings.TrimSpace(cfg.CACert); ca != "" {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM([]byte(ca)) {
			return nil, fmt.Errorf("the CA certificate is not valid PEM")
		}
		out.RootCAs = pool
	}
	return out, nil
}

// loginAuth implements the non-standard but widely required AUTH LOGIN, which
// stdlib net/smtp does not provide and which on-prem Exchange commonly demands.
type loginAuth struct {
	username, password string
	host               string
	tlsOK              bool
}

func (a *loginAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	// Never hand credentials to an unauthenticated or plaintext server.
	if !server.TLS && !a.tlsOK {
		return "", nil, errors.New("refusing to send AUTH LOGIN credentials over an unencrypted connection")
	}
	if server.Name != a.host {
		return "", nil, errors.New("smtp: server name does not match the configured host")
	}
	return "LOGIN", nil, nil
}

func (a *loginAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	switch strings.ToLower(strings.TrimSpace(string(fromServer))) {
	case "username:":
		return []byte(a.username), nil
	case "password:":
		return []byte(a.password), nil
	default:
		return nil, fmt.Errorf("smtp: unexpected AUTH LOGIN challenge %q", fromServer)
	}
}

// EmailSink delivers by SMTP.
//
// It deliberately dials with an explicit timeout and sets deadlines on the
// connection before handing it to net/smtp: the stdlib client has no context
// support, so a relay that accepts a connection and then goes silent would
// otherwise block the dispatcher goroutine for as long as the OS allows.
type EmailSink struct {
	Config EmailConfig
}

// Send delivers one message.
func (m EmailSink) Send(e events.Event, subject, body string) error {
	cfg := m.Config
	if cfg.Host == "" || cfg.From == "" || len(cfg.To) == 0 {
		return fmt.Errorf("email sink is incomplete (host, from and at least one recipient are required)")
	}
	port := cfg.Port
	if port == 0 {
		port = 587
	}
	timeout := time.Duration(cfg.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 20 * time.Second
	}

	addr := net.JoinHostPort(cfg.Host, fmt.Sprint(port))
	encryption := strings.ToLower(strings.TrimSpace(cfg.Encryption))

	tlsConfig, err := cfg.tlsConfig()
	if err != nil {
		return err
	}

	var conn net.Conn
	dialer := &net.Dialer{Timeout: timeout}
	if encryption == "tls" {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, tlsConfig)
	} else {
		conn, err = dialer.Dial("tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("connect to %s: %w", addr, err)
	}
	defer conn.Close()
	// The deadline that net/smtp cannot give us.
	deadline := time.Now().Add(timeout)
	conn.SetDeadline(deadline)

	client, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		return fmt.Errorf("smtp handshake: %w", err)
	}
	defer client.Close()

	usingTLS := encryption == "tls"
	if encryption == "starttls" || encryption == "" {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(tlsConfig); err != nil {
				return fmt.Errorf("starttls: %w", err)
			}
			usingTLS = true
		} else if encryption == "starttls" {
			return fmt.Errorf("server does not offer STARTTLS")
		}
	}

	switch strings.ToLower(strings.TrimSpace(cfg.AuthMethod)) {
	case "", "plain":
		if cfg.Username != "" {
			auth := smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
			if err := client.Auth(auth); err != nil {
				return fmt.Errorf("smtp auth: %w", err)
			}
		}
	case "login":
		auth := &loginAuth{username: cfg.Username, password: cfg.Password, host: cfg.Host, tlsOK: usingTLS}
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth (login): %w", err)
		}
	case "none":
		// no authentication
	default:
		return fmt.Errorf("unknown SMTP auth method %q", cfg.AuthMethod)
	}

	if err := client.Mail(cfg.From); err != nil {
		return fmt.Errorf("MAIL FROM: %w", err)
	}
	for _, to := range cfg.To {
		if err := client.Rcpt(strings.TrimSpace(to)); err != nil {
			return fmt.Errorf("RCPT TO %s: %w", to, err)
		}
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("DATA: %w", err)
	}
	if _, err := w.Write([]byte(buildMessage(cfg, e, subject, body))); err != nil {
		w.Close()
		return fmt.Errorf("write message: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("close message: %w", err)
	}
	return client.Quit()
}

// buildMessage renders the RFC5322 message. The subject is MIME-encoded so a
// non-ASCII summary does not arrive as mojibake.
func buildMessage(cfg EmailConfig, e events.Event, subject, body string) string {
	if subject == "" {
		subject = "[SnmpLens] " + e.Summary
	}
	if body == "" {
		body = e.Summary
	}

	var b strings.Builder
	b.WriteString("From: " + headerValue(cfg.From) + "\r\n")
	b.WriteString("To: " + headerValue(strings.Join(cfg.To, ", ")) + "\r\n")
	// QEncoding encodes every byte below 0x20, so a CR or LF in the subject
	// becomes =0D=0A rather than starting a new header. That is what stops an
	// event summary from injecting a Bcc, so it is pinned by a test: the
	// safety of this line lives in the standard library, not here.
	b.WriteString("Subject: " + capEncodedSubject(subject) + "\r\n")
	b.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	// A stable id so a mail client threads retries instead of showing copies.
	if e.ID != "" {
		b.WriteString("Message-ID: <" + headerValue(e.ID) + "@snmplens>\r\n")
		b.WriteString("X-SnmpLens-Event-Id: " + headerValue(e.ID) + "\r\n")
	}
	b.WriteString("\r\n")
	b.WriteString(dotStuff(body))
	b.WriteString("\r\n")
	return b.String()
}

// maxSubjectOctets leaves headroom under the RFC5322 998-octet line limit,
// after "Subject: " and the trailing CRLF.
const maxSubjectOctets = 900

// capEncodedSubject MIME-encodes the subject and keeps it to one legal line.
//
// The cap is measured on the ENCODED form because that is what goes on the
// wire, and the two lengths are not proportional: Q-encoding expands non-ASCII
// text by up to three times. It matters too that Go joins its encoded-words
// with a plain space and never a CRLF, so an over-long subject becomes one very
// long line rather than a folded one, and nothing downstream would bring it
// back under the limit.
func capEncodedSubject(subject string) string {
	encoded := mime.QEncoding.Encode("utf-8", subject)
	if len(encoded) <= maxSubjectOctets {
		return encoded
	}
	runes := []rune(subject)
	for len(runes) > 0 && len(encoded) > maxSubjectOctets {
		drop := 1
		if over := len(encoded) - maxSubjectOctets; over > 64 {
			drop = over / 4 // converge fast, then settle a rune at a time
		}
		if drop > len(runes) {
			drop = len(runes)
		}
		runes = runes[:len(runes)-drop]
		encoded = mime.QEncoding.Encode("utf-8", string(runes)+"…")
	}
	return encoded
}

// headerValue strips CR and LF from a header value.
//
// The subject goes through MIME encoding, which handles this, but these fields
// do not: a sender address pasted with a stray newline would end the header and
// let whatever followed be read as one of its own.
func headerValue(s string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(s)
}

// dotStuff prepares the body for the DATA phase.
//
// Two things have to be right, and only one of them used to be. First the body
// must use CRLF: Render emits bare LFs, so the previous rule — replacing the
// exact sequence "\r\n.\r\n" — never matched anything it produced. Second,
// RFC5321 section 4.5.2 requires an extra dot on EVERY line starting with one,
// not only on a line that is nothing but a dot.
//
// This matters because event text is not all ours. A trap arrives from the
// network unauthenticated, and its trap OID value reaches the rendered body; a
// line beginning with a dot in it would end DATA early, truncating the alert
// and handing the remainder to the relay as SMTP commands — through a
// connection this application has already authenticated.
func dotStuff(body string) string {
	// Normalise to CRLF without doubling the ones already there.
	body = strings.ReplaceAll(body, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\r", "\n")

	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, ".") {
			lines[i] = "." + line
		}
	}
	return strings.Join(lines, "\r\n")
}

// Describe names the destination for the delivery log.
func (m EmailSink) Describe() string {
	return "email " + m.Config.Host + " -> " + strings.Join(m.Config.To, ",")
}
