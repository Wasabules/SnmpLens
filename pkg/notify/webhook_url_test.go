package notify

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"SnmpLens/pkg/events"
)

// For Slack, Teams and Discord — the exact receivers PayloadMode "template" was
// built for — the URL IS the credential. They authenticate by nothing else.
//
// {{secret}} was substituted into custom header values only, so those sinks had
// no path into pkg/secrets at all: the whole webhook URL went into the
// notify_sinks config blob in the clear, and a copied monitoring.db hands over a
// working post-anything-to-that-channel capability. Measured: a sink whose URL
// ended in "/services/{{secret}}" reached the receiver as
// "/services/%7B%7Bsecret%7D%7D" — the placeholder, percent-encoded.
func TestTheSecretPlaceholderWorksInTheURL(t *testing.T) {
	var mu sync.Mutex
	var gotPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotPath = r.URL.Path
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	sink := WebhookSink{
		Config: WebhookConfig{
			URL:   srv.URL + "/services/" + SecretPlaceholder,
			Token: "T000/B000/THE-REAL-TOKEN",
		},
	}
	if err := sink.Send(events.Event{ID: "e1"}, "s", "b"); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if want := "/services/T000/B000/THE-REAL-TOKEN"; gotPath != want {
		t.Errorf("the receiver was asked for %q, want %q", gotPath, want)
	}
}

// A URL with no placeholder is untouched, including one carrying characters
// that look like a placeholder to a careless matcher.
func TestAURLWithoutThePlaceholderIsUnchanged(t *testing.T) {
	var mu sync.Mutex
	var gotPath, gotQuery string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	sink := WebhookSink{Config: WebhookConfig{
		URL:   srv.URL + "/hooks/plain?tenant=acme&x={not-a-placeholder}",
		Token: "unused",
	}}
	if err := sink.Send(events.Event{ID: "e1"}, "s", "b"); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if gotPath != "/hooks/plain" {
		t.Errorf("path = %q", gotPath)
	}
	if !strings.Contains(gotQuery, "tenant=acme") {
		t.Errorf("query = %q", gotQuery)
	}
}

// A URL that carries the credential must be refused over plaintext http, the
// same as a bearer token or a header does. The guard read the token and the
// headers and never the URL, so the one place the credential is the whole
// address was the one place it could go out in the clear.
func TestAPlaceholderInTheURLIsRefusedOverPlaintextHTTP(t *testing.T) {
	sink := WebhookSink{Config: WebhookConfig{
		URL:   "http://hooks.example.com/services/" + SecretPlaceholder,
		Token: "THE-REAL-TOKEN",
	}}

	err := sink.Send(events.Event{ID: "e1"}, "s", "b")
	if err == nil {
		t.Fatal("the credential went out over plaintext http")
	}
	if !strings.Contains(err.Error(), "plaintext") {
		t.Errorf("refused for the wrong reason: %v", err)
	}
	if strings.Contains(err.Error(), "THE-REAL-TOKEN") {
		t.Errorf("the credential is in the error, which reaches the outbox: %v", err)
	}
}

// A receiver's error must not hand back the credential the URL carried.
func TestAURLEmbeddedCredentialIsScrubbedFromErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		// A receiver that echoes what it was asked for. Debug endpoints do.
		_, _ = w.Write([]byte("no such hook: " + r.URL.Path))
	}))
	defer srv.Close()

	sink := WebhookSink{Config: WebhookConfig{
		URL:   srv.URL + "/services/" + SecretPlaceholder,
		Token: "T000-THE-REAL-TOKEN",
	}}

	err := sink.Send(events.Event{ID: "e1"}, "s", "b")
	if err == nil {
		t.Fatal("a 400 was reported as a success")
	}
	if strings.Contains(err.Error(), "T000-THE-REAL-TOKEN") {
		t.Errorf("the credential reached the error, and from there "+
			"notify_outbox.last_error:\n%v", err)
	}
}
