package snmp

import (
	"net"
	"testing"
	"time"
)

// deadTarget returns an address nothing will answer on.
//
// A bound-then-closed UDP port, so the address is real and routable and the
// packets simply go nowhere — which is what an unreachable device looks like
// to the client, rather than an immediate ICMP refusal.
func deadTarget(t *testing.T) (string, int) {
	t.Helper()
	c, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("no UDP socket: %v", err)
	}
	port := c.LocalAddr().(*net.UDPAddr).Port
	c.Close()
	return "127.0.0.1", port
}

// The cost of an unreachable device must not scale with the number of OIDs.
//
// The poll used to call Get once per OID: a fresh socket and ONE varbind each
// time, walked serially while only the targets ran concurrently. Ten OIDs
// against a device that does not answer therefore cost ten full timeouts, one
// after another — on the clock that runs with the window closed, where nothing
// is watching. GetMany asks for all of them in one PDU on one connection, so
// the cost is one timeout whatever the count.
//
// The assertion is deliberately loose. What matters is the SHAPE — constant
// rather than linear in the OID count — not a millisecond figure that would
// make this test a flake on a loaded runner.
func TestAnUnreachableDeviceCostsOneTimeoutNotOnePerOid(t *testing.T) {
	host, port := deadTarget(t)
	c := newHeadlessClient()

	const timeoutSec = 1
	oids := []string{
		"1.3.6.1.2.1.1.1.0", "1.3.6.1.2.1.1.3.0", "1.3.6.1.2.1.1.5.0",
		"1.3.6.1.2.1.1.6.0", "1.3.6.1.2.1.1.4.0", "1.3.6.1.2.1.2.1.0",
	}

	start := time.Now()
	res := c.GetMany([]string{host}, oids, "public", "v2c", port, timeoutSec, 0, V3Params{})
	elapsed := time.Since(start)

	// One timeout, with generous headroom. The old shape would be at least
	// len(oids) x timeout — six seconds here.
	limit := time.Duration(timeoutSec) * time.Second * 3
	if elapsed > limit {
		t.Errorf("%d OIDs against an unreachable device took %v; a single timeout is %ds, so this "+
			"is still paying one per OID", len(oids), elapsed.Round(time.Millisecond), timeoutSec)
	}

	// And the failure is reported PER OID. A target-level error that produced
	// no per-OID entries would leave the scheduler with nothing to record, and
	// a session with no points looks paused rather than broken.
	if len(res) != 1 {
		t.Fatalf("%d results for one target", len(res))
	}
	if len(res[0].Errors) != len(oids) {
		t.Errorf("%d errors for %d OIDs; every one must be accounted for", len(res[0].Errors), len(oids))
	}
	for _, oid := range oids {
		if res[0].Errors[oid] == "" {
			t.Errorf("no error recorded for %s", oid)
		}
		if res[0].Results[oid] != nil {
			t.Errorf("a value was reported for %s by a device that never answered", oid)
		}
	}
}

// Several unreachable targets stay concurrent, as they were before.
func TestUnreachableTargetsAreStillPolledConcurrently(t *testing.T) {
	c := newHeadlessClient()
	var targets []string
	var port int
	for i := 0; i < 4; i++ {
		h, p := deadTarget(t)
		targets = append(targets, h)
		port = p // they share a port; the addresses are what differ in production
	}

	start := time.Now()
	res := c.GetMany(targets, []string{"1.3.6.1.2.1.1.3.0"}, "public", "v2c", port, 1, 0, V3Params{})
	elapsed := time.Since(start)

	if elapsed > 3*time.Second {
		t.Errorf("%d unreachable targets took %v; they are being polled one after another",
			len(targets), elapsed.Round(time.Millisecond))
	}
	if len(res) != len(targets) {
		t.Errorf("%d results for %d targets", len(res), len(targets))
	}
}

// The caller's target order is what pairs a result with the device it came
// from. The goroutines finish in whatever order the network allows.
func TestGetManyReturnsTargetsInTheOrderAsked(t *testing.T) {
	c := newHeadlessClient()
	_, port := deadTarget(t)
	targets := []string{"127.0.0.1", "127.0.0.2", "127.0.0.3"}

	res := c.GetMany(targets, []string{"1.3.6.1.2.1.1.3.0"}, "public", "v2c", port, 1, 0, V3Params{})
	if len(res) != len(targets) {
		t.Fatalf("%d results for %d targets", len(res), len(targets))
	}
	for i, want := range targets {
		if res[i].Target != want {
			t.Errorf("position %d is %q, want %q — a caller zipping this against its own list "+
				"would attribute one device's readings to another", i, res[i].Target, want)
		}
	}
}

// No OIDs is not an error, and must not open a connection.
func TestGetManyWithNoOidsDoesNothing(t *testing.T) {
	c := newHeadlessClient()
	host, port := deadTarget(t)

	start := time.Now()
	res := c.GetMany([]string{host}, nil, "public", "v2c", port, 5, 3, V3Params{})
	if len(res) != 0 {
		t.Errorf("%d results for no OIDs", len(res))
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("took %v with nothing to ask; it connected anyway", d.Round(time.Millisecond))
	}
}
