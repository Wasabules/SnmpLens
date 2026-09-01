package notify

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"SnmpLens/pkg/events"
)

// selfSigned mints a throwaway CA-less certificate valid for 127.0.0.1 and
// "localhost", so the tests exercise real certificate verification rather than
// switching it off.
func selfSigned(t *testing.T, cn string) (certPEM, keyPEM []byte) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM
}

// tlsCollector is a minimal RFC5425 receiver: it reads one octet-counted frame
// and hands it back.
func tlsCollector(t *testing.T, certPEM, keyPEM []byte, clientCAs *x509.CertPool) (addr string, received chan string) {
	t.Helper()

	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("server keypair: %v", err)
	}
	cfg := &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12}
	if clientCAs != nil {
		cfg.ClientCAs = clientCAs
		cfg.ClientAuth = tls.RequireAndVerifyClientCert
	}

	ln, err := tls.Listen("tcp", "127.0.0.1:0", cfg)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	received = make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))

		// RFC6587 octet counting: "<length> <message>".
		r := bufio.NewReader(conn)
		countStr, err := r.ReadString(' ')
		if err != nil {
			return
		}
		n, err := strconv.Atoi(strings.TrimSpace(countStr))
		if err != nil || n <= 0 || n > 1<<20 {
			return
		}
		buf := make([]byte, n)
		if _, err := readFull(r, buf); err != nil {
			return
		}
		received <- string(buf)
	}()

	return ln.Addr().String(), received
}

func readFull(r *bufio.Reader, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := r.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

func tlsEvent() events.Event {
	return events.Event{
		ID: "evt-tls-1", Category: events.CategoryThreshold, Kind: events.KindThresholdOpened,
		Severity: "major", Source: "10.0.0.1", OID: "1.3.6.1.2.1.2.2.1.10.1",
		Ts: time.Now().UTC().Format(time.RFC3339), Summary: "ifInOctets above 900 on 10.0.0.1",
	}
}

// The whole point of RFC5425: the message must actually arrive over a verified
// TLS connection, framed the way a collector expects.
func TestSyslogOverTLSDelivers(t *testing.T) {
	certPEM, keyPEM := selfSigned(t, "localhost")
	addr, received := tlsCollector(t, certPEM, keyPEM, nil)

	sink := SyslogSink{Config: SyslogConfig{
		Address:  addr,
		Protocol: SyslogTLS,
		Facility: 16,
		Hostname: "workstation",
		AppName:  "SnmpLens",
		// Trust the throwaway certificate as a private CA — this is the
		// internal-collector case, NOT verification being switched off.
		CACert: string(certPEM),
	}}

	if err := sink.Send(tlsEvent(), "", "ifInOctets above 900 on 10.0.0.1"); err != nil {
		t.Fatalf("Send over TLS: %v", err)
	}

	select {
	case line := <-received:
		if !strings.HasPrefix(line, "<") {
			t.Errorf("not an RFC5424 line: %q", line)
		}
		for _, want := range []string{"threshold.opened", `id="evt-tls-1"`, "ifInOctets above 900"} {
			if !strings.Contains(line, want) {
				t.Errorf("delivered line is missing %q: %s", want, line)
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("nothing reached the collector")
	}
}

// The octet count must be a BYTE count. A rune count would desynchronise the
// collector on the first non-ASCII summary, and all five shipped locales
// produce those.
func TestOctetCountIsBytesNotRunes(t *testing.T) {
	certPEM, keyPEM := selfSigned(t, "localhost")
	addr, received := tlsCollector(t, certPEM, keyPEM, nil)

	sink := SyslogSink{Config: SyslogConfig{
		Address: addr, Protocol: SyslogTLS, Hostname: "workstation", CACert: string(certPEM),
	}}
	e := tlsEvent()
	body := "Seuil dépassé sur l'équipement — 数值过高"

	if err := sink.Send(e, "", body); err != nil {
		t.Fatalf("Send: %v", err)
	}
	select {
	case line := <-received:
		// The collector read exactly `count` bytes; if the count had been in
		// runes the tail would be truncated.
		if !strings.HasSuffix(line, body) {
			t.Errorf("the frame was truncated, so the count was not in octets: %q", line)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("nothing reached the collector")
	}
}

// A collector presenting an untrusted certificate must be REFUSED. Getting
// this wrong would give the appearance of transport security and none of it.
func TestUntrustedCertificateIsRefused(t *testing.T) {
	certPEM, keyPEM := selfSigned(t, "localhost")
	addr, _ := tlsCollector(t, certPEM, keyPEM, nil)

	sink := SyslogSink{Config: SyslogConfig{Address: addr, Protocol: SyslogTLS}}
	err := sink.Send(tlsEvent(), "", "x")
	if err == nil {
		t.Fatal("an unverifiable certificate was accepted")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "certificate") &&
		!strings.Contains(strings.ToLower(err.Error()), "authority") {
		t.Errorf("the error should name the certificate problem, got: %v", err)
	}
}

// ...but the escape hatch has to work, because a lab collector with a
// self-signed certificate is a real situation.
func TestInsecureSkipVerifyIsHonoured(t *testing.T) {
	certPEM, keyPEM := selfSigned(t, "localhost")
	addr, received := tlsCollector(t, certPEM, keyPEM, nil)

	sink := SyslogSink{Config: SyslogConfig{
		Address: addr, Protocol: SyslogTLS, InsecureSkipVerify: true,
	}}
	if err := sink.Send(tlsEvent(), "", "x"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	select {
	case <-received:
	case <-time.After(5 * time.Second):
		t.Fatal("nothing reached the collector")
	}
}

// Mutual TLS: the client key is a credential, so it arrives through
// SinkConfig.Secret rather than being stored with the configuration.
func TestMutualTLSUsesTheStoredKey(t *testing.T) {
	serverCert, serverKey := selfSigned(t, "localhost")
	clientCert, clientKey := selfSigned(t, "snmplens-client")

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(clientCert) {
		t.Fatal("client certificate is not valid PEM")
	}
	addr, received := tlsCollector(t, serverCert, serverKey, pool)

	cfg := SinkConfig{
		Kind: SinkSyslog,
		Syslog: SyslogConfig{
			Address: addr, Protocol: SyslogTLS,
			CACert:     string(serverCert),
			ClientCert: string(clientCert),
		},
	}
	sink, ok := Build(cfg, string(clientKey))
	if !ok {
		t.Fatal("Build refused the syslog sink")
	}
	if err := sink.Send(tlsEvent(), "", "mutual"); err != nil {
		t.Fatalf("Send with mutual TLS: %v", err)
	}
	select {
	case <-received:
	case <-time.After(5 * time.Second):
		t.Fatal("nothing reached the collector")
	}
}

// A certificate with no key is a misconfiguration that must be reported, not
// silently downgraded to a one-way handshake the collector will reject anyway.
func TestClientCertWithoutKeyIsAnError(t *testing.T) {
	clientCert, _ := selfSigned(t, "snmplens-client")
	_, err := tlsConfigFor(SyslogConfig{Address: "c:6514", ClientCert: string(clientCert)}, "")
	if err == nil {
		t.Fatal("expected an error for a certificate with no key")
	}
}

func TestMalformedCAIsReported(t *testing.T) {
	_, err := tlsConfigFor(SyslogConfig{Address: "c:6514", CACert: "not a certificate"}, "")
	if err == nil {
		t.Fatal("expected an error for a malformed CA bundle")
	}
}

// The name checked against the certificate comes from the address unless it is
// overridden, so a collector reached by IP can still be verified by name.
func TestServerNameDefaultsToTheHost(t *testing.T) {
	cfg, err := tlsConfigFor(SyslogConfig{Address: "collector.example.com:6514"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ServerName != "collector.example.com" {
		t.Errorf("ServerName = %q", cfg.ServerName)
	}

	cfg, err = tlsConfigFor(SyslogConfig{Address: "10.0.0.9:6514", ServerName: "collector.example.com"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ServerName != "collector.example.com" {
		t.Errorf("the override was ignored: %q", cfg.ServerName)
	}
}

func TestTLSMinimumVersionIs12(t *testing.T) {
	cfg, err := tlsConfigFor(SyslogConfig{Address: "c:6514"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %x, want TLS 1.2", cfg.MinVersion)
	}
}

// An address with no port must reach 6514 rather than failing to parse.
func TestDefaultTLSPortIsApplied(t *testing.T) {
	cases := map[string]string{
		"collector.example.com":      "collector.example.com:6514",
		"collector.example.com:1514": "collector.example.com:1514",
		"10.0.0.9":                   "10.0.0.9:6514",
		"[fe80::1]:1514":             "[fe80::1]:1514",
		"fe80::1":                    "[fe80::1]:6514",
	}
	for in, want := range cases {
		if got := withDefaultPort(in, DefaultSyslogTLSPort); got != want {
			t.Errorf("withDefaultPort(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestProtocolNormalisation(t *testing.T) {
	cases := map[string]string{
		"tls": SyslogTLS, "TLS": SyslogTLS, " ssl ": SyslogTLS,
		"tcp": SyslogTCP, "TCP": SyslogTCP,
		"udp": SyslogUDP, "": SyslogUDP, "nonsense": SyslogUDP,
	}
	for in, want := range cases {
		if got := normaliseProtocol(in); got != want {
			t.Errorf("normaliseProtocol(%q) = %q, want %q", in, got, want)
		}
	}
}

// Describe feeds the delivery log, so it must name the transport actually used.
func TestDescribeNamesTheTransport(t *testing.T) {
	s := SyslogSink{Config: SyslogConfig{Address: "c:6514", Protocol: "TLS"}}
	if got := s.Describe(); got != fmt.Sprintf("syslog tls://%s", "c:6514") {
		t.Errorf("Describe() = %q", got)
	}
}
