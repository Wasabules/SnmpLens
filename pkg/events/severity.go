package events

import "strings"

// Severity uses the X.733 ladder, which is what a network engineer already
// reads on their NMS. It is stored in SQLite as an INTEGER so that
// "severity >= ?" is a correct comparison: as TEXT, SQLite would order it
// lexicographically and 'critical' < 'info' < 'major' would silently return the
// wrong rows for any minimum-severity filter.
type Severity int

const (
	SevInfo     Severity = 1
	SevWarning  Severity = 2
	SevMinor    Severity = 3
	SevMajor    Severity = 4
	SevCritical Severity = 5
)

var severityNames = map[Severity]string{
	SevInfo:     "info",
	SevWarning:  "warning",
	SevMinor:    "minor",
	SevMajor:    "major",
	SevCritical: "critical",
}

// String returns the X.733 name used on the bridge and in sink output.
func (s Severity) String() string {
	if name, ok := severityNames[s]; ok {
		return name
	}
	return "info"
}

// Syslog maps to the RFC5424 severity level.
//
//	critical -> crit(2)  major -> err(3)  minor -> warning(4)
//	warning  -> notice(5)  info -> info(6)
func (s Severity) Syslog() int {
	switch s {
	case SevCritical:
		return 2
	case SevMajor:
		return 3
	case SevMinor:
		return 4
	case SevWarning:
		return 5
	default:
		return 6
	}
}

// ParseSeverity accepts an X.733 name (case-insensitive). Unknown input becomes
// info rather than an error: a severity is a display and routing concern, and
// refusing to record an event because its label was odd would be worse.
func ParseSeverity(name string) Severity {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "critical":
		return SevCritical
	case "major":
		return SevMajor
	case "minor":
		return SevMinor
	case "warning":
		return SevWarning
	default:
		return SevInfo
	}
}
