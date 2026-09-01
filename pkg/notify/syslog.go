package notify

import (
	"crypto/tls"
	"fmt"
	"net"
	"strings"
	"time"

	"SnmpLens/pkg/events"
)

// log/syslog is unavailable on Windows (//go:build !windows && !plan9), and the
// popular wrappers either fail to build there or — worse — compile cleanly and
// then return "Platform unsupported" at runtime, so CI stays green and delivery
// silently never happens. This is a small, explicit RFC5424 encoder over a
// plain net.Conn instead: no build tags, no dependency, same behaviour on all
// three targets.

const (
	// NILVALUE for any header field we cannot fill.
	syslogNil = "-"
	// The BOM that tells a collector the message is UTF-8.
	utf8BOM = "\xEF\xBB\xBF"
)

// SyslogConfig describes one syslog destination.
type SyslogConfig struct {
	Address  string `json:"address"`  // host:port; the port may be omitted for TLS
	Protocol string `json:"protocol"` // "udp", "tcp" or "tls" (RFC5425)
	Facility int    `json:"facility"` // 0-23; 16 (local0) is the usual choice
	Hostname string `json:"hostname"` // ours; empty means ask the OS
	AppName  string `json:"appName"`  // defaults to SnmpLens
	Timeout  int    `json:"timeout"`  // seconds; 0 means 5

	// --- TLS, used when Protocol is "tls" ---

	// CACert is a PEM bundle trusting a private CA. Empty means the system
	// trust store, which is right for a publicly-signed collector.
	CACert string `json:"caCert,omitempty"`
	// ServerName overrides the name checked against the certificate, for a
	// collector reached by IP or through a load balancer.
	ServerName string `json:"serverName,omitempty"`
	// InsecureSkipVerify disables certificate verification entirely. It is
	// offered because a lab collector with a self-signed certificate is a real
	// situation, and named so nobody can enable it by accident.
	InsecureSkipVerify bool `json:"insecureSkipVerify,omitempty"`
	// ClientCert is the PEM certificate for mutual TLS. It is public and lives
	// with the config; its private key is a credential and travels through
	// SinkConfig.Secret into pkg/secrets, hence json:"-" below.
	ClientCert string `json:"clientCert,omitempty"`
	// ClientKey is the PEM private key, supplied at Build time from secure
	// storage and never serialised. See EmailConfig.Password for the reasoning.
	ClientKey string `json:"-"`
}

// sanitizePrintUSASCII enforces the header grammar: printable US-ASCII with no
// spaces. A hostname with a space in it would silently shift every later field.
func sanitizePrintUSASCII(s string, max int) string {
	if s == "" {
		return syslogNil
	}
	var b strings.Builder
	for _, r := range s {
		if r < 33 || r > 126 {
			continue
		}
		b.WriteRune(r)
		if b.Len() >= max {
			break
		}
	}
	if b.Len() == 0 {
		return syslogNil
	}
	return b.String()
}

// FormatRFC5424 renders one event as a syslog line.
//
// <PRI>1 TIMESTAMP HOSTNAME APP-NAME PROCID MSGID STRUCTURED-DATA MSG
//
// MSGID carries the event kind and the structured data carries the event UUID,
// so a collector can deduplicate across retries: delivery is at-least-once.
func FormatRFC5424(cfg SyslogConfig, e events.Event, message string) string {
	facility := cfg.Facility
	if facility < 0 || facility > 23 {
		facility = 16 // local0
	}
	severity := events.ParseSeverity(e.Severity).Syslog()
	pri := facility*8 + severity

	hostname := cfg.Hostname
	if hostname == "" {
		hostname, _ = osHostname()
	}
	appName := cfg.AppName
	if appName == "" {
		appName = "SnmpLens"
	}

	// RFC5424 wants its own timestamp shape; fall back to now if the event's is
	// unparseable, because a malformed header would break the whole line.
	stamp := time.Now().UTC()
	if parsed, err := time.Parse(time.RFC3339, e.Ts); err == nil {
		stamp = parsed.UTC()
	}
	ts := stamp.Format("2006-01-02T15:04:05.000000Z")

	// Structured data: PARAM-VALUE escapes ", \ and ].
	esc := func(v string) string {
		r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `]`, `\]`)
		return r.Replace(v)
	}
	sd := fmt.Sprintf(`[snmplens@0 id="%s" category="%s" severity="%s" state="%s"`,
		esc(e.ID), esc(e.Category), esc(e.Severity), esc(e.State))
	if e.Source != "" {
		sd += fmt.Sprintf(` source="%s"`, esc(e.Source))
	}
	if e.OID != "" {
		sd += fmt.Sprintf(` oid="%s"`, esc(e.OID))
	}
	sd += "]"

	if message == "" {
		message = e.Summary
	}

	return fmt.Sprintf("<%d>1 %s %s %s %s %s %s%s%s",
		pri, ts,
		sanitizePrintUSASCII(hostname, 255),
		sanitizePrintUSASCII(appName, 48),
		syslogNil, // PROCID
		sanitizePrintUSASCII(e.Kind, 32),
		sd,
		" "+utf8BOM,
		message,
	)
}

// SyslogSink delivers to a syslog collector.
type SyslogSink struct {
	Config SyslogConfig
}

// Send writes one message. A new connection per message keeps the sink
// stateless: a desktop app can be suspended, moved between networks or offline
// for hours, and a long-lived socket would simply be dead by then.
func (s SyslogSink) Send(e events.Event, subject, body string) error {
	proto := normaliseProtocol(s.Config.Protocol)
	timeout := time.Duration(s.Config.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	address := s.Config.Address
	if proto == SyslogTLS {
		address = withDefaultPort(address, DefaultSyslogTLSPort)
	}

	conn, err := s.dial(proto, address, timeout)
	if err != nil {
		return err
	}
	defer conn.Close()

	conn.SetWriteDeadline(time.Now().Add(timeout))
	line := FormatRFC5424(s.Config, e, body)

	if proto != SyslogUDP {
		// RFC6587 octet counting, which RFC5425 mandates for TLS: without a
		// framing rule a collector cannot tell where one message ends on a
		// stream. The count is in OCTETS, so len() on the byte string — a
		// rune count would desynchronise the collector on any non-ASCII
		// summary, and every one of our five locales can produce one.
		line = fmt.Sprintf("%d %s", len(line), line)
	}
	_, err = conn.Write([]byte(line))
	return err
}

// dial opens the transport.
func (s SyslogSink) dial(proto, address string, timeout time.Duration) (net.Conn, error) {
	if proto != SyslogTLS {
		conn, err := net.DialTimeout(proto, address, timeout)
		if err != nil {
			return nil, fmt.Errorf("dial %s %s: %w", proto, address, err)
		}
		return conn, nil
	}

	tlsCfg, err := tlsConfigFor(s.Config, s.Config.ClientKey)
	if err != nil {
		return nil, fmt.Errorf("syslog TLS configuration: %w", err)
	}
	// Dial and handshake share one deadline: a collector that accepts the
	// connection and then never completes the handshake would otherwise hold
	// the dispatcher goroutine for as long as the OS allows.
	dialer := &net.Dialer{Timeout: timeout}
	conn, err := tls.DialWithDialer(dialer, "tcp", address, tlsCfg)
	if err != nil {
		return nil, fmt.Errorf("dial tls %s: %w", address, err)
	}
	return conn, nil
}

// Describe names the destination for the delivery log.
func (s SyslogSink) Describe() string {
	return "syslog " + normaliseProtocol(s.Config.Protocol) + "://" + s.Config.Address
}
