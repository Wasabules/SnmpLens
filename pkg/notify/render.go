package notify

import (
	"fmt"
	"strings"

	"SnmpLens/pkg/events"
)

// Render produces the subject and body a sink will send.
//
// This runs at ENQUEUE time, not at send time, so the queued row is
// self-contained. The consequence worth stating in the UI: editing a rule or a
// template does not change messages that are already queued.
//
// The text is English on purpose. The UI renders an event from its titleKey in
// the operator's locale, but a syslog collector or a mail archive is read by
// whoever is on call — often not the person who set the language — and the
// stored catalogue is in the frontend, unreachable from a background
// dispatcher.
func Render(e events.Event, redact bool) (subject, body string) {
	source := e.Source
	if redact {
		source = redactAddress(source)
	}

	summary := e.Summary
	if redact && e.Source != "" {
		summary = strings.ReplaceAll(summary, e.Source, source)
	}

	subject = fmt.Sprintf("[SnmpLens][%s] %s", strings.ToUpper(e.Severity), summary)

	var b strings.Builder
	b.WriteString(summary)
	b.WriteString("\n\n")
	writeField(&b, "Severity", e.Severity)
	writeField(&b, "Category", e.Category)
	writeField(&b, "Kind", e.Kind)
	writeField(&b, "State", e.State)
	writeField(&b, "Time", e.Ts)
	writeField(&b, "Source", source)
	writeField(&b, "OID", e.OID)
	writeField(&b, "Session", e.SessionName)
	if e.Value != nil {
		writeField(&b, "Value", fmt.Sprintf("%g", *e.Value))
	}
	writeField(&b, "Event ID", e.ID)
	if e.CorrID != "" {
		writeField(&b, "Correlation", e.CorrID)
	}
	return subject, b.String()
}

func writeField(b *strings.Builder, label, value string) {
	if value == "" {
		return
	}
	b.WriteString(label)
	b.WriteString(": ")
	b.WriteString(value)
	b.WriteString("\n")
}

// redactAddress masks the host part of an address, keeping enough shape for an
// operator to tell two devices apart.
//
// This is NOT Anonymous Mode: that is renderer-only display masking and is
// deliberately non-persistent, so it cannot govern what a background dispatcher
// puts on the wire. This is its explicit, per-sink counterpart.
func redactAddress(addr string) string {
	if addr == "" {
		return ""
	}
	if parts := strings.Split(addr, "."); len(parts) == 4 {
		return parts[0] + ".x.x." + parts[3]
	}
	if len(addr) <= 4 {
		return "***"
	}
	return addr[:2] + strings.Repeat("*", len(addr)-4) + addr[len(addr)-2:]
}
