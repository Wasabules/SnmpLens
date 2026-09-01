package notify

import (
	"crypto/tls"
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
	AuthMethod         string `json:"authMethod"`
	InsecureSkipVerify bool   `json:"insecureSkipVerify"`
	Timeout            int    `json:"timeout"` // seconds; 0 means 20
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

	tlsConfig := &tls.Config{
		ServerName:         cfg.Host,
		InsecureSkipVerify: cfg.InsecureSkipVerify, // #nosec G402 -- opt-in, for private CAs
		MinVersion:         tls.VersionTLS12,
	}

	var conn net.Conn
	var err error
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
	b.WriteString("From: " + cfg.From + "\r\n")
	b.WriteString("To: " + strings.Join(cfg.To, ", ") + "\r\n")
	b.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", subject) + "\r\n")
	b.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	// A stable id so a mail client threads retries instead of showing copies.
	if e.ID != "" {
		b.WriteString("Message-ID: <" + e.ID + "@snmplens>\r\n")
		b.WriteString("X-SnmpLens-Event-Id: " + e.ID + "\r\n")
	}
	b.WriteString("\r\n")
	// Dot-stuffing: a line consisting of a single dot would end the DATA phase.
	b.WriteString(strings.ReplaceAll(body, "\r\n.\r\n", "\r\n..\r\n"))
	b.WriteString("\r\n")
	return b.String()
}

// Describe names the destination for the delivery log.
func (m EmailSink) Describe() string {
	return "email " + m.Config.Host + " -> " + strings.Join(m.Config.To, ",")
}
