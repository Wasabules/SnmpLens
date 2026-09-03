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

// A target becomes argv for ping and traceroute. There is no shell, so nothing
// can be injected as a command — but a value beginning with a dash IS read as
// an option: measured, `tracert -d -w 2000 -h` answers "a value must be
// supplied for the option -h" while an ordinary name is resolved.
func TestValidTargetAcceptsRealHosts(t *testing.T) {
	for _, in := range []string{
		"192.168.1.1", "8.8.8.8", "::1", "2001:db8::1", "[2001:db8::1]",
		"fe80::1%eth0", "switch-01", "switch-01.example.com", "a.b.c.d.e",
		"example.com.", "XN--BCHER-KVA.example", "0", "host-with-many-hyphens-1",
		// Not RFC 1123, and used everywhere: Microsoft DNS permits it, and
		// /etc/hosts has no syntax rules at all.
		"my_switch.corp.local", "core_sw_01", "_underscore-start",
	} {
		if err := ValidTarget(in); err != nil {
			t.Errorf("ValidTarget(%q) = %v, want nil", in, err)
		}
	}
}

func TestValidTargetRefusesWhatIsNotAHost(t *testing.T) {
	for _, in := range []string{
		"", "   ",
		// The reason this function exists.
		"-h", "-d", "--help", "-w 2000", "-j 1.2.3.4",
		// A label may not start or end with a hyphen.
		"-leading.example.com", "trailing-.example.com", "mid..dot",
		// Not host syntax at all.
		"host name", "host;name", "host|name", "host$(id)", "host\ttab",
		"host/../etc", "*.example.com", "héllo.example.com",
	} {
		if err := ValidTarget(in); err == nil {
			t.Errorf("ValidTarget(%q) was accepted", in)
		}
	}
}

// Everything ValidTarget accepts must be safe to place in argv: never read as
// an option by anything, on any platform.
func TestEveryAcceptedTargetIsArgvSafe(t *testing.T) {
	for _, in := range []string{
		"192.168.1.1", "::1", "[2001:db8::1]", "fe80::1%eth0", "switch-01.example.com",
	} {
		if err := ValidTarget(in); err != nil {
			t.Fatalf("%q should be accepted: %v", in, err)
		}
		got := NormaliseTarget(in)
		if strings.HasPrefix(got, "-") {
			t.Errorf("%q normalises to %q, which argv reads as an option", in, got)
		}
	}
}

// A name longer than a hostname can be is refused before it reaches a resolver.
func TestValidTargetBoundsLength(t *testing.T) {
	if err := ValidTarget(strings.Repeat("a", 64) + ".example.com"); err == nil {
		t.Error("a 64-character label was accepted")
	}
	long := strings.TrimSuffix(strings.Repeat("ab.", 100), ".")
	if err := ValidTarget(long); err == nil {
		t.Error("a 299-character name was accepted")
	}
}
