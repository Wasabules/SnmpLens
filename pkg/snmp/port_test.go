package snmp

import "testing"

// gosnmp takes a uint16 and Go truncates silently on conversion, so a port of
// 70000 became 4464 and the request went somewhere nobody asked for. The value
// arrives from the renderer, so it is not this package's to trust.
//
// Found by gosec (G115) rather than by reading the code.
func TestNormalisePortRefusesOutOfRange(t *testing.T) {
	cases := map[int]uint16{
		161:    161,
		65535:  65535,
		1:      1,
		0:      DefaultPort, // unset means the default, not port zero
		-1:     DefaultPort,
		65536:  DefaultPort, // would have truncated to 0
		70000:  DefaultPort, // would have truncated to 4464
		131233: DefaultPort, // would have truncated to 161, right by accident
	}
	for in, want := range cases {
		if got := normalisePort(in, DefaultPort); got != want {
			t.Errorf("normalisePort(%d) = %d, want %d", in, got, want)
		}
	}
	if got := normalisePort(0, DefaultTrapPort); got != DefaultTrapPort {
		t.Errorf("the trap fallback is not applied: %d, want %d", got, DefaultTrapPort)
	}
}
