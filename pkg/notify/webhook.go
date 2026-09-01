package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"SnmpLens/pkg/events"
)

// WebhookConfig describes an HTTP destination.
type WebhookConfig struct {
	URL     string            `json:"url"`
	Method  string            `json:"method"`  // defaults to POST
	Headers map[string]string `json:"headers"` // extra headers, e.g. a custom auth scheme
	// Token is json:"-": see EmailConfig.Password. It is supplied from
	// pkg/secrets, never persisted alongside the sink configuration.
	Token   string `json:"-"`       // sent as "Authorization: Bearer <token>"
	Timeout int    `json:"timeout"` // seconds; 0 means 10
}

// WebhookSink posts the event as JSON.
//
// This is also the escape hatch for logic the fixed-field rules cannot express:
// route broadly to a webhook and decide on your own side. It uses the default
// transport, so it honours HTTP_PROXY/HTTPS_PROXY — which makes it the sink
// most likely to work at all inside a locked-down corporate network.
type WebhookSink struct {
	Config WebhookConfig
}

type webhookBody struct {
	Event   events.Event `json:"event"`
	Subject string       `json:"subject"`
	Body    string       `json:"body"`
	Sender  string       `json:"sender"`
}

// Send posts one event.
func (w WebhookSink) Send(e events.Event, subject, body string) error {
	if strings.TrimSpace(w.Config.URL) == "" {
		return fmt.Errorf("webhook URL is empty")
	}

	payload, err := json.Marshal(webhookBody{Event: e, Subject: subject, Body: body, Sender: "SnmpLens"})
	if err != nil {
		return err
	}

	method := strings.ToUpper(strings.TrimSpace(w.Config.Method))
	if method == "" {
		method = http.MethodPost
	}

	timeout := time.Duration(w.Config.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	req, err := http.NewRequest(method, w.Config.URL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "SnmpLens")
	// The stable event id lets the receiver deduplicate: delivery is
	// at-least-once, never exactly-once.
	req.Header.Set("X-SnmpLens-Event-Id", e.ID)
	for k, v := range w.Config.Headers {
		req.Header.Set(k, v)
	}
	if w.Config.Token != "" {
		req.Header.Set("Authorization", "Bearer "+w.Config.Token)
	}

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// Drain a little of the body so the connection can be reused, and so the
	// error message says something useful.
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned %s: %s", resp.Status, strings.TrimSpace(string(snippet)))
	}
	return nil
}

// Describe names the destination for the delivery log.
func (w WebhookSink) Describe() string {
	return "webhook " + w.Config.URL
}
