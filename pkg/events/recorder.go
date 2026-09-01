package events

// Recorder is how a producer hands an event to whatever durably stores and
// routes it. Producers (pkg/snmp, pkg/monitor) depend only on this interface,
// which is what keeps them from importing pkg/storage or pkg/notify.
//
// payload carries bulk detail — a trap's full varbind list, for instance — and
// is stored apart from the event row so listing the journal never has to read
// it. Pass "" when there is none.
type Recorder interface {
	Record(e Event, payload string) error
}

// Nop discards events. It is the zero value used before wiring is complete, so
// a producer can always call Record without a nil check — losing an event is
// strictly better than panicking inside a trap listener goroutine.
type Nop struct{}

// Record implements Recorder.
func (Nop) Record(Event, string) error { return nil }

// RecorderFunc adapts a plain function to Recorder.
type RecorderFunc func(Event, string) error

// Record implements Recorder.
func (f RecorderFunc) Record(e Event, payload string) error { return f(e, payload) }
