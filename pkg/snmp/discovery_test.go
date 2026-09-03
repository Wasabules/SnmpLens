package snmp

import (
	"strings"
	"testing"
)

// A scan is sized by its prefix before a packet is sent, so the prefix is what
// has to be refused. Without this, 10.0.0.0/8 allocates 16.7 million strings
// and an IPv6 /64 never finishes expanding at all.
func TestExpandCIDRRefusesPrefixesTooLargeToScan(t *testing.T) {
	for _, cidr := range []string{"10.0.0.0/8", "0.0.0.0/0", "2001:db8::/64", "::/0", "2001:db8::/32"} {
		ips, err := expandCIDR(cidr)
		if err == nil {
			t.Errorf("%s was accepted and expanded to %d addresses", cidr, len(ips))
			continue
		}
		if !strings.Contains(err.Error(), "smaller prefix") {
			t.Errorf("%s: the error should say what to do instead: %v", cidr, err)
		}
	}
}

func TestExpandCIDRAcceptsWhatFits(t *testing.T) {
	cases := []struct {
		cidr string
		want int
	}{
		// IPv4 drops the network and the broadcast, which answer nothing.
		{"192.168.1.0/24", 254},
		{"192.168.1.0/30", 2},
		{"192.168.1.0/31", 2},
		{"192.168.1.5/32", 1},
		// IPv6 has no broadcast and no unusable first address, so every
		// address in the prefix is a host worth probing.
		{"2001:db8::/126", 4},
		{"2001:db8::/128", 1},
		{"2001:db8::/112", 65536},
	}
	for _, c := range cases {
		ips, err := expandCIDR(c.cidr)
		if err != nil {
			t.Errorf("%s: %v", c.cidr, err)
			continue
		}
		if len(ips) != c.want {
			t.Errorf("%s expanded to %d addresses, want %d", c.cidr, len(ips), c.want)
		}
	}
}

// The bug this fixes: an IPv6 /126 lost 2001:db8:: and 2001:db8::3 to a rule
// about IPv4 broadcast addresses. Both are ordinary hosts.
func TestExpandCIDRKeepsEveryIPv6Host(t *testing.T) {
	ips, err := expandCIDR("2001:db8::/126")
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(ips, " ")
	for _, want := range []string{"2001:db8::", "2001:db8::1", "2001:db8::2", "2001:db8::3"} {
		if !strings.Contains(got, want+" ") && !strings.HasSuffix(got, want) {
			t.Errorf("%s is missing from %v", want, ips)
		}
	}
}

// A bare address, in either family, is a scan of one.
func TestExpandCIDRAcceptsASingleAddress(t *testing.T) {
	for in, want := range map[string]string{
		"192.168.1.1":   "192.168.1.1",
		"2001:db8::1":   "2001:db8::1",
		"  ::1  ":       "::1",
		"[2001:db8::1]": "",
	} {
		ips, err := expandCIDR(in)
		if want == "" {
			if err == nil {
				t.Errorf("%q was accepted as %v", in, ips)
			}
			continue
		}
		if err != nil || len(ips) != 1 || ips[0] != want {
			t.Errorf("expandCIDR(%q) = %v, %v; want [%s]", in, ips, err, want)
		}
	}
}

func TestExpandCIDRRejectsNonsense(t *testing.T) {
	for _, in := range []string{"", "not-a-cidr", "192.168.1.0/33", "2001:db8::/129"} {
		if ips, err := expandCIDR(in); err == nil {
			t.Errorf("%q was accepted as %v", in, ips)
		}
	}
}
