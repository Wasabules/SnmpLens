package monitor_test

// Integration check for the Go poll clock against a REAL SNMP agent.
//
// The unit tests in this package drive the scheduler with a stub fetcher, which
// proves the clock and the derived maths but says nothing about whether the
// thing actually talks to an agent. This test closes that gap using the bundled
// simulator, the same way the SNMPv3 fix was verified rather than assumed.
//
// It is skipped unless SNMPLENS_TEST_AGENT points at a running simulator:
//
//	python tools/snmp_test_agent.py --port 11611 --no-traps
//	SNMPLENS_TEST_AGENT=127.0.0.1:11611 go test ./pkg/monitor/ -run Integration -v

import (
	"context"
	"net"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"SnmpLens/pkg/monitor"
	"SnmpLens/pkg/snmp"
)

// sysUpTime is a TimeTicks counter that always moves, which makes it the one
// OID guaranteed to produce a non-zero delta between two polls.
const sysUpTime = "1.3.6.1.2.1.1.3.0"

func agentAddr(t *testing.T) (string, int) {
	t.Helper()
	addr := os.Getenv("SNMPLENS_TEST_AGENT")
	if addr == "" {
		t.Skip("set SNMPLENS_TEST_AGENT=host:port to run against the bundled simulator")
	}
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("SNMPLENS_TEST_AGENT must be host:port, got %q: %v", addr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("bad port in SNMPLENS_TEST_AGENT: %v", err)
	}
	return host, port
}

func TestIntegrationSchedulerPollsARealAgent(t *testing.T) {
	host, port := agentAddr(t)

	client := snmp.NewClient(context.Background())
	fetch := func(_ context.Context, oid string, targets []string) []monitor.Reading {
		out := []monitor.Reading{}
		for _, r := range client.Get(targets, oid, "public", "v2c", port, 2, 1, snmp.V3Params{}) {
			reading := monitor.Reading{Target: r.Target, Error: r.Error, ResponseTimeMs: int(r.ResponseTimeMs)}
			if r.Result != nil {
				reading.SnmpType = r.Result.Type
				switch v := r.Result.Value.(type) {
				case uint32:
					f := float64(v)
					reading.Value = &f
				case int:
					f := float64(v)
					reading.Value = &f
				case int64:
					f := float64(v)
					reading.Value = &f
				case uint64:
					f := float64(v)
					reading.Value = &f
				}
			}
			out = append(out, reading)
		}
		return out
	}

	var mu sync.Mutex
	var points []monitor.Point

	s := monitor.NewScheduler()
	s.Persist = func(p []monitor.Point) {
		mu.Lock()
		defer mu.Unlock()
		points = append(points, p...)
	}

	s.Start(monitor.SessionSpec{
		ID: "integration", OIDs: []string{sysUpTime}, Targets: []string{host},
		Interval: 400 * time.Millisecond, Fetch: fetch,
	})
	defer s.StopAll()

	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(points)
		mu.Unlock()
		if n >= 3 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	mu.Lock()
	got := append([]monitor.Point(nil), points...)
	mu.Unlock()

	if len(got) < 3 {
		t.Fatalf("only %d samples in 8s; is the simulator running on %s:%d?", len(got), host, port)
	}
	for i, p := range got {
		if p.Error != "" {
			t.Fatalf("sample %d returned an error: %s", i, p.Error)
		}
		if p.Value == nil {
			t.Fatalf("sample %d has no value; sysUpTime must parse as a number", i)
		}
	}
	// The very first sample has nothing to compare against; every later one
	// must carry a derived delta and rate, which is the whole reason the clock
	// moved into Go.
	if got[1].Delta == nil || *got[1].Delta <= 0 {
		t.Errorf("second sample delta = %v, want a positive value", got[1].Delta)
	}
	if got[1].Rate == nil || *got[1].Rate <= 0 {
		t.Errorf("second sample rate = %v, want a positive value", got[1].Rate)
	}
	if got[0].SnmpType == "" {
		t.Error("the SNMP type was not carried through; delta correction depends on it")
	}
	t.Logf("%d samples, type=%s, delta=%v rate=%v/s", len(got), got[0].SnmpType, *got[1].Delta, *got[1].Rate)
}

// An unreachable target must produce recorded failures rather than silence:
// that is what reachability alerting is built on.
func TestIntegrationUnreachableTargetIsRecorded(t *testing.T) {
	_, port := agentAddr(t)

	client := snmp.NewClient(context.Background())
	fetch := func(_ context.Context, oid string, targets []string) []monitor.Reading {
		out := []monitor.Reading{}
		for _, r := range client.Get(targets, oid, "public", "v2c", port, 1, 0, snmp.V3Params{}) {
			out = append(out, monitor.Reading{Target: r.Target, Error: r.Error})
		}
		return out
	}

	var mu sync.Mutex
	var points []monitor.Point
	s := monitor.NewScheduler()
	s.Persist = func(p []monitor.Point) {
		mu.Lock()
		defer mu.Unlock()
		points = append(points, p...)
	}

	// 192.0.2.0/24 is TEST-NET-1: reserved for documentation, so it is
	// guaranteed not to answer.
	s.Start(monitor.SessionSpec{
		ID: "unreachable", OIDs: []string{sysUpTime}, Targets: []string{"192.0.2.1"},
		Interval: 500 * time.Millisecond, Fetch: fetch,
	})
	defer s.StopAll()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(points)
		mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(points) == 0 {
		t.Fatal("an unreachable target produced no sample at all; nothing would ever alert")
	}
	if points[0].Error == "" {
		t.Errorf("expected a recorded error, got %+v", points[0])
	}
	if points[0].Value != nil {
		t.Error("a failed poll must not carry a value")
	}
}
