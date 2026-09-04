package notify

import (
	"errors"
	"net"
	"net/http"
	"net/textproto"
	"strings"
	"time"
)

// MaxAttempts before a delivery becomes a dead letter.
const MaxAttempts = 6

// Backoff returns how long to wait before attempt n (1-based), capped.
//
// Exponential with a cap rather than unbounded: a desktop app is routinely
// offline for hours, and a delivery whose next attempt is a day away is
// indistinguishable from a lost one.
func Backoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	d := time.Duration(1<<uint(attempt-1)) * 15 * time.Second
	if d > 15*time.Minute {
		d = 15 * time.Minute
	}
	return d
}

// Permanent reports whether an error is worth giving up on immediately rather
// than retrying six times.
//
// The distinction matters: retrying a bad password six times can lock an
// account, whereas retrying a refused connection is exactly right.
func Permanent(err error) bool {
	if err == nil {
		return false
	}
	// A network error is by definition worth another try.
	var netErr net.Error
	if errors.As(err, &netErr) {
		return false
	}
	// A reply code the peer actually SENT, in preference to anything it wrote.
	// The error text carries whatever the peer said — an SMTP greeting, an
	// HTTP error page — and matching words in it gave the receiver a vote on
	// whether its own outage was worth retrying. "454 4.7.0 Temporary
	// authentication failure" contains "authentication"; a 503 from a load
	// balancer routinely contains "invalid".
	var smtpErr *textproto.Error
	if errors.As(err, &smtpErr) {
		// RFC 5321 4.2.1: 4yz is transient and the client should try again;
		// 5yz is permanent. Retrying bad credentials six times just locks the
		// account, and giving up on an overloaded relay loses the incident.
		return smtpErr.Code >= 500
	}

	var httpErr *HTTPStatusError
	if errors.As(err, &httpErr) {
		switch httpErr.Code {
		case http.StatusRequestTimeout, http.StatusTooEarly, http.StatusTooManyRequests:
			// The receiver asked to be tried again, in those words.
			return false
		}
		// A 4xx means this request will never be accepted; a 5xx is the
		// receiver's problem and is usually over by the next attempt.
		return httpErr.Code >= 400 && httpErr.Code < 500
	}

	// Everything left is ours: a configuration that cannot produce a request at
	// all. Retrying an empty host six times helps nobody.
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "is empty"),
		strings.Contains(msg, "incomplete"),
		strings.Contains(msg, "unknown smtp auth"),
		strings.Contains(msg, "not a valid"),
		strings.Contains(msg, "redirects are not followed"):
		return true
	}
	return false
}
