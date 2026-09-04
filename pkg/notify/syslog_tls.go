package notify

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"strings"
)

// Syslog transports.
const (
	SyslogUDP = "udp"
	SyslogTCP = "tcp"
	// SyslogTLS is RFC5425: syslog over TLS, conventionally on port 6514. It
	// is the only transport of the three that protects the message in flight —
	// plain UDP and TCP send device names, OIDs and breach values in the clear
	// across whatever network sits between here and the collector.
	SyslogTLS = "tls"
)

// DefaultSyslogTLSPort is the IANA-assigned port for syslog over TLS.
const DefaultSyslogTLSPort = "6514"

// normaliseProtocol maps whatever was stored onto the three we support.
func normaliseProtocol(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case SyslogTLS, "ssl", "syslog-tls":
		return SyslogTLS
	case SyslogTCP:
		return SyslogTCP
	default:
		return SyslogUDP
	}
}

// tlsConfigFor builds the client TLS configuration for a syslog sink.
//
// The defaults are the strict ones: the system trust store, TLS 1.2 or better,
// and the certificate name checked against the host being dialled. Everything
// that loosens them is opt-in and named for what it actually does, because a
// collector reached over TLS that silently accepts any certificate provides
// the appearance of transport security and none of the substance.
func tlsConfigFor(cfg SyslogConfig, clientKeyPEM string) (*tls.Config, error) {
	serverName := strings.TrimSpace(cfg.ServerName)
	if serverName == "" {
		// SNI and hostname verification need the name alone, not host:port.
		host, _, err := net.SplitHostPort(cfg.Address)
		if err != nil {
			// No port in the address; the whole string is the host.
			host = cfg.Address
		}
		serverName = host
	}

	out := &tls.Config{
		ServerName:         serverName,
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: cfg.InsecureSkipVerify, // #nosec G402 -- opt-in, named, and documented
	}

	// A private CA is the normal case for an internal collector, so support it
	// without forcing the operator to install anything machine-wide.
	if ca := strings.TrimSpace(cfg.CACert); ca != "" {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM([]byte(ca)) {
			return nil, fmt.Errorf("the CA certificate is not valid PEM")
		}
		out.RootCAs = pool
	}

	// Mutual TLS: the certificate is public and stored with the config, the
	// private key is a credential and comes from pkg/secrets.
	cert := strings.TrimSpace(cfg.ClientCert)
	key := strings.TrimSpace(clientKeyPEM)
	switch {
	case cert != "" && key != "":
		pair, err := tls.X509KeyPair([]byte(cert), []byte(key))
		if err != nil {
			return nil, fmt.Errorf("client certificate and key do not form a valid pair: %w", err)
		}
		out.Certificates = []tls.Certificate{pair}
	case cert != "" && key == "":
		return nil, fmt.Errorf("a client certificate was configured but its private key is missing")
	case cert == "" && key != "":
		return nil, fmt.Errorf("a client private key was configured but its certificate is missing")
	}

	return out, nil
}

// ValidateTLSMaterial checks a syslog sink's certificates and key without
// dialling anything.
//
// NotifySaveSink gates the message template and the webhook payload precisely
// so a broken sink is not discovered at 03:00 — and never checked any of this.
// Measured, all four surface only at send: a client certificate whose key does
// not match gives "tls: private key does not match public key"; a stored secret
// that is not PEM gives "failed to find any PEM data in key input"; a cleared
// certificate with the key still in pkg/secrets gives "a client private key was
// configured but its certificate is missing"; a malformed CA bundle gives "the
// CA certificate is not valid PEM". Permanent() is false for every one, so each
// routed event retried six times and dead-lettered.
func ValidateTLSMaterial(cfg SyslogConfig, clientKeyPEM string) error {
	if !strings.EqualFold(strings.TrimSpace(cfg.Protocol), "tls") {
		return nil
	}
	_, err := tlsConfigFor(cfg, clientKeyPEM)
	return err
}

// withDefaultPort appends the RFC5425 port when the address carries none, so
// "collector.example.com" reaches 6514 rather than failing to parse.
func withDefaultPort(address, port string) string {
	address = strings.TrimSpace(address)
	if address == "" {
		return address
	}
	if _, _, err := net.SplitHostPort(address); err == nil {
		return address
	}
	// An unbracketed IPv6 literal is the one case SplitHostPort rejects that
	// adding a port would not fix.
	if strings.Count(address, ":") > 1 && !strings.HasPrefix(address, "[") {
		return "[" + address + "]:" + port
	}
	return net.JoinHostPort(address, port)
}
