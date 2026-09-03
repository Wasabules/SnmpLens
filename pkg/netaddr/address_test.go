package netaddr

import (
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The measured failure: gosnmp brackets an IPv6 literal itself, so a target
// pasted in the form everything else writes it in became [[::1]]:161 and was
// rejected with an error naming neither the brackets nor the target.
func TestNormaliseTargetUnbracketsIPv6(t *testing.T) {
	cases := map[string]string{
		"[::1]":                 "::1",
		"[2001:db8::1]":         "2001:db8::1",
		"  [fe80::1%eth0]  ":    "fe80::1%eth0",
		"::1":                   "::1",
		"2001:db8::1":           "2001:db8::1",
		"192.168.1.1":           "192.168.1.1",
		"switch-01.example.com": "switch-01.example.com",
		"  10.0.0.1  ":          "10.0.0.1",
	}
	for in, want := range cases {
		if got := NormaliseTarget(in); got != want {
			t.Errorf("NormaliseTarget(%q) = %q, want %q", in, got, want)
		}
	}
}

// Brackets are stripped only around something that IS an address. A hostname
// in brackets is not a thing, and neither is an unbalanced pair; leaving them
// alone means the error the user sees still contains what they typed.
func TestNormaliseTargetLeavesNonAddressesAlone(t *testing.T) {
	for _, in := range []string{"[not-an-ip]", "[::1", "::1]", "[]", ""} {
		if got := NormaliseTarget(in); got != strings.TrimSpace(in) {
			t.Errorf("NormaliseTarget(%q) = %q, want it unchanged", in, got)
		}
	}
}

// The whole point of the function: what comes out must survive JoinHostPort,
// because that is what gosnmp does with it.
func TestNormalisedTargetSurvivesJoinHostPort(t *testing.T) {
	for _, in := range []string{"[::1]", "::1", "[2001:db8::1]", "192.168.1.1", "[fe80::1%eth0]"} {
		addr := net.JoinHostPort(NormaliseTarget(in), "161")
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			t.Errorf("%q -> %q is not a dialable address: %v", in, addr, err)
			continue
		}
		if port != "161" || !IsIPLiteral(host) {
			t.Errorf("%q -> host %q port %q", in, host, port)
		}
	}
}

// A wildcard listen must accept both families, or IPv6 traps never arrive.
// Asserted against the socket rather than the string, because it is Go's
// wildcard handling — not the text — that decides.
func TestListenAddressIsDualStack(t *testing.T) {
	ua, err := net.ResolveUDPAddr("udp", ListenAddress(0))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	conn, err := net.ListenUDP("udp", ua)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer conn.Close()
	port := conn.LocalAddr().(*net.UDPAddr).Port

	for _, from := range []string{"127.0.0.1", "::1"} {
		c, err := net.Dial("udp", net.JoinHostPort(from, itoa(port)))
		if err != nil {
			t.Logf("no %s stack on this host, skipping: %v", from, err)
			continue
		}
		if _, err := c.Write([]byte("x")); err != nil {
			t.Errorf("write from %s: %v", from, err)
		}
		c.Close()

		buf := make([]byte, 8)
		conn.SetReadDeadline(deadline())
		if _, peer, err := conn.ReadFromUDP(buf); err != nil {
			t.Errorf("a datagram from %s did not arrive on the wildcard listener: %v", from, err)
		} else if peer.IP.String() != from {
			// A dual-stack socket must report an IPv4 peer unmapped, or every
			// source address in the trap journal grows a ::ffff: prefix.
			t.Errorf("peer reported as %s, want %s", peer.IP, from)
		}
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

func deadline() time.Time { return time.Now().Add(2 * time.Second) }
