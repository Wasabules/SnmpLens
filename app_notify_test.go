package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"SnmpLens/pkg/notify"
	"SnmpLens/pkg/secrets"
	"SnmpLens/pkg/storage"
)

// newTestApp wires just enough of App to exercise the notification bindings:
// the storage and the secret store, with no GUI and no Wails context.
func newTestApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()

	st, err := storage.Init(filepath.Join(dir, "monitoring.db"))
	if err != nil {
		t.Fatalf("storage.Init: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	sec, err := secrets.Open(dir)
	if err != nil {
		t.Fatalf("secrets.Open: %v", err)
	}

	return &App{storage: st, secrets: sec}
}

// Saving a sink must move the credential out of the configuration and into the
// secret store, and never hand it back.
func TestNotifySaveSinkMovesTheCredentialOut(t *testing.T) {
	a := newTestApp(t)

	saved, err := a.NotifySaveSink(notify.SinkConfig{
		Name: "NOC", Kind: notify.SinkWebhook, Enabled: true,
		Webhook: notify.WebhookConfig{URL: "https://hooks.example.com/x"},
		Secret:  "TOP-SECRET-TOKEN",
	})
	if err != nil {
		t.Fatalf("NotifySaveSink: %v", err)
	}
	if saved.Secret != "" {
		t.Error("the credential was handed back to the caller")
	}
	if !saved.HasSecret {
		t.Error("HasSecret is false, so the UI would show the sink as unconfigured")
	}
	if got := a.sinkSecret(saved.ID); got != "TOP-SECRET-TOKEN" {
		t.Errorf("the credential did not reach the secret store: %q", got)
	}

	list, err := a.NotifyListSinks()
	if err != nil {
		t.Fatalf("NotifyListSinks: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("got %d sinks", len(list))
	}
	if list[0].Secret != "" || list[0].Webhook.Token != "" {
		t.Error("a credential came back through the listing")
	}
	if !list[0].HasSecret {
		t.Error("HasSecret is false in the listing")
	}
}

// Testing an ALREADY-SAVED sink must authenticate with its stored credential.
//
// It did not: notify.Build captures the secret by value, and the lookup ran
// after the sink had already been built with an empty one. The symptom was the
// confusing kind — the Test button failed while the real alerts arrived.
func TestNotifyTestSinkUsesTheStoredCredential(t *testing.T) {
	a := newTestApp(t)

	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(200)
	}))
	defer srv.Close()

	saved, err := a.NotifySaveSink(notify.SinkConfig{
		Name: "NOC", Kind: notify.SinkWebhook, Enabled: true,
		Webhook: notify.WebhookConfig{URL: srv.URL},
		Secret:  "STORED-TOKEN",
	})
	if err != nil {
		t.Fatalf("NotifySaveSink: %v", err)
	}

	// The renderer never receives the secret, so this is exactly what the UI
	// sends when the user presses Test on a saved sink: no credential.
	if saved.Secret != "" {
		t.Fatal("this test is meaningless if the caller still holds the secret")
	}
	if err := a.NotifyTestSink(saved); err != nil {
		t.Fatalf("NotifyTestSink: %v", err)
	}
	if gotAuth != "Bearer STORED-TOKEN" {
		t.Errorf("the test request authenticated with %q; the stored credential was not used", gotAuth)
	}
}

// A credential typed into the form but not yet saved must be used as-is, so
// the button can validate a new sink before it is created.
func TestNotifyTestSinkUsesAnUnsavedCredential(t *testing.T) {
	a := newTestApp(t)

	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(200)
	}))
	defer srv.Close()

	err := a.NotifyTestSink(notify.SinkConfig{
		Kind: notify.SinkWebhook,
		Webhook: notify.WebhookConfig{URL: srv.URL},
		Secret:  "TYPED-TOKEN",
	})
	if err != nil {
		t.Fatalf("NotifyTestSink: %v", err)
	}
	if gotAuth != "Bearer TYPED-TOKEN" {
		t.Errorf("authenticated with %q, want the typed credential", gotAuth)
	}
}

// Clearing a credential must remove it, not leave the old one usable.
func TestNotifyClearSinkSecret(t *testing.T) {
	a := newTestApp(t)

	saved, err := a.NotifySaveSink(notify.SinkConfig{
		Name: "NOC", Kind: notify.SinkWebhook,
		Webhook: notify.WebhookConfig{URL: "https://hooks.example.com/x"},
		Secret:  "STORED-TOKEN",
	})
	if err != nil {
		t.Fatalf("NotifySaveSink: %v", err)
	}
	if err := a.NotifyClearSinkSecret(saved.ID); err != nil {
		t.Fatalf("NotifyClearSinkSecret: %v", err)
	}
	if got := a.sinkSecret(saved.ID); got != "" {
		t.Errorf("the credential survived being cleared: %q", got)
	}
	list, _ := a.NotifyListSinks()
	if len(list) == 1 && list[0].HasSecret {
		t.Error("the UI would still show a credential on file")
	}
}

// Saving a sink again without supplying a credential must keep the stored one,
// or every edit to an unrelated field would silently disarm the sink.
func TestNotifySaveSinkKeepsTheCredentialOnEdit(t *testing.T) {
	a := newTestApp(t)

	saved, err := a.NotifySaveSink(notify.SinkConfig{
		Name: "NOC", Kind: notify.SinkWebhook,
		Webhook: notify.WebhookConfig{URL: "https://hooks.example.com/x"},
		Secret:  "STORED-TOKEN",
	})
	if err != nil {
		t.Fatalf("NotifySaveSink: %v", err)
	}

	saved.Name = "NOC renamed"
	saved.Secret = "" // the renderer never has the credential to send back
	again, err := a.NotifySaveSink(saved)
	if err != nil {
		t.Fatalf("second NotifySaveSink: %v", err)
	}
	if !again.HasSecret {
		t.Error("HasSecret went false after an unrelated edit")
	}
	if got := a.sinkSecret(saved.ID); got != "STORED-TOKEN" {
		t.Errorf("the credential was lost on edit: %q", got)
	}
}
