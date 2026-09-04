// Package notify routes journal events to outbound sinks: syslog, webhooks and
// email.
//
// Rules are a fixed-field struct, never a language. Four reasons, and they are
// constraints rather than taste: a rule must round-trip losslessly into a form
// (an operator who cannot see their rule in a form will not trust it); it is
// evaluated on the synchronous insert path so it must be incapable of hanging
// or backtracking; a rules engine in a project with this little test surface is
// a bug farm; and it must not become a code-execution surface in a tool that
// already holds network credentials. When the fixed fields prove too weak, the
// answer is one more field — or a webhook, evaluated on the user's own side.
package notify

import (
	"net/netip"
	"path"
	"strings"
	"time"

	"SnmpLens/pkg/events"
)

// Window is a wall-clock range, e.g. 22:00 to 07:00, and may wrap midnight.
type Window struct {
	From string `json:"from"` // "HH:MM", local time
	To   string `json:"to"`
}

// RouteMatch selects events. Within a field the values are OR'ed; across
// populated fields they are AND'ed. An empty field matches everything.
type RouteMatch struct {
	Categories  []string `json:"categories,omitempty"`
	Kinds       []string `json:"kinds,omitempty"`
	MinSeverity string   `json:"minSeverity,omitempty"`
	Sources     []string `json:"sources,omitempty"` // CIDR "10.0.0.0/8" or glob "sw-*"
	OIDPrefix   string   `json:"oidPrefix,omitempty"`
	SessionIDs  []string `json:"sessionIds,omitempty"`
	States      []string `json:"states,omitempty"`
	Contains    string   `json:"contains,omitempty"` // substring of the summary
	QuietHours  *Window  `json:"quietHours,omitempty"`
}

// Route binds a match to one or more sinks.
type Route struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Enabled  bool       `json:"enabled"`
	Priority int        `json:"priority"`
	Match    RouteMatch `json:"match"`
	SinkIDs  []string   `json:"sinkIds"`
	// Stop ends evaluation when this route matches, so a broad catch-all can
	// sit at the bottom without doubling every delivery above it.
	Stop bool `json:"stop"`
}

// matchesAny reports whether value equals one of the listed values. An empty
// list is "no constraint".
func matchesAny(list []string, value string) bool {
	if len(list) == 0 {
		return true
	}
	for _, v := range list {
		if v == value {
			return true
		}
	}
	return false
}

// matchesSource accepts a CIDR or a shell-style glob. Anything that parses as a
// prefix is treated as CIDR; everything else falls back to path.Match, whose
// syntax has no backtracking and therefore cannot hang.
func matchesSource(patterns []string, source string) bool {
	if len(patterns) == 0 {
		return true
	}
	addr, addrErr := netip.ParseAddr(source)
	for _, p := range patterns {
		if prefix, err := netip.ParsePrefix(p); err == nil {
			if addrErr == nil && prefix.Contains(addr) {
				return true
			}
			continue
		}
		if ok, err := path.Match(p, source); err == nil && ok {
			return true
		}
		if p == source {
			return true
		}
	}
	return false
}

// inWindow reports whether t falls inside w, which may wrap past midnight.
func inWindow(w *Window, t time.Time) bool {
	if w == nil || w.From == "" || w.To == "" {
		return false
	}
	from, okFrom := parseHHMM(w.From)
	to, okTo := parseHHMM(w.To)
	if !okFrom || !okTo {
		return false
	}
	minutes := t.Hour()*60 + t.Minute()
	if from <= to {
		return minutes >= from && minutes < to
	}
	// Wraps midnight: 22:00 -> 07:00.
	return minutes >= from || minutes < to
}

func parseHHMM(s string) (int, bool) {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != 2 {
		return 0, false
	}
	h, err1 := atoi(parts[0])
	m, err2 := atoi(parts[1])
	if !err1 || !err2 || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

func atoi(s string) (int, bool) {
	n := 0
	if s == "" {
		return 0, false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
	}
	return n, true
}

// oidHasPrefix reports whether oid sits at or under prefix, comparing
// SUB-IDENTIFIERS rather than characters.
//
// `strings.HasPrefix` reads a dotted-decimal OID as text, and the two disagree
// exactly where it matters: 1.3.6.1.2.1.2 is `interfaces` and 1.3.6.1.2.1.25 is
// `host` — a different subtree that merely starts with the same characters. A
// route meant for interface traps also fired for every host, printer and
// disk-storage event on the network, which is worse than not firing: the rule
// looks like it works.
//
// The leading dot is not part of the identifier. gosnmp hands back every
// varbind name with one (measured: ".1.3.6.1.6.3.1.1.4.1.0"), while the UI
// placeholder and everything a user types has none, so a trap rule matched
// nothing at all until both ends were trimmed.
func oidHasPrefix(oid, prefix string) bool {
	oid = strings.TrimLeft(oid, ".")
	prefix = strings.TrimLeft(prefix, ".")
	if oid == "" || prefix == "" {
		return prefix == ""
	}
	return oid == prefix || strings.HasPrefix(oid, prefix+".")
}

// Matches reports whether the event satisfies the rule at time now.
func (m RouteMatch) Matches(e events.Event, now time.Time) bool {
	if !matchesAny(m.Categories, e.Category) {
		return false
	}
	if !matchesAny(m.Kinds, e.Kind) {
		return false
	}
	if !matchesAny(m.SessionIDs, e.SessionID) {
		return false
	}
	if !matchesAny(m.States, e.State) {
		return false
	}
	if m.MinSeverity != "" {
		if events.ParseSeverity(e.Severity) < events.ParseSeverity(m.MinSeverity) {
			return false
		}
	}
	if !matchesSource(m.Sources, e.Source) {
		return false
	}
	if m.OIDPrefix != "" && !oidHasPrefix(e.OID, m.OIDPrefix) {
		return false
	}
	if m.Contains != "" && !strings.Contains(strings.ToLower(e.Summary), strings.ToLower(m.Contains)) {
		return false
	}
	// Quiet hours SUPPRESS delivery: inside the window the route does not match.
	if inWindow(m.QuietHours, now) {
		return false
	}
	return true
}

// Select returns the sinks an event should be delivered to, deduplicated and in
// route priority order. Routes are evaluated by ascending Priority, then by ID.
func Select(routes []Route, e events.Event, now time.Time) []string {
	ordered := make([]Route, 0, len(routes))
	ordered = append(ordered, routes...)
	// Simple insertion sort: rule counts are small and this keeps the ordering
	// obvious at a glance.
	for i := 1; i < len(ordered); i++ {
		for j := i; j > 0; j-- {
			a, b := ordered[j-1], ordered[j]
			if a.Priority < b.Priority || (a.Priority == b.Priority && a.ID <= b.ID) {
				break
			}
			ordered[j-1], ordered[j] = b, a
		}
	}

	seen := map[string]bool{}
	out := []string{}
	for _, r := range ordered {
		if !r.Enabled || !r.Match.Matches(e, now) {
			continue
		}
		for _, sinkID := range r.SinkIDs {
			if !seen[sinkID] {
				seen[sinkID] = true
				out = append(out, sinkID)
			}
		}
		if r.Stop {
			break
		}
	}
	return out
}
