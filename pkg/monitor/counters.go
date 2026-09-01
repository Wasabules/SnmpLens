package monitor

import (
	"math"
	"regexp"
	"strings"
	"time"
)

// Counter moduli. A Counter32 restarts at zero past 2^32, a Counter64 past
// 2^64.
const (
	Counter32Max float64 = 1 << 32
	Counter64Max float64 = 1 << 64
)

var counterRe = regexp.MustCompile(`(?i)counter`)

// IsCounterType reports whether an SNMP type name denotes a counter. Only
// counters wrap; a Gauge may legitimately decrease.
func IsCounterType(snmpType string) bool {
	return counterRe.MatchString(snmpType)
}

// CounterModulus returns the wrap point for a counter type.
//
// Note that the Counter64 value is beyond float64's exact-integer range, so
// wrap repair only ever succeeds for Counter32. That is by design rather than
// an oversight: a 64-bit octet counter takes decades to wrap, so a large
// negative jump on one is an agent reset, which CorrectedDelta reports as a gap.
func CounterModulus(snmpType string) float64 {
	if strings.Contains(snmpType, "64") {
		return Counter64Max
	}
	return Counter32Max
}

// CorrectedDelta is the change between two samples, with a counter wrap
// repaired.
//
// Subtracting blindly turns both a wrap and an agent reboot into an enormous
// negative spike that wrecks the chart scale and, worse, would fire a
// "below minimum" alert on a link that is perfectly healthy. A wrap is
// recoverable; a reset is not, and is reported as a gap (ok=false) rather than
// an invented number.
func CorrectedDelta(prev, current float64, snmpType string) (delta float64, ok bool) {
	d := current - prev
	if d >= 0 {
		return d, true
	}
	if !IsCounterType(snmpType) {
		return d, true // a gauge going down is not an anomaly
	}
	mod := CounterModulus(snmpType)
	wrapped := d + mod
	// A real wrap leaves a small positive delta. Anything approaching the
	// modulus means the counter was reset, not wrapped.
	if wrapped > 0 && wrapped < mod/2 {
		return wrapped, true
	}
	return 0, false
}

// ElapsedSeconds is the time that ACTUALLY passed between two samples.
//
// A rate must divide by this rather than by the configured interval: polling
// jitter, slow agents, retries and a suspended laptop all make the two differ,
// and the whole error lands in the rate.
func ElapsedSeconds(prev, current time.Time) (float64, bool) {
	dt := current.Sub(prev).Seconds()
	if dt > 0 && !math.IsInf(dt, 0) && !math.IsNaN(dt) {
		return dt, true
	}
	return 0, false
}
