package notify

import (
	"encoding/base64"
	"errors"
	"os"
	"strings"

	"SnmpLens/pkg/events"
)

// Sink kinds.
const (
	SinkSyslog  = "syslog"
	SinkWebhook = "webhook"
	SinkEmail   = "email"
)

// Sink is one outbound destination.
type Sink interface {
	// Send delivers one event. subject and body are rendered by the caller at
	// enqueue time, so a sink never has to reach back into the journal.
	Send(e events.Event, subject, body string) error
	// Describe names the destination for the delivery log.
	Describe() string
}

// SinkConfig is the stored, serialisable description of a sink. Secrets are
// NOT held here: they are looked up separately by sink id, so a config can be
// exported or logged without leaking a password.
type SinkConfig struct {
	ID      string        `json:"id"`
	Name    string        `json:"name"`
	Kind    string        `json:"kind"`
	Enabled bool          `json:"enabled"`
	Syslog  SyslogConfig  `json:"syslog,omitzero"`
	Webhook WebhookConfig `json:"webhook,omitzero"`
	Email   EmailConfig   `json:"email,omitzero"`
	// Template customises the subject and body. It lives on SinkConfig rather
	// than on EmailConfig because the rendered body is also the syslog MSG and
	// the webhook's body field: one concept, one slot.
	Template MessageTemplate `json:"template,omitzero"`

	// Redact masks target addresses in outbound messages. Anonymous Mode is
	// renderer-only display masking and deliberately non-persistent, so it
	// cannot govern what a background dispatcher sends: this is its explicit,
	// per-sink counterpart.
	Redact bool `json:"redact"`

	// Secret is WRITE-ONLY transport: the UI sends a new password or token
	// here, the app hands it to pkg/secrets and clears the field before the
	// config is persisted. It is never populated when reading a sink back, so
	// a credential cannot leak through an export, a log line or the bridge.
	Secret string `json:"secret,omitempty"`
	// HasSecret tells the UI whether a credential is on file, so the form can
	// show "configured" without ever receiving the value.
	HasSecret bool `json:"hasSecret"`
}

var osHostname = os.Hostname

// Build turns a stored config into a live sink. secret is the sink's credential
// (SMTP password, bearer token), supplied by the caller from secure storage.
func Build(cfg SinkConfig, secret string) (Sink, bool) {
	// The stored credential always wins over anything left on the config: a
	// value that survived on the struct can only be stale, and preferring it
	// would silently keep using a password the operator has already changed.
	if secret == "" {
		secret = cfg.Secret
	}
	switch cfg.Kind {
	case SinkSyslog:
		sc := cfg.Syslog
		// For a syslog sink the credential is the mutual-TLS private key. A
		// sink has exactly one secret, so this reuses the same slot rather
		// than inventing a second storage path per kind.
		sc.ClientKey = secret
		return SyslogSink{Config: sc}, true
	case SinkWebhook:
		wc := cfg.Webhook
		wc.Token = secret
		return WebhookSink{Config: wc}, true
	case SinkEmail:
		ec := cfg.Email
		ec.Password = secret
		return EmailSink{Config: ec}, true
	default:
		return nil, false
	}
}

// scrubSecret removes a credential from an error before it leaves a sink.
//
// The error is stored in notify_outbox.last_error and surfaced in the event
// journal, so it OUTLIVES the request — and a receiver decides what its own
// error text says. A debug endpoint that echoes the request headers, or an
// SMTP server that quotes the AUTH line back, writes the credential into
// monitoring.db where it stays.
//
// Shared rather than per-sink: the webhook had one and the email sink had
// none, which is the shape a rule takes when each sink is asked to remember
// it separately.
func scrubSecret(err error, secret string) error {
	if err == nil || secret == "" {
		return err
	}
	msg := err.Error()
	cleaned := msg

	for _, form := range secretForms(secret) {
		cleaned = strings.ReplaceAll(cleaned, form, "[redacted]")
	}
	if cleaned == msg {
		return err
	}
	return errors.New(cleaned)
}

// secretForms is every spelling of a credential that can appear in an error.
//
// The literal is not enough. SMTP sends the password base64-encoded inside an
// AUTH line, and a server that quotes the line back in its refusal — which is
// entirely its choice — puts the credential in notify_outbox.last_error in a
// form a plain search walks straight past. Measured against a server doing
// exactly that: "535 rejected credentials: AUTH PLAIN
// AHVzZXIAaHVudGVyMi10aGUtc210cC1wYXNzd29yZA==", which decodes to the
// password.
func secretForms(secret string) []string {
	forms := []string{secret}

	// AUTH LOGIN sends the password on its own; AUTH PLAIN sends
	// \x00user\x00password, so the password's own encoding appears at an offset
	// and its middle survives base64's three-byte grouping. Cover the three
	// alignments rather than guess which one a server echoes.
	forms = append(forms, base64.StdEncoding.EncodeToString([]byte(secret)))
	for _, pad := range []string{"\x00", "\x00\x00"} {
		encoded := base64.StdEncoding.EncodeToString([]byte(pad + secret))
		// Drop the leading characters that encode the padding, keeping the
		// part that is stable wherever the secret sits.
		if len(encoded) > 4 {
			forms = append(forms, encoded[4:len(encoded)-4])
		}
	}

	out := forms[:0]
	for _, f := range forms {
		// A very short form would match ordinary text.
		if len(f) >= 6 {
			out = append(out, f)
		}
	}
	return out
}
