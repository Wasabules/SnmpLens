package monitor

import (
	"testing"
	"time"
)

func TestCorrectedDeltaPlainIncrease(t *testing.T) {
	d, ok := CorrectedDelta(100, 150, "Counter32")
	if !ok || d != 50 {
		t.Errorf("got (%v, %v), want (50, true)", d, ok)
	}
}

// The case the whole function exists for: a Counter32 rolling over must read as
// a small positive delta, not as a -4-billion spike.
func TestCorrectedDeltaRepairsA32BitWrap(t *testing.T) {
	prev := Counter32Max - 100
	d, ok := CorrectedDelta(prev, 50, "Counter32")
	if !ok || d != 150 {
		t.Errorf("got (%v, %v), want (150, true)", d, ok)
	}
}

// A Counter64 wrap is NOT repairable, and must not pretend to be.
//
// float64 carries 53 bits of mantissa, so every value within a few thousand of
// 2^64 rounds to 2^64 itself and the arithmetic that recovers a 32-bit wrap
// collapses to zero. That is acceptable because a 64-bit octet counter needs
// roughly 46 years at 100 Gbit/s to reach its wrap point: in practice a large
// negative jump on a Counter64 is an agent reboot, and reporting a gap is the
// truthful reading.
func TestCounter64DecreaseIsReportedAsAGap(t *testing.T) {
	if _, ok := CorrectedDelta(Counter64Max-1000, 500, "Counter64"); ok {
		t.Error("a Counter64 decrease must be a gap, not a fabricated delta")
	}
}

// An agent reboot is not a wrap. Reporting it as one would invent a gigantic
// delta; the honest answer is a gap.
// A counter sitting at a LOW value that drops cannot have wrapped — it would
// have had to travel nearly the whole 32-bit range between two polls. That is a
// reset, and the honest answer is a gap.
func TestCounterResetIsAGapNotASpike(t *testing.T) {
	if _, ok := CorrectedDelta(1000, 10, "Counter32"); ok {
		t.Error("a counter reset must be reported as a gap")
	}
}

// Conversely, a counter near the top of its range that reappears low DID wrap,
// and must yield the small positive delta rather than a gap.
func TestCounterNearTheTopIsTreatedAsAWrap(t *testing.T) {
	d, ok := CorrectedDelta(Counter32Max-1000, 500, "Counter32")
	if !ok || d != 1500 {
		t.Errorf("got (%v, %v), want (1500, true)", d, ok)
	}
}

// Gauges are allowed to go down, and must not be "corrected".
func TestGaugeDecreaseIsKept(t *testing.T) {
	d, ok := CorrectedDelta(80, 30, "Gauge32")
	if !ok || d != -50 {
		t.Errorf("got (%v, %v), want (-50, true)", d, ok)
	}
}

func TestCounterTypeDetectionIsCaseInsensitive(t *testing.T) {
	for _, s := range []string{"Counter32", "counter64", "COUNTER32"} {
		if !IsCounterType(s) {
			t.Errorf("%q should be a counter", s)
		}
	}
	if IsCounterType("Integer") || IsCounterType("") {
		t.Error("non-counters misdetected")
	}
}

func TestCounterModulus(t *testing.T) {
	if CounterModulus("Counter64") != Counter64Max {
		t.Error("Counter64 modulus")
	}
	if CounterModulus("Counter32") != Counter32Max {
		t.Error("Counter32 modulus")
	}
}

func TestElapsedSecondsRejectsNonProgress(t *testing.T) {
	now := time.Now()
	if _, ok := ElapsedSeconds(now, now); ok {
		t.Error("a zero interval must be rejected: it would divide by zero")
	}
	if _, ok := ElapsedSeconds(now, now.Add(-time.Second)); ok {
		t.Error("clock going backwards must be rejected")
	}
	dt, ok := ElapsedSeconds(now, now.Add(2500*time.Millisecond))
	if !ok || dt != 2.5 {
		t.Errorf("got (%v, %v), want (2.5, true)", dt, ok)
	}
}
