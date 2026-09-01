package notify

import (
	"errors"
	"net"
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
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "authentication"), strings.Contains(msg, "auth "),
		strings.Contains(msg, "535"), // SMTP: bad credentials
		strings.Contains(msg, "550"), // SMTP: mailbox unavailable
		strings.Contains(msg, "553"), // SMTP: bad address
		strings.Contains(msg, "invalid"),
		strings.Contains(msg, "is empty"),
		strings.Contains(msg, "incomplete"),
		strings.Contains(msg, "unknown smtp auth"),
		strings.Contains(msg, "returned 400"), strings.Contains(msg, "returned 401"),
		strings.Contains(msg, "returned 403"), strings.Contains(msg, "returned 404"),
		strings.Contains(msg, "returned 422"):
		return true
	}
	return false
}
