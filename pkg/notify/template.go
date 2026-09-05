package notify

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"SnmpLens/pkg/events"
)

// Message templates let an operator decide what an alert mail actually says.
//
// The language is deliberately tiny: substitution, a fallback for an empty
// value, and a block that disappears when a value is absent. It is NOT Go's
// text/template. That would be less code here and worse everywhere else — it
// reaches arbitrary struct fields and methods on whatever it is handed, its
// errors are written for programmers, and a template is edited in a settings
// dialog by someone who is not one. A fixed vocabulary can be listed in the UI,
// checked when it is saved, and cannot reach a field nobody chose to expose.
//
// Two rules carry the safety of the whole thing:
//
//   - Substituted text is never re-scanned. A trap arrives from the network
//     unauthenticated; if its OID value is literally "{{secret}}" it comes out
//     as those nine characters, not as a lookup.
//   - The event is redacted BEFORE it gets here. Templates can name fields the
//     default rendering never showed, so masking has to happen upstream of the
//     template rather than inside it.

// MessageTemplate customises the rendered subject and body. An empty field
// falls back to the built-in rendering, so an unset template produces exactly
// what the sink sent before templates existed.
type MessageTemplate struct {
	Subject string `json:"subject,omitempty"`
	Body    string `json:"body,omitempty"`
}

// IsZero reports whether nothing is customised.
func (t MessageTemplate) IsZero() bool { return t.Subject == "" && t.Body == "" }

// Limits. The body cap is generous; the subject cap is enforced later, on the
// MIME-encoded form, because that is what actually goes on the wire.
const (
	MaxTemplateLen = 8 << 10
	MaxBodyLen     = 64 << 10
)

// TemplateError is one problem with a template, located so the editor can
// point at it.
type TemplateError struct {
	// Field is "subject" or "body".
	Field string `json:"field"`
	// Offset is the byte offset of the offending token.
	Offset int `json:"offset"`
	// Token is the text at fault, for the message.
	Token string `json:"token,omitempty"`
	// Message says what is wrong, in English; the UI shows it verbatim
	// because a translated parser error would drift from the parser.
	Message string `json:"message"`
}

func (e TemplateError) Error() string {
	return fmt.Sprintf("%s at offset %d: %s", e.Field, e.Offset, e.Message)
}

// VariableDoc describes one template variable for the settings UI. The list is
// generated here rather than mirrored in JavaScript, so the two cannot drift.
type VariableDoc struct {
	Name string `json:"name"`
	// Description is an i18n KEY suffix, not prose: the UI looks up
	// notify.tplVar.<name>. Keeping the English out of Go is what stops the
	// variable list from being the one part of the app that never translates.
	Description string `json:"description"`
	// Example is what this variable typically looks like.
	Example string `json:"example"`
	// Redacted marks variables that masking rewrites, so a redacting sink's
	// editor can show which ones still carry a real address.
	Redacted bool `json:"redacted"`
}

// reservedNames may never be a variable, nor a params key. Template text lives
// in notify_sinks.config and rendered output in notify_outbox, both readable in
// a copied monitoring.db; a template must never become a way to print a
// credential into either.
var reservedNames = map[string]bool{
	"secret": true, "password": true, "token": true, "community": true,
	"authpass": true, "privpass": true, "apikey": true,
}

// AppVersion is filled in once at startup. It is a package var for the same
// reason osHostname is: importing pkg/updater here would add an edge from the
// notification layer to the update checker for the sake of one string.
var AppVersion = ""

// templateVariables is the frozen vocabulary. Like pkg/events/kinds.go, names
// here are a compatibility promise: a template saved today must still render in
// two years, so a name may be added but never renamed or removed.
var templateVariables = []VariableDoc{
	{Name: "severity", Example: "major", Redacted: false},
	{Name: "severityUpper", Example: "MAJOR"},
	{Name: "severityNumber", Example: "4"},
	{Name: "category", Example: "threshold"},
	{Name: "kind", Example: "threshold.opened"},
	{Name: "state", Example: "open"},
	{Name: "summary", Example: "ifInOctets above 900 on 10.0.0.1", Redacted: true},
	{Name: "source", Example: "10.0.0.1", Redacted: true},
	{Name: "oid", Example: "1.3.6.1.2.1.2.2.1.10.1"},
	{Name: "sessionName", Example: "WAN Paris"},
	{Name: "sessionId", Example: "9f2c…"},
	{Name: "dedupKey", Example: "threshold|s1|10.0.0.1|1.3.6", Redacted: true},
	{Name: "corrId", Example: "3b81…"},
	{Name: "id", Example: "7c41…"},
	{Name: "seq", Example: "1042"},
	{Name: "ts", Example: "2026-09-01T09:12:44Z"},
	{Name: "tsLocal", Example: "Tue, 01 Sep 2026 11:12:44 CEST"},
	{Name: "value", Example: "912.5"},
	{Name: "hostname", Example: "workstation"},
	{Name: "appVersion", Example: "1.4.1"},
	{Name: "sinkName", Example: "NOC mail"},
	{Name: "lb", Example: "{{"},
}

// TemplateVariables returns the vocabulary for the settings UI.
func TemplateVariables() []VariableDoc {
	out := make([]VariableDoc, len(templateVariables))
	copy(out, templateVariables)
	for i := range out {
		out[i].Description = "notify.tplVar." + out[i].Name
	}
	return out
}

var knownVariables = func() map[string]bool {
	m := make(map[string]bool, len(templateVariables))
	for _, v := range templateVariables {
		m[v.Name] = true
	}
	return m
}()

// --- scanning ---------------------------------------------------------------

type tokenKind int

const (
	tokLiteral tokenKind = iota
	tokSubst
	tokBlockOpen
	tokBlockClose
)

type token struct {
	kind tokenKind
	// text is the literal for tokLiteral, otherwise the raw tag source, kept
	// so an unknown token can be emitted verbatim.
	text string
	name string
	// fallback is emitted when name resolves to empty. Only for tokSubst.
	fallback string
	hasFall  bool
	offset   int
	// ownLine marks a block tag that stood alone on its line, whose line is
	// removed. The trigger is the shape of the TEMPLATE, never the data: a
	// line vanishing because of a value is indistinguishable from a bug.
	ownLine bool
}

// scan turns template source into tokens. It never fails: anything malformed
// becomes a literal, so rendering always produces something. ValidateTemplate
// is what refuses malformed input, at save time, where a human can fix it.
func scan(src string) []token {
	var out []token
	var lit strings.Builder
	litStart := 0

	flush := func(at int) {
		if lit.Len() > 0 {
			out = append(out, token{kind: tokLiteral, text: lit.String(), offset: litStart})
			lit.Reset()
		}
		litStart = at
	}

	for i := 0; i < len(src); {
		if !strings.HasPrefix(src[i:], "{{") {
			lit.WriteByte(src[i])
			i++
			continue
		}
		end := strings.Index(src[i+2:], "}}")
		if end < 0 {
			// Unterminated: the rest is literal text.
			lit.WriteString(src[i:])
			break
		}
		raw := src[i : i+2+end+2]
		inner := src[i+2 : i+2+end]

		tok, ok := parseTag(inner, raw, i)
		if !ok {
			lit.WriteString(raw)
			i += len(raw)
			continue
		}
		flush(i)
		out = append(out, tok)
		i += len(raw)
		litStart = i
	}
	flush(len(src))
	return applyOwnLine(out)
}

// parseTag interprets the text between the braces.
func parseTag(inner, raw string, offset int) (token, bool) {
	trimmed := strings.Trim(inner, " \t")
	if trimmed == "" {
		return token{}, false
	}

	switch trimmed[0] {
	case '#', '/':
		name := strings.Trim(trimmed[1:], " \t")
		if !validName(name) {
			return token{}, false
		}
		kind := tokBlockOpen
		if trimmed[0] == '/' {
			kind = tokBlockClose
		}
		return token{kind: kind, text: raw, name: name, offset: offset}, true
	}

	// A fallback takes everything after the FIRST pipe, byte for byte, so a
	// default value may contain spaces and punctuation without quoting.
	if pipe := strings.Index(inner, "|"); pipe >= 0 {
		name := strings.Trim(inner[:pipe], " \t")
		if !validName(name) {
			return token{}, false
		}
		return token{
			kind: tokSubst, text: raw, name: name,
			fallback: inner[pipe+1:], hasFall: true, offset: offset,
		}, true
	}
	if !validName(trimmed) {
		return token{}, false
	}
	return token{kind: tokSubst, text: raw, name: trimmed, offset: offset}, true
}

// validName accepts a lowerCamelCase variable or params.<key>.
func validName(s string) bool {
	if rest, ok := strings.CutPrefix(s, "params."); ok {
		if rest == "" || len(rest) > 64 {
			return false
		}
		for _, r := range rest {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_') {
				return false
			}
		}
		return true
	}
	if s == "" || len(s) > 64 || !(s[0] >= 'a' && s[0] <= 'z') {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// applyOwnLine removes the line around a block tag that stands alone on it, so
// a conditional section does not leave a blank line behind when it renders and
// an orphan one when it does not.
func applyOwnLine(toks []token) []token {
	for i, t := range toks {
		if t.kind != tokBlockOpen && t.kind != tokBlockClose {
			continue
		}
		before, okBefore := trailingBlankToNewline(toks, i)
		after, okAfter := leadingBlankToNewline(toks, i)
		if !okBefore || !okAfter {
			continue
		}
		toks[i].ownLine = true
		if before >= 0 {
			toks[before].text = trimTrailingInlineSpace(toks[before].text)
		}
		if after >= 0 {
			toks[after].text = trimLeadingLine(toks[after].text)
		}
	}
	return toks
}

// trailingBlankToNewline reports whether everything between the previous
// newline (or the start) and this tag is blank.
func trailingBlankToNewline(toks []token, i int) (int, bool) {
	if i == 0 {
		return -1, true
	}
	prev := toks[i-1]
	if prev.kind != tokLiteral {
		return -1, false
	}
	nl := strings.LastIndexByte(prev.text, '\n')
	tail := prev.text[nl+1:]
	if strings.Trim(tail, " \t") != "" {
		return -1, false
	}
	if nl < 0 && i-1 != 0 {
		return -1, false
	}
	return i - 1, true
}

// leadingBlankToNewline reports whether everything from this tag to the next
// newline (or the end) is blank.
func leadingBlankToNewline(toks []token, i int) (int, bool) {
	if i == len(toks)-1 {
		return -1, true
	}
	next := toks[i+1]
	if next.kind != tokLiteral {
		return -1, false
	}
	nl := strings.IndexByte(next.text, '\n')
	head := next.text
	if nl >= 0 {
		head = next.text[:nl]
	}
	if strings.Trim(head, " \t") != "" {
		return -1, false
	}
	if nl < 0 && i+1 != len(toks)-1 {
		return -1, false
	}
	return i + 1, true
}

func trimTrailingInlineSpace(s string) string {
	return strings.TrimRight(s, " \t")
}

func trimLeadingLine(s string) string {
	if nl := strings.IndexByte(s, '\n'); nl >= 0 {
		return s[nl+1:]
	}
	return strings.TrimLeft(s, " \t")
}

// --- rendering --------------------------------------------------------------

// escapeFunc transforms a substituted VALUE before it joins the output. The
// literal text of the template is never touched: the operator wrote that on
// purpose, and escaping their punctuation would make a JSON template
// impossible to write.
type escapeFunc func(string) string

// jsonEscape renders a value as it would appear inside a JSON string, without
// the surrounding quotes.
//
// This is not decoration. A trap arrives from the network and its OID reaches
// the rendered body; one double quote in it turns a hand-written JSON payload
// into a parse error at the receiver, or worse, into a different document than
// the operator wrote. Same class of problem as dot-stuffing an SMTP body.
func jsonEscape(v string) string {
	encoded, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	// Marshal wraps the value in quotes; the template supplies its own.
	return string(encoded[1 : len(encoded)-1])
}

// htmlEscape renders a value as it would appear inside HTML text.
//
// Same reasoning as jsonEscape, one protocol along. An event's summary and its
// OID come off the network unauthenticated and reach the rendered body; in an
// HTML mail a single "<" turns the rest of the message into markup, and a
// crafted one turns it into a link somewhere else. The template's OWN markup is
// untouched — the operator wrote that on purpose, and escaping their tags would
// make an HTML template impossible to write.
func htmlEscape(v string) string {
	return html.EscapeString(v)
}

// xmlEscape renders a value as XML character data.
//
// xml.EscapeText, not html.EscapeString: they differ on exactly the characters
// that matter here. EscapeText encodes CR, LF and TAB as numeric references,
// which keeps a multi-line summary from breaking an attribute value, and it is
// the encoder the standard library will itself accept back.
func xmlEscape(v string) string {
	var b strings.Builder
	if err := xml.EscapeText(&b, []byte(v)); err != nil {
		return ""
	}
	return b.String()
}

// formEscape renders a value for application/x-www-form-urlencoded.
func formEscape(v string) string { return url.QueryEscape(v) }

// noEscape is the identity, for a body that is plain text.
//
// Named rather than passed as nil: renderWith treats nil as "no escaping" too,
// but a nil there reads as an oversight, and this one is a decision — plain
// text has no punctuation that a value can break.
func noEscape(v string) string { return v }

// renderTemplateText walks the tokens once.
func renderTemplateText(src string, lookup func(string) (string, bool), escape escapeFunc) string {
	var b strings.Builder
	toks := scan(src)

	skipUntil := "" // name of the block being skipped, "" when not skipping
	inBlock := ""

	for _, t := range toks {
		if skipUntil != "" {
			if t.kind == tokBlockClose && t.name == skipUntil {
				skipUntil = ""
			}
			continue
		}
		switch t.kind {
		case tokLiteral:
			b.WriteString(t.text)

		case tokSubst:
			value, known := lookup(t.name)
			if !known {
				// Emit the tag verbatim rather than blanking it. A blank looks
				// like a field that happened to be empty; "{{sevrity}}" in the
				// mail says plainly that the template has a typo.
				b.WriteString(t.text)
				continue
			}
			if value == "" && t.hasFall {
				b.WriteString(t.fallback)
				continue
			}
			if escape != nil {
				value = escape(value)
			}
			b.WriteString(value)

		case tokBlockOpen:
			value, known := lookup(t.name)
			if !known {
				b.WriteString(t.text)
				continue
			}
			inBlock = t.name
			if value == "" {
				skipUntil = t.name
			}

		case tokBlockClose:
			if inBlock == t.name {
				inBlock = ""
			} else {
				b.WriteString(t.text)
			}
		}
	}
	return b.String()
}

// RenderTemplate produces the subject and body for one event.
//
// The event must ALREADY be redacted if the sink requires it: templates can
// name fields the built-in rendering never showed, so masking belongs upstream.
// An empty field falls back to the built-in rendering, byte for byte.
func RenderTemplate(e events.Event, sinkName string, tpl MessageTemplate) (subject, body string) {
	return renderWith(e, sinkName, tpl, nil)
}

// DefaultJSONPayload is what a JSON webhook sends before anyone writes a
// template, and the starting point offered when the mode is switched on.
//
// It exists because the mode has to be STABLE: falling back to the built-in
// plain-text rendering would post prose to an endpoint expecting JSON, which
// fails at the receiver with a message nobody sees. Every field is a string —
// a numeric field would become `"value": ` and stop being JSON the moment an
// event arrives without one.
const DefaultJSONPayload = `{
  "severity": "{{severity}}",
  "category": "{{category}}",
  "kind": "{{kind}}",
  "source": "{{source}}",
  "oid": "{{oid}}",
  "value": "{{value}}",
  "summary": "{{summary}}",
  "time": "{{ts}}",
  "eventId": "{{id}}",
  "sender": "SnmpLens"
}`

// DefaultHTMLPayload is what an HTML mail sends before anyone writes a template.
//
// The same argument as DefaultJSONPayload: the format has to be STABLE. Falling
// back to the plain-text default would send prose with a text/html header, which
// a mail client renders as one unbroken paragraph — every newline in it means
// nothing, so a carefully laid-out alert arrives as a wall.
//
// Deliberately plain markup with inline styles and no <html> wrapper: mail
// clients strip <head>, ignore stylesheets, and Outlook renders tables more
// predictably than anything else. This is the shape that survives them.
const DefaultHTMLPayload = `<div style="font:14px/1.5 -apple-system,Segoe UI,Roboto,sans-serif;color:#14171a">
  <p style="margin:0 0 12px"><strong>{{severity}}</strong> — {{summary}}</p>
  <table cellpadding="0" cellspacing="0" style="border-collapse:collapse;font-size:13px">
    <tr><td style="padding:2px 12px 2px 0;color:#656d76">Device</td><td>{{source}}</td></tr>
    <tr><td style="padding:2px 12px 2px 0;color:#656d76">OID</td><td><code>{{oid}}</code></td></tr>
    <tr><td style="padding:2px 12px 2px 0;color:#656d76">Value</td><td>{{value}}</td></tr>
    <tr><td style="padding:2px 12px 2px 0;color:#656d76">Time</td><td>{{ts}}</td></tr>
  </table>
  <p style="margin:12px 0 0;font-size:12px;color:#656d76">Sent by SnmpLens &middot; event {{id}}</p>
</div>`

// RenderHTMLTemplate is RenderTemplate with every substituted value escaped as
// HTML text, for a mail sink whose body the operator writes as markup.
//
// An empty template renders DefaultHTMLPayload rather than the plain-text
// default, for the same reason the JSON mode does: the body is declared
// text/html, so it has to BE html whatever the operator has or has not written.
func RenderHTMLTemplate(e events.Event, sinkName string, tpl MessageTemplate) (subject, body string) {
	if strings.TrimSpace(tpl.Body) == "" {
		tpl.Body = DefaultHTMLPayload
	}
	return renderWith(e, sinkName, tpl, htmlEscape)
}

// RenderJSONTemplate is RenderTemplate with every substituted value escaped as
// a JSON string fragment, for a webhook whose body the operator writes as JSON.
//
// An empty template renders DefaultJSONPayload rather than the plain-text
// default: in this mode the body is the request, so it has to be JSON whatever
// the operator has or has not written.
func RenderJSONTemplate(e events.Event, sinkName string, tpl MessageTemplate) (subject, body string) {
	if strings.TrimSpace(tpl.Body) == "" {
		tpl.Body = DefaultJSONPayload
	}
	return renderWith(e, sinkName, tpl, jsonEscape)
}

func renderWith(e events.Event, sinkName string, tpl MessageTemplate, escape escapeFunc) (subject, body string) {
	lookup := func(name string) (string, bool) { return resolveVariable(e, sinkName, name) }

	defSubject, defBody := renderDefault(e)
	subject, body = defSubject, defBody

	if tpl.Subject != "" {
		// A subject is one line. Anything else is a mistake, and QEncoding
		// would turn it into =0D=0A noise rather than a header break.
		subject = collapseToOneLine(renderTemplateText(tpl.Subject, lookup, nil))
	}
	if tpl.Body != "" {
		body = truncateRunes(renderTemplateText(tpl.Body, lookup, escape), MaxBodyLen)
	}
	return subject, body
}

// collapseToOneLine folds any line break into a space.
func collapseToOneLine(s string) string {
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool {
		return r == '\r' || r == '\n'
	}), " ")
}

// truncateRunes cuts at a rune boundary and says that it did.
func truncateRunes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "\n[truncated]"
}

// resolveVariable is a switch, never reflection: a field is reachable because
// somebody listed it, not because it exists.
func resolveVariable(e events.Event, sinkName, name string) (string, bool) {
	if key, ok := strings.CutPrefix(name, "params."); ok {
		if reservedNames[strings.ToLower(key)] {
			return "", false
		}
		v, present := e.Params[key]
		if !present {
			return "", true // a known shape with no value: empty, not unknown
		}
		return stringifyParam(v), true
	}

	switch name {
	case "severity":
		return e.Severity, true
	case "severityUpper":
		return strings.ToUpper(e.Severity), true
	case "severityNumber":
		return strconv.Itoa(int(events.ParseSeverity(e.Severity))), true
	case "category":
		return e.Category, true
	case "kind":
		return e.Kind, true
	case "state":
		return e.State, true
	case "summary":
		return e.Summary, true
	case "source":
		return e.Source, true
	case "oid":
		return e.OID, true
	case "sessionName":
		return e.SessionName, true
	case "sessionId":
		return e.SessionID, true
	case "dedupKey":
		return e.DedupKey, true
	case "corrId":
		return e.CorrID, true
	case "id":
		return e.ID, true
	case "seq":
		if e.Seq == 0 {
			return "", true
		}
		return strconv.FormatInt(e.Seq, 10), true
	case "ts":
		return e.Ts, true
	case "tsLocal":
		if parsed, err := time.Parse(time.RFC3339, e.Ts); err == nil {
			return parsed.Local().Format(time.RFC1123), true
		}
		return e.Ts, true
	case "value":
		if e.Value == nil {
			return "", true
		}
		return fmt.Sprintf("%g", *e.Value), true
	case "hostname":
		h, err := osHostname()
		if err != nil {
			return "", true
		}
		return h, true
	case "appVersion":
		return AppVersion, true
	case "sinkName":
		return sinkName, true
	case "lb":
		return "{{", true
	default:
		return "", false
	}
}

// stringifyParam renders a scalar param.
//
// A composite renders empty on purpose, rather than with %v. RedactEvent walks
// only top-level string params, so printing a nested map or slice would put an
// address on the wire that the masker never had a chance to see. Params are
// flat today; this exists so that stops being load-bearing the first time a
// producer adds a structured one.
func stringifyParam(v any) string {
	switch n := v.(type) {
	case nil:
		return ""
	case string:
		return n
	case bool:
		return strconv.FormatBool(n)
	case int:
		return strconv.Itoa(n)
	case int32:
		return strconv.FormatInt(int64(n), 10)
	case int64:
		return strconv.FormatInt(n, 10)
	case uint:
		return strconv.FormatUint(uint64(n), 10)
	case uint32:
		return strconv.FormatUint(uint64(n), 10)
	case uint64:
		return strconv.FormatUint(n, 10)
	case float32:
		return strconv.FormatFloat(float64(n), 'g', -1, 64)
	case float64:
		return strconv.FormatFloat(n, 'g', -1, 64)
	default:
		return ""
	}
}

// --- validation -------------------------------------------------------------

// ValidateTemplate reports every problem, so the editor can show them all at
// once rather than one per save.
//
// Rendering deliberately cannot fail — it runs at enqueue time, where an error
// would lose the alert rather than spoil its formatting — so this is the only
// place a bad template is refused, and it has to be thorough.
func ValidateTemplate(tpl MessageTemplate) []TemplateError {
	errs := validateField("subject", tpl.Subject)
	errs = append(errs, validateField("body", tpl.Body)...)

	// A subject is one header line. Refusing a line break at save beats
	// discovering =0D=0A in an alert at 03:00.
	if i := strings.IndexAny(tpl.Subject, "\r\n"); i >= 0 {
		errs = append(errs, TemplateError{
			Field: "subject", Offset: i,
			Message: "a subject must be a single line",
		})
	}
	return errs
}

func validateField(field, src string) []TemplateError {
	var errs []TemplateError
	if src == "" {
		return nil
	}
	if len(src) > MaxTemplateLen {
		return []TemplateError{{
			Field: field, Offset: MaxTemplateLen,
			Message: fmt.Sprintf("a template may not exceed %d bytes", MaxTemplateLen),
		}}
	}

	openName, openOffset := "", 0

	for i := 0; i < len(src); {
		if !strings.HasPrefix(src[i:], "{{") {
			i++
			continue
		}
		end := strings.Index(src[i+2:], "}}")
		if end < 0 {
			errs = append(errs, TemplateError{
				Field: field, Offset: i, Token: "{{",
				Message: "this {{ is never closed with }}",
			})
			break
		}
		inner := src[i+2 : i+2+end]
		raw := src[i : i+2+end+2]
		i += len(raw)

		tok, ok := parseTag(inner, raw, 0)
		if !ok {
			errs = append(errs, TemplateError{
				Field: field, Offset: i - len(raw), Token: raw,
				Message: "not a valid tag; expected {{name}}, {{name|default}}, {{#name}} or {{/name}}",
			})
			continue
		}

		if tok.hasFall && strings.Contains(tok.fallback, "{{") {
			errs = append(errs, TemplateError{
				Field: field, Offset: i - len(raw), Token: raw,
				Message: "a default value cannot contain another tag",
			})
		}

		lower := strings.ToLower(tok.name)
		if reservedNames[lower] || reservedNames[strings.ToLower(strings.TrimPrefix(tok.name, "params."))] {
			errs = append(errs, TemplateError{
				Field: field, Offset: i - len(raw), Token: raw,
				Message: fmt.Sprintf("%q is reserved and can never be rendered", tok.name),
			})
			continue
		}

		if !strings.HasPrefix(tok.name, "params.") && !knownVariables[tok.name] {
			errs = append(errs, TemplateError{
				Field: field, Offset: i - len(raw), Token: raw,
				Message: fmt.Sprintf("unknown variable %q", tok.name),
			})
			continue
		}

		switch tok.kind {
		case tokBlockOpen:
			if openName != "" {
				errs = append(errs, TemplateError{
					Field: field, Offset: i - len(raw), Token: raw,
					Message: fmt.Sprintf("blocks cannot be nested; {{#%s}} is still open", openName),
				})
				continue
			}
			openName, openOffset = tok.name, i-len(raw)
		case tokBlockClose:
			if openName == "" {
				errs = append(errs, TemplateError{
					Field: field, Offset: i - len(raw), Token: raw,
					Message: "this closing tag has no matching opening tag",
				})
				continue
			}
			if openName != tok.name {
				errs = append(errs, TemplateError{
					Field: field, Offset: i - len(raw), Token: raw,
					Message: fmt.Sprintf("expected {{/%s}} to close the block opened here", openName),
				})
				continue
			}
			openName = ""
		}
	}

	if openName != "" {
		errs = append(errs, TemplateError{
			Field: field, Offset: openOffset, Token: "{{#" + openName + "}}",
			Message: fmt.Sprintf("this block is never closed with {{/%s}}", openName),
		})
	}
	return errs
}

// SampleEvent builds a realistic event for the template preview.
//
// Canned rather than synthesised from the journal: a preview must work on a
// fresh install with nothing in it, and must show a completed incident with
// every field populated — including the Params that {{params.*}} needs and
// that a real "test notification" event does not have.
func SampleEvent(kind string) events.Event {
	value := 912.5
	base := events.Event{
		Seq: 1042, ID: "7c41f0a2-3b81-4d6e-9f2c-5a0e1b7d8c34",
		Ts:          time.Now().UTC().Format(time.RFC3339),
		Severity:    "major",
		State:       events.StateOpen,
		Source:      "10.0.0.1",
		SessionName: "WAN Paris",
		SessionID:   "9f2c5a0e-1b7d-8c34-7c41-f0a23b814d6e",
		CorrID:      "3b814d6e-9f2c-5a0e-1b7d-8c347c41f0a2",
		Value:       &value,
	}

	switch kind {
	case events.CategoryTrap:
		base.Category, base.Kind = events.CategoryTrap, events.KindTrapReceived
		base.Severity, base.State, base.Value = "minor", events.StateOneshot, nil
		base.OID = "1.3.6.1.6.3.1.1.5.3"
		base.Summary = "Trap from 10.0.0.1 (Version2c, 4 varbinds)"
		base.DedupKey = "trap|10.0.0.1|1.3.6.1.6.3.1.1.5.3"
		base.Params = map[string]any{
			"source": "10.0.0.1", "version": "Version2c", "pduType": "Trap", "varbinds": 4,
		}
	case events.CategoryReachability:
		base.Category, base.Kind = events.CategoryReachability, events.KindReachabilityDown
		base.Severity, base.Value = "critical", nil
		base.Summary = "10.0.0.1 stopped answering in session WAN Paris"
		base.DedupKey = "reach|9f2c|10.0.0.1"
		base.Params = map[string]any{"source": "10.0.0.1", "session": "WAN Paris"}
	case events.CategorySystem:
		// A first-class routable category with no sample: the default branch
		// returned a THRESHOLD event, so ValidatePayloadTemplate never checked a
		// template against the shape system events actually have, and the
		// preview showed the operator a threshold alert when they asked for a
		// system one. A real dead letter leaves 8 of the 22 variables empty —
		// appVersion, corrId, dedupKey, oid, sessionId, sessionName, source,
		// value — and a template using any of them saves cleanly, then renders
		// invalid JSON the first time a delivery fails.
		base.Category, base.Kind = events.CategorySystem, events.KindSystemSinkDeadLetter
		base.Severity, base.State, base.Value = "major", events.StateOneshot, nil
		base.Source, base.SessionName, base.SessionID, base.CorrID = "", "", "", ""
		base.DedupKey = ""
		base.Summary = "Delivery to NOC webhook given up: webhook returned 503 Service Unavailable"
		base.Params = map[string]any{
			"sink": "NOC webhook", "attempts": 6,
			"error": "webhook returned 503 Service Unavailable",
		}
	default:
		base.Category, base.Kind = events.CategoryThreshold, events.KindThresholdOpened
		base.OID = "1.3.6.1.2.1.2.2.1.10.1"
		base.Summary = "ifInOctets reached 912.5, above 900, on 10.0.0.1"
		base.DedupKey = "threshold|9f2c|10.0.0.1|1.3.6.1.2.1.2.2.1.10.1"
		base.Params = map[string]any{
			"source": "10.0.0.1", "bound": 900, "held": 120, "oid": "1.3.6.1.2.1.2.2.1.10.1",
		}
	}
	base.TitleKey = "events.kind." + base.Kind
	return base
}
