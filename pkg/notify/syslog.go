package notify

import (
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
	Address  string `json:"address"`  // host:port
	Protocol string `json:"protocol"` // "udp" or "tcp"
	Facility int    `json:"facility"` // 0-23; 16 (local0) is the usual choice
	Hostname string `json:"hostname"` // ours; empty means ask the OS
	AppName  string `json:"appName"`  // defaults to SnmpLens
	Timeout  int    `json:"timeout"`  // seconds; 0 means 5
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
	proto := strings.ToLower(strings.TrimSpace(s.Config.Protocol))
	if proto != "tcp" {
		proto = "udp"
	}
	timeout := time.Duration(s.Config.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	conn, err := net.DialTimeout(proto, s.Config.Address, timeout)
	if err != nil {
		return fmt.Errorf("dial %s %s: %w", proto, s.Config.Address, err)
	}
	defer conn.Close()

	conn.SetWriteDeadline(time.Now().Add(timeout))
	line := FormatRFC5424(s.Config, e, body)

	if proto == "tcp" {
		// RFC6587 octet counting: without a framing rule a collector cannot
		// tell where one message ends on a stream.
		line = fmt.Sprintf("%d %s", len(line), line)
	}
	_, err = conn.Write([]byte(line))
	return err
}

// Describe names the destination for the delivery log.
func (s SyslogSink) Describe() string {
	return "syslog " + s.Config.Protocol + "://" + s.Config.Address
}
