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
// Render produces the built-in subject and body.
//
// Kept as a wrapper so existing callers are unchanged. Note that it redacts by
// applying RedactEvent and then rendering, rather than masking inside the
// rendering: those were two code paths that happened to agree, and templates
// can reach fields only the second one covered.
func Render(e events.Event, redact bool) (subject, body string) {
	if redact {
		e = RedactEvent(e)
	}
	return renderDefault(e)
}

// renderDefault is the built-in rendering, used when a sink has no template.
// It takes an event that is already redacted if it needed to be.
func renderDefault(e events.Event) (subject, body string) {
	source := e.Source
	summary := e.Summary

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
// RedactEvent returns a copy with the target address masked everywhere it
// appears, for a sink whose Redact option is on.
//
// Rendering alone is not enough: the webhook sink embeds the whole event as
// JSON, so masking only the subject and body would send the real address in
// the structured payload of the very sink most likely to forward it onwards.
// The redaction is applied when the delivery is QUEUED, so what is stored is
// already what will be sent — a later change to the option cannot retroactively
// unmask an alert that has already gone out, and a queued row never holds an
// address the operator asked to hide.
func RedactEvent(e events.Event) events.Event {
	if e.Source == "" {
		return e
	}
	masked := redactAddress(e.Source)

	out := e
	out.Source = masked
	out.Summary = strings.ReplaceAll(e.Summary, e.Source, masked)
	out.DedupKey = strings.ReplaceAll(e.DedupKey, e.Source, masked)

	if len(e.Params) > 0 {
		// Copy: the caller's map is shared with the journal entry, which must
		// keep the real address.
		params := make(map[string]any, len(e.Params))
		for k, v := range e.Params {
			if str, ok := v.(string); ok {
				v = strings.ReplaceAll(str, e.Source, masked)
			}
			params[k] = v
		}
		out.Params = params
	}
	return out
}

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
