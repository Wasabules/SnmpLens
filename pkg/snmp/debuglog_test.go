package snmp

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/gosnmp/gosnmp"
)

// The debug log used to carry the community string.
//
// gosnmp's SnmpPacket.SafeString formats "Community:%s" on every SENDING
// PACKET despite its name, and unmarshalling logs "Parsed community %s". Both
// landed in the ring buffer that SnmpGetDebugLog hands to the debug panel,
// where Anonymous Mode masked IP addresses and nothing else.

func TestScrubRemovesTheCommunityFromWhatGosnmpPrints(t *testing.T) {
	// The real shapes, copied from gosnmp v1.43.2 marshal.go.
	lines := []string{
		"SENDING PACKET: Version:2c, MsgFlags:NoAuthNoPriv, SecurityModel:UserSecurityModel, " +
			"SecurityParameters:, ContextEngineID:, ContextName:, Community:s3cr3t-community, " +
			"PDUType:GetRequest, MsgID:0, RequestID:1836429849, MsgMaxSize:0, Error:NoError, Variables:[]",
		"Parsed community s3cr3t-community",
	}
	for _, line := range lines {
		got := scrubSecrets(line, []string{"s3cr3t-community", "", ""})
		if strings.Contains(got, "s3cr3t-community") {
			t.Errorf("the community survived scrubbing:\n%s", got)
		}
		if !strings.Contains(got, redacted) {
			t.Errorf("nothing was marked as redacted:\n%s", got)
		}
	}
}

// A community this writer was never told about — a trap sender's, or one from
// a request built elsewhere — must still be removed.
func TestScrubRemovesAnUnknownCommunity(t *testing.T) {
	line := "SENDING PACKET: Version:1, Community:someone-elses, PDUType:GetRequest, MsgID:7"
	got := scrubSecrets(line, nil)
	if strings.Contains(got, "someone-elses") {
		t.Errorf("an unknown community survived: %s", got)
	}
	// The fields around it must survive, or the log stops being useful.
	for _, keep := range []string{"Version:1", "PDUType:GetRequest", "MsgID:7"} {
		if !strings.Contains(got, keep) {
			t.Errorf("%s was destroyed: %s", keep, got)
		}
	}
}

// v3 passphrases are not logged today. Scrubbing them anyway is what makes a
// gosnmp upgrade that starts logging them a non-event.
func TestScrubRemovesV3Passphrases(t *testing.T) {
	line := "SEND STORE SECURITY PARAMS from result: user snmplens auth authpass123 priv privpass123"
	got := scrubSecrets(line, []string{"public", "authpass123", "privpass123"})
	for _, secret := range []string{"authpass123", "privpass123"} {
		if strings.Contains(got, secret) {
			t.Errorf("%s survived: %s", secret, got)
		}
	}
	if !strings.Contains(got, "snmplens") {
		t.Errorf("the username was destroyed; it is not a credential: %s", got)
	}
}

// A one- or two-character community must not turn every matching letter in the
// log into [redacted] — the patterns already cover the lines that carry it.
func TestScrubDoesNotShredTheLogForAShortCommunity(t *testing.T) {
	line := "SENDING PACKET: Version:2c, Community:ab, PDUType:GetRequest, Variables:[]"
	got := scrubSecrets(line, []string{"ab"})
	if strings.Contains(got, "Community:ab") {
		t.Errorf("the community field was not redacted: %s", got)
	}
	if !strings.Contains(got, "PDUType:GetRequest") {
		t.Errorf("the log was shredded: %s", got)
	}
}

// End to end against a real agent: enable debug, do a GET, and read back the
// buffer the panel would show.
func TestDebugLogNeverHoldsTheCommunity(t *testing.T) {
	raw := os.Getenv("SNMPLENS_AGENT_PORT")
	if raw == "" {
		t.Skip("set SNMPLENS_AGENT_PORT to run against the simulator")
	}
	port, _ := strconv.Atoi(raw)

	c := NewClient(context.Background())
	c.SetDebugMode(true)
	defer c.SetDebugMode(false)

	const community = "s3cr3t-community"
	c.Get([]string{"127.0.0.1"}, "1.3.6.1.2.1.1.1.0", community, "v2c", port, 2, 0, V3Params{})

	entries := c.GetDebugLog()
	if len(entries) == 0 {
		t.Skip("the client logged nothing")
	}
	for _, e := range entries {
		if strings.Contains(e.Message, community) {
			t.Fatalf("the community reached the debug buffer: %s", e.Message)
		}
	}
	t.Logf("%d entries, none carrying the community", len(entries))
}

var _ = gosnmp.Version2c
