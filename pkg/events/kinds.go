package events

import "fmt"

// Categories: the closed set the UI filters on and routing rules match against.
const (
	CategoryTrap         = "trap"
	CategoryThreshold    = "threshold"
	CategoryReachability = "reachability"
	CategorySystem       = "system"
	// CategoryOperation is opt-in and off by default: failed SNMP SETs, for
	// config-change auditing. It never carries a result payload.
	CategoryOperation = "operation"
)

// Kinds: stable machine keys. They are the routing vocabulary AND the i18n key
// suffix, so they must never be renamed once shipped — a stored event has to
// stay readable years later, including in a locale added since.
const (
	KindTrapReceived = "trap.received"
	KindTrapInform   = "trap.inform"

	KindThresholdOpened   = "threshold.opened"
	KindThresholdResolved = "threshold.resolved"

	KindReachabilityDown = "reachability.down"
	KindReachabilityUp   = "reachability.up"

	KindSystemListenerStarted = "system.listener_started"
	KindSystemListenerError   = "system.listener_error"
	KindSystemPollFailed      = "system.poll_failed"
	KindSystemSinkFailed      = "system.sink_failed"
	KindSystemSinkDeadLetter  = "system.sink_dead_letter"
	KindSystemMibLoadFailed   = "system.mib_load_failed"
	KindSystemUpdateAvailable = "system.update_available"
	KindSystemRetentionRan    = "system.retention_ran"
	// KindSystemInfo carries operational notices that are not failures:
	// sessions resumed at startup, background mode degraded to foreground.
	KindSystemInfo = "system.info"

	KindOperationFailed = "operation.failed"
)

// Episode state. An incident is a pair of events (opened then resolved) tied by
// CorrID; a oneshot has no natural end.
const (
	StateOneshot  = "oneshot"
	StateOpen     = "open"
	StateResolved = "resolved"
)

var categories = map[string]bool{
	CategoryTrap:         true,
	CategoryThreshold:    true,
	CategoryReachability: true,
	CategorySystem:       true,
	CategoryOperation:    true,
}

var states = map[string]bool{
	StateOneshot:  true,
	StateOpen:     true,
	StateResolved: true,
}

// Validate rejects an event that could not be filtered or routed correctly.
// It is intentionally shallow: it guards the closed sets, not the free text.
func Validate(e Event) error {
	if !categories[e.Category] {
		return fmt.Errorf("unknown event category %q", e.Category)
	}
	if e.Kind == "" {
		return fmt.Errorf("event kind is required")
	}
	if e.TitleKey == "" {
		return fmt.Errorf("event titleKey is required (kind %q)", e.Kind)
	}
	if e.State != "" && !states[e.State] {
		return fmt.Errorf("unknown event state %q", e.State)
	}
	return nil
}
