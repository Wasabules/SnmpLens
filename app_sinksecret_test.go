package main

import (
	"strings"
	"testing"

	"SnmpLens/pkg/notify"
	"SnmpLens/pkg/secrets"
)

// Saving a credential with no store to put it in must FAIL, loudly.
//
// The guard read `if incoming != "" && a.secrets != nil`, so with no secret
// store — a locked keychain, a config directory that cannot be written — the
// typed token was silently dropped and the save reported success. The operator
// sees the destination saved, believes the token is held, and finds out at 03:00
// when the webhook posts unauthenticated or the SMTP AUTH is refused.
func TestSavingASecretWithNoStoreIsRefused(t *testing.T) {
	a := newTestApp(t)
	a.secrets = nil // as at startup when the protector could not be opened

	_, err := a.NotifySaveSink(notify.SinkConfig{
		Name: "NOC", Kind: notify.SinkWebhook, Enabled: true,
		Webhook: notify.WebhookConfig{URL: "https://hooks.example.com/x"},
		Secret:  "TOP-SECRET-TOKEN",
	})
	if err == nil {
		t.Fatal("the save reported success and discarded the credential")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "credential") {
		t.Errorf("the error does not say the credential is the problem: %v", err)
	}
}

// With no credential typed there is nothing to store, so no store is needed.
// A sink that authenticates with nothing must stay savable.
func TestSavingWithoutASecretNeedsNoStore(t *testing.T) {
	a := newTestApp(t)
	a.secrets = nil

	saved, err := a.NotifySaveSink(notify.SinkConfig{
		Name: "open relay", Kind: notify.SinkSyslog, Enabled: true,
		Syslog: notify.SyslogConfig{Address: "127.0.0.1:514", Protocol: "udp"},
	})
	if err != nil {
		t.Fatalf("a sink with no credential was refused: %v", err)
	}
	if saved.HasSecret {
		t.Error("HasSecret is true with no store and no secret")
	}
}

// Deleting a destination must take its credential with it.
//
// DeleteSink removed the row and left the secret in DPAPI, the Keychain or the
// file store forever — a bearer token or an SMTP password the operator believes
// they deleted, outliving the thing that named it, under a key nothing in the
// app will ever look up again.
func TestDeletingASinkRemovesItsCredential(t *testing.T) {
	a := newTestApp(t)

	saved, err := a.NotifySaveSink(notify.SinkConfig{
		Name: "NOC", Kind: notify.SinkWebhook, Enabled: true,
		Webhook: notify.WebhookConfig{URL: "https://hooks.example.com/x"},
		Secret:  "TOP-SECRET-TOKEN",
	})
	if err != nil {
		t.Fatal(err)
	}
	if a.sinkSecret(saved.ID) == "" {
		t.Fatal("the credential was never stored, so this proves nothing")
	}

	if err := a.NotifyDeleteSink(saved.ID); err != nil {
		t.Fatalf("NotifyDeleteSink: %v", err)
	}

	if got, _ := a.secrets.Get(secrets.SinkRef(saved.ID)); got != "" {
		t.Errorf("the credential outlived the destination: %q", got)
	}
}

// A sink that never had a credential must still delete cleanly: Delete on a ref
// that was never set must not be treated as a failure.
func TestDeletingASinkWithNoCredentialSucceeds(t *testing.T) {
	a := newTestApp(t)

	saved, err := a.NotifySaveSink(notify.SinkConfig{
		Name: "plain", Kind: notify.SinkSyslog, Enabled: true,
		Syslog: notify.SyslogConfig{Address: "127.0.0.1:514", Protocol: "udp"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.NotifyDeleteSink(saved.ID); err != nil {
		t.Errorf("deleting a sink with no credential failed: %v", err)
	}
}

// And with no secret store at all, deleting the destination must still work —
// the row is the user's actual intent.
func TestDeletingASinkWithNoStoreStillRemovesIt(t *testing.T) {
	a := newTestApp(t)
	saved, err := a.NotifySaveSink(notify.SinkConfig{
		Name: "plain", Kind: notify.SinkSyslog, Enabled: true,
		Syslog: notify.SyslogConfig{Address: "127.0.0.1:514", Protocol: "udp"},
	})
	if err != nil {
		t.Fatal(err)
	}
	a.secrets = nil

	if err := a.NotifyDeleteSink(saved.ID); err != nil {
		t.Errorf("the destination could not be deleted: %v", err)
	}
	sinks, err := a.storage.ListSinks()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sinks {
		if s.ID == saved.ID {
			t.Error("the destination is still there")
		}
	}
}
