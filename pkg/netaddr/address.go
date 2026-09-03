package netaddr

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

// NormaliseTarget puts an address into the form gosnmp expects.
//
// gosnmp builds its dial string with net.JoinHostPort, which brackets an IPv6
// literal itself. A target pasted as "[2001:db8::1]" — the form every URL,
// every piece of documentation and every `ss -u` line writes it in — therefore
// becomes "[[2001:db8::1]]:161" and fails with
//
//	dial udp: address [[::1]]:11661: missing port in address
//
// which names neither the brackets nor the target. Measured against a live
// agent before this function existed; the unbracketed form worked all along.
//
// A host:port pair is deliberately NOT split: the port is its own field in the
// UI, and quietly overriding what someone typed there is worse than an error.
func NormaliseTarget(target string) string {
	t := strings.TrimSpace(target)
	if len(t) >= 2 && t[0] == '[' && t[len(t)-1] == ']' {
		if inner := t[1 : len(t)-1]; IsIPLiteral(inner) {
			return inner
		}
	}
	return t
}

// IsIPLiteral reports whether s is an IP address, zone and all.
//
// net.ParseIP rejects "fe80::1%eth0" — it has no place to put the zone — and
// a link-local address with a zone index is exactly how you reach a switch on
// the same segment, which is most of what this app talks to. Go's dialer
// understands the zone, so refusing it here would refuse it everywhere.
func IsIPLiteral(s string) bool {
	if i := strings.LastIndexByte(s, '%'); i > 0 {
		s = s[:i]
	}
	return net.ParseIP(s) != nil
}

// ListenAddress is the bind address for a UDP listener on every interface.
//
// The empty host is the wildcard, and Go opens a DUAL-STACK socket for it:
// AF_INET6 with IPV6_V6ONLY cleared, so IPv4 and IPv6 senders both arrive and
// an IPv4 peer is reported unmapped (127.0.0.1, not ::ffff:127.0.0.1).
//
// Written this way rather than "0.0.0.0" because the two behave identically —
// 0.0.0.0 is a wildcard too — and the numeric form reads like a deliberate
// choice to accept IPv4 only, which is how it gets "fixed" into one.
func ListenAddress(port int) string {
	return fmt.Sprintf(":%d", port)
}

// LastAddressIn returns the last IP address appearing in a line of text.
//
// Parsed rather than matched. An IPv6 address has several legal spellings and
// a regex that accepts them all accepts a great deal besides — the previous
// one accepted only `\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}`, so every hop of an
// IPv6 traceroute rendered with an empty address and no error. Splitting on
// the punctuation traceroute puts around addresses and asking net.ParseIP is
// both shorter and exactly right.
func LastAddressIn(line string) string {
	last := ""
	fields := strings.FieldsFunc(line, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '(' || r == ')' || r == ',' || r == '\r'
	})
	for _, f := range fields {
		f = strings.TrimSuffix(strings.TrimPrefix(f, "["), "]")
		if IsIPLiteral(f) {
			last = f
		}
	}
	return last
}

// ErrNotATarget is returned for something that cannot be a host.
var ErrNotATarget = errors.New("not an IP address or hostname")

// ValidTarget reports whether s can be handed to a program as a host.
//
// This exists because ping and traceroute are EXECUTED, and a value is passed
// as an argument rather than through a shell. There is no command injection —
// but there is option injection: measured, `tracert -d -w 2000 -h` answers
// "Une valeur doit être fournie pour l'option -h", so a target beginning with
// a dash is read as a flag and not as a host. No flag of tracert, traceroute
// or traceroute6 writes a file or runs anything, so the ceiling is low; the
// input is still one a person typed and one nothing checked.
//
// Deliberately a whitelist. A blacklist of "-" would let through the next
// shape somebody's traceroute treats specially, and every legitimate target is
// an address or a hostname.
func ValidTarget(s string) error {
	t := NormaliseTarget(s)
	if t == "" {
		return fmt.Errorf("%w: empty", ErrNotATarget)
	}
	if len(t) > 253 {
		return fmt.Errorf("%w: too long", ErrNotATarget)
	}
	if IsIPLiteral(t) {
		return nil
	}
	if isHostname(t) {
		return nil
	}
	return fmt.Errorf("%q is %w", s, ErrNotATarget)
}

// isHostname applies RFC 1123 host syntax.
//
// A leading dash is refused as a consequence of the rules rather than as a
// special case: a label may contain a hyphen and may not begin or end with
// one.
func isHostname(s string) bool {
	s = strings.TrimSuffix(s, ".")
	if s == "" {
		return false
	}
	for _, label := range strings.Split(s, ".") {
		if len(label) == 0 || len(label) > 63 {
			return false
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			switch {
			case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-':
			default:
				return false
			}
		}
	}
	return true
}
