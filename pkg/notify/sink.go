package notify

import (
	"os"

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
