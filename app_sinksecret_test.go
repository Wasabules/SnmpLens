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

// Deleting a destination must unbind it from every rule that named it.
//
// The id was left in each route's SinkIDs: the rule then rendered a raw UUID in
// the list — sinkName() falls back to the id — and every matching event queued
// a delivery nothing could resolve. A rule pointing at one live destination and
// one dead id looks like it works, and half of it does not.
func TestDeletingASinkUnbindsItFromRoutes(t *testing.T) {
	a := newTestApp(t)

	gone, err := a.NotifySaveSink(notify.SinkConfig{
		Name: "old", Kind: notify.SinkWebhook, Enabled: true,
		Webhook: notify.WebhookConfig{URL: "https://a.example/x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	stays, err := a.NotifySaveSink(notify.SinkConfig{
		Name: "kept", Kind: notify.SinkWebhook, Enabled: true,
		Webhook: notify.WebhookConfig{URL: "https://b.example/x"},
	})
	if err != nil {
		t.Fatal(err)
	}

	both, err := a.storage.SaveRoute(notify.Route{
		Name: "both", Enabled: true, SinkIDs: []string{gone.ID, stays.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	only, err := a.storage.SaveRoute(notify.Route{
		Name: "only the one being deleted", Enabled: true, SinkIDs: []string{gone.ID},
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := a.NotifyDeleteSink(gone.ID); err != nil {
		t.Fatal(err)
	}

	routes, err := a.NotifyListRoutes()
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]notify.Route{}
	for _, r := range routes {
		byID[r.ID] = r
	}

	for _, r := range routes {
		for _, id := range r.SinkIDs {
			if id == gone.ID {
				t.Errorf("route %q still names the deleted destination", r.Name)
			}
		}
	}
	if got := byID[both.ID].SinkIDs; len(got) != 1 || got[0] != stays.ID {
		t.Errorf("the surviving destination was lost from the rule: %v", got)
	}
	if !byID[both.ID].Enabled {
		t.Error("a rule that still has a destination was disabled")
	}

	// A rule left with nothing is disabled, not deleted: it carries the match
	// conditions, which are the part that took thought.
	left, ok := byID[only.ID]
	if !ok {
		t.Fatal("the rule was deleted along with its destination")
	}
	if left.Enabled {
		t.Error("a rule with no destinations left is still enabled, so it matches " +
			"events and delivers to nothing")
	}
}

// A list binding that could not be read must be reported, not answered with an
// empty configuration that reads as "you have none".
func TestListingWithNoDatabaseIsAnError(t *testing.T) {
	a := &App{}

	if _, err := a.NotifyListSinks(); err == nil {
		t.Error("NotifyListSinks answered an empty list and no error, which the " +
			"settings page renders as a fresh install")
	}
	if _, err := a.NotifyListRoutes(); err == nil {
		t.Error("NotifyListRoutes answered an empty list and no error")
	}
	if _, err := a.NotifyListDeliveries("", 10); err == nil {
		t.Error("NotifyListDeliveries answered an empty list and no error")
	}
}
