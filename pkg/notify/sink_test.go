package notify

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/smtp"
	"strings"
	"testing"
	"time"

	"SnmpLens/pkg/events"
)

func alertEvent() events.Event {
	return events.Event{
		ID:       "11111111-2222-3333-4444-555555555555",
		Ts:       "2026-09-01T10:00:00Z",
		Category: events.CategoryThreshold,
		Kind:     events.KindThresholdOpened,
		Severity: "major",
		State:    events.StateOpen,
		Source:   "10.0.0.1",
		OID:      "1.3.6.1.2.1.2.2.1.10.1",
		Summary:  "Link saturated",
	}
}

// PRI = facility*8 + severity. major maps to RFC5424 err(3), so local0 (16)
// gives 16*8+3 = 131.
func TestRFC5424PriorityAndHeaderShape(t *testing.T) {
	cfg := SyslogConfig{Facility: 16, Hostname: "monitor01", AppName: "SnmpLens"}
	line := FormatRFC5424(cfg, alertEvent(), "Link saturated on WAN")

	if !strings.HasPrefix(line, "<131>1 ") {
		t.Fatalf("bad PRI/VERSION prefix: %q", line[:min(24, len(line))])
	}

	fields := strings.SplitN(line, " ", 7)
	if len(fields) < 7 {
		t.Fatalf("header has too few fields: %q", line)
	}
	if fields[1] != "2026-09-01T10:00:00.000000Z" {
		t.Errorf("timestamp = %q", fields[1])
	}
	if fields[2] != "monitor01" {
		t.Errorf("hostname = %q", fields[2])
	}
	if fields[3] != "SnmpLens" {
		t.Errorf("app-name = %q", fields[3])
	}
	if fields[4] != "-" {
		t.Errorf("procid should be NILVALUE, got %q", fields[4])
	}
	if fields[5] != events.KindThresholdOpened {
		t.Errorf("msgid = %q", fields[5])
	}
	if !strings.Contains(line, `id="11111111-2222-3333-4444-555555555555"`) {
		t.Error("structured data must carry the event id so a collector can deduplicate")
	}
	if !strings.Contains(line, "\xEF\xBB\xBF") {
		t.Error("UTF-8 BOM missing before the message")
	}
}

// A space in a header field would silently shift every field after it, so the
// grammar has to be enforced rather than trusted.
func TestRFC5424SanitizesHeaderFields(t *testing.T) {
	cfg := SyslogConfig{Hostname: "my host with spaces", AppName: "App Name"}
	line := FormatRFC5424(cfg, alertEvent(), "msg")
	fields := strings.SplitN(line, " ", 7)
	if strings.Contains(fields[2], " ") || fields[2] != "myhostwithspaces" {
		t.Errorf("hostname not sanitised: %q", fields[2])
	}
	if fields[3] != "AppName" {
		t.Errorf("app-name not sanitised: %q", fields[3])
	}
}

func TestRFC5424EscapesStructuredData(t *testing.T) {
	e := alertEvent()
	e.Source = `10.0.0.1"]x\`
	line := FormatRFC5424(SyslogConfig{}, e, "msg")
	if !strings.Contains(line, `source="10.0.0.1\"\]x\\"`) {
		t.Errorf("structured data not escaped: %q", line)
	}
}

func TestRFC5424FallsBackOnUnparseableTimestamp(t *testing.T) {
	e := alertEvent()
	e.Ts = "not a timestamp"
	line := FormatRFC5424(SyslogConfig{}, e, "msg")
	fields := strings.SplitN(line, " ", 3)
	if _, err := time.Parse("2006-01-02T15:04:05.000000Z", fields[1]); err != nil {
		t.Errorf("expected a valid substituted timestamp, got %q", fields[1])
	}
}

// Real socket, not a formatting check: this proves the sink actually writes.
func TestSyslogSinkSendsOverUDP(t *testing.T) {
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	sink := SyslogSink{Config: SyslogConfig{
		Address: conn.LocalAddr().String(), Protocol: "udp", Facility: 16, Hostname: "h",
	}}
	if err := sink.Send(alertEvent(), "", "Link saturated on WAN"); err != nil {
		t.Fatalf("Send: %v", err)
	}

	buf := make([]byte, 4096)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := conn.ReadFrom(buf)
	if err != nil {
		t.Fatalf("nothing arrived: %v", err)
	}
	got := string(buf[:n])
	if !strings.HasPrefix(got, "<131>1 ") || !strings.Contains(got, "Link saturated on WAN") {
		t.Errorf("unexpected datagram: %q", got)
	}
}

// TCP needs octet-counted framing, or a collector cannot tell where a message
// ends on a stream.
func TestSyslogSinkFramesTCPWithOctetCount(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	done := make(chan string, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			done <- ""
			return
		}
		defer c.Close()
		buf := make([]byte, 4096)
		c.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, _ := c.Read(buf)
		done <- string(buf[:n])
	}()

	sink := SyslogSink{Config: SyslogConfig{Address: ln.Addr().String(), Protocol: "tcp", Hostname: "h"}}
	if err := sink.Send(alertEvent(), "", "hello"); err != nil {
		t.Fatalf("Send: %v", err)
	}

	got := <-done
	space := strings.Index(got, " ")
	if space <= 0 {
		t.Fatalf("no octet count prefix: %q", got)
	}
	count := got[:space]
	rest := got[space+1:]
	if len(rest) != atoiOrZero(count) {
		t.Errorf("octet count %s does not match the %d bytes that followed", count, len(rest))
	}
}

func atoiOrZero(s string) int {
	n, ok := atoi(s)
	if !ok {
		return 0
	}
	return n
}

func TestWebhookSinkPostsEventAndDedupHeader(t *testing.T) {
	type received struct {
		auth  string
		id    string
		body  webhookBody
		extra string
	}
	got := make(chan received, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b webhookBody
		json.NewDecoder(r.Body).Decode(&b)
		got <- received{
			auth:  r.Header.Get("Authorization"),
			id:    r.Header.Get("X-SnmpLens-Event-Id"),
			body:  b,
			extra: r.Header.Get("X-Team"),
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	sink := WebhookSink{Config: WebhookConfig{
		URL:     srv.URL,
		Token:   "s3cret",
		Headers: map[string]string{"X-Team": "noc"},
	}}
	if err := sink.Send(alertEvent(), "subject", "body text"); err != nil {
		t.Fatalf("Send: %v", err)
	}

	r := <-got
	if r.auth != "Bearer s3cret" {
		t.Errorf("Authorization = %q", r.auth)
	}
	if r.id != alertEvent().ID {
		t.Errorf("dedup header = %q", r.id)
	}
	if r.extra != "noc" {
		t.Errorf("custom header not sent: %q", r.extra)
	}
	if r.body.Event.Kind != events.KindThresholdOpened || r.body.Body != "body text" {
		t.Errorf("payload = %+v", r.body)
	}
}

func TestWebhookSinkReportsHTTPFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte("upstream is down"))
	}))
	defer srv.Close()

	sink := WebhookSink{Config: WebhookConfig{URL: srv.URL}}
	err := sink.Send(alertEvent(), "", "x")
	if err == nil {
		t.Fatal("a 502 must be reported as a failure so the delivery is retried")
	}
	if !strings.Contains(err.Error(), "upstream is down") {
		t.Errorf("error should quote the response body, got %v", err)
	}
}

func TestWebhookSinkRejectsEmptyURL(t *testing.T) {
	if err := (WebhookSink{}).Send(alertEvent(), "", ""); err == nil {
		t.Fatal("an unconfigured webhook must fail loudly rather than silently succeed")
	}
}

// Credentials must never go out over a connection that is not encrypted.
func TestLoginAuthRefusesPlaintextConnection(t *testing.T) {
	a := &loginAuth{username: "u", password: "p", host: "mail.example.com", tlsOK: false}
	if _, _, err := a.Start(smtpServerInfo("mail.example.com", false)); err == nil {
		t.Fatal("AUTH LOGIN must refuse an unencrypted connection")
	}
	b := &loginAuth{username: "u", password: "p", host: "mail.example.com", tlsOK: true}
	if _, _, err := b.Start(smtpServerInfo("mail.example.com", true)); err != nil {
		t.Fatalf("AUTH LOGIN should proceed over TLS: %v", err)
	}
}

func TestBuildMessageEncodesSubjectAndCarriesID(t *testing.T) {
	cfg := EmailConfig{From: "a@example.com", To: []string{"b@example.com"}}
	msg := buildMessage(cfg, alertEvent(), "Seuil dépassé sur é", "corps")

	if !strings.Contains(msg, "Subject: =?utf-8?q?") {
		t.Errorf("non-ASCII subject not MIME-encoded:\n%s", msg)
	}
	if !strings.Contains(msg, "X-SnmpLens-Event-Id: "+alertEvent().ID) {
		t.Error("event id header missing")
	}
	if !strings.Contains(msg, "\r\n\r\ncorps") {
		t.Error("body not separated from the headers by a blank line")
	}
}

func smtpServerInfo(name string, tls bool) *smtp.ServerInfo {
	return &smtp.ServerInfo{Name: name, TLS: tls, Auth: []string{"LOGIN"}}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
