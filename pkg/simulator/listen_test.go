package simulator

import (
	"errors"
	"net"
	"testing"
)

func TestOnlyALoopbackAddressIsAccepted(t *testing.T) {
	for in, want := range map[string]string{
		"127.0.0.1:161":          "127.0.0.1:161",
		"127.0.0.2:16161":        "127.0.0.2:16161",
		"127.1.2.3:0":            "127.1.2.3:0",
		"[::1]:161":              "[::1]:161",
		"localhost:1161":         "127.0.0.1:1161",
		"[::ffff:127.0.0.1]:161": "127.0.0.1:161",
	} {
		got, err := CheckListen(in)
		if err != nil || got.String() != want {
			t.Errorf("%q: %v, %v; want %s", in, got, err, want)
		}
	}
	for _, in := range []string{
		"0.0.0.0:161", ":161", "[::]:161",
		"192.168.1.10:161", "10.0.0.1:161", "[::ffff:10.0.0.1]:161",
		"example.com:161", // refused, not resolved
	} {
		if _, err := CheckListen(in); !errors.Is(err, ErrNotLoopback) {
			t.Errorf("%q: %v, want ErrNotLoopback", in, err)
		}
	}
	for _, in := range []string{"", "127.0.0.1", "127.0.0.1:65536", "127.0.0.1:snmp", "[::1%1]:161"} {
		if _, err := CheckListen(in); err == nil {
			t.Errorf("%q was accepted", in)
		}
	}
}

// A configuration cannot reach Start with one.
func TestAnAgentCannotBeToldToListenOnTheNetwork(t *testing.T) {
	_, err := NewAgent(Config{Listen: "0.0.0.0:161", Versions: []string{"v2c"}, Community: "public", Objects: sampleObjects()})
	if !errors.Is(err, ErrNotLoopback) {
		t.Fatalf("err = %v, want ErrNotLoopback", err)
	}
}

// Start holds what it actually bound to the rule as well. Checked on addresses
// rather than by binding one, since binding every interface is precisely what
// the rule exists to prevent.
func TestTheBoundSocketIsCheckedToo(t *testing.T) {
	for _, addr := range []net.Addr{
		&net.UDPAddr{IP: net.IPv4zero, Port: 161},
		&net.UDPAddr{IP: net.IPv6unspecified, Port: 161},
		&net.UDPAddr{IP: net.ParseIP("192.168.1.10"), Port: 161},
		&net.UDPAddr{Port: 161}, // what a zero Agent binds
	} {
		if err := checkBound(addr); !errors.Is(err, ErrNotLoopback) {
			t.Errorf("%v: %v, want ErrNotLoopback", addr, err)
		}
	}
	if err := checkBound(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 2), Port: 161}); err != nil {
		t.Error(err)
	}
}
