package snmp

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"time"

	"SnmpLens/pkg/events"
	"SnmpLens/pkg/netaddr"

	"github.com/gosnmp/gosnmp"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// TrapVariable represents a varbind to include in a sent trap.
type TrapVariable struct {
	Oid   string `json:"oid"`
	Type  string `json:"type"`
	Value string `json:"value"`
}

// StartTrapListener starts listening for SNMP traps, configured with v3 parameters.
// DefaultTrapPort is the IANA port for SNMP traps.
const DefaultTrapPort = 162

func (c *Client) StartTrapListener(port int, v3 V3Params) error {
	c.trapMu.Lock()
	defer c.trapMu.Unlock()

	if c.trapListener != nil {
		return fmt.Errorf("trap listener is already running")
	}

	params := &gosnmp.GoSNMP{
		Port:    normalisePort(port, DefaultTrapPort),
		Version: gosnmp.Version3,
	}

	if v3.User != "" {
		secLevel, err := getSecurityLevel(v3.SecLevel)
		if err != nil {
			return err
		}
		authProto, err := getAuthProtocol(v3.AuthProto)
		if err != nil {
			return err
		}
		privProto, err := getPrivProtocol(v3.PrivProto)
		if err != nil {
			return err
		}

		params.SecurityModel = gosnmp.UserSecurityModel
		params.MsgFlags = secLevel
		params.SecurityParameters = &gosnmp.UsmSecurityParameters{
			UserName:                 v3.User,
			AuthenticationProtocol:   authProto,
			AuthenticationPassphrase: v3.AuthPass,
			PrivacyProtocol:          privProto,
			PrivacyPassphrase:        v3.PrivPass,
		}
	} else {
		params.Version = gosnmp.Version2c
	}

	listener := gosnmp.NewTrapListener()
	listener.OnNewTrap = c.handleTrap
	listener.Params = params
	// Through the scrubbed ring buffer, and only when debug is on.
	//
	// This used to be log.New(os.Stdout, ...) unconditionally: not the ring
	// writer, so scrubSecrets never saw it, and not gated on debug, so every
	// arriving trap printed "Parsed community <whatever the sender used>" to
	// stdout whether or not anyone had asked for a debug log. A trap sender's
	// community is not ours to disclose, and stdout in a packaged app goes
	// somewhere nobody chose.
	if c.debugEnabled {
		listener.Params.Logger = gosnmp.NewLogger(log.New(&ringLogWriter{client: c}, "", 0))
	}

	// Enlarge the socket buffer as soon as it is bound.
	//
	// This is what decides whether a trap storm is recorded. Measured with a
	// burst of 2000 datagrams: 417 journalled with the system default, 2000
	// with 8 MiB. The loss is silent — the kernel drops the datagram before Go
	// sees it, so there is no error, no journal entry and nothing to count.
	//
	// Listening() closes once the socket is bound, which is the earliest moment
	// there is a socket to configure. In its own goroutine so a listener that
	// never binds cannot hold up the caller.
	c.trapListener = listener

	go func() {
		select {
		case <-listener.Listening():
			raiseTrapReadBuffer(listener, TrapReadBuffer)
		case <-time.After(5 * time.Second):
			log.Printf("trap listener: did not bind within 5s; leaving the default read buffer")
		}
	}()

	go func() {
		defer func() {
			// Clear it only if it is still OURS: a Stop followed by a Start can
			// have installed a new listener before this goroutine unwinds, and
			// setting nil unconditionally would strand that one with nothing
			// able to close it.
			c.trapMu.Lock()
			if c.trapListener == listener {
				c.trapListener = nil
			}
			c.trapMu.Unlock()
		}()
		log.Printf("Starting trap listener on port %d", port)
		err := listener.Listen(netaddr.ListenAddress(port))
		if err != nil && !strings.Contains(err.Error(), "closed") {
			log.Printf("Error in trap listener: %v", err)
			runtime.EventsEmit(c.ctx, "trapError", fmt.Sprintf("Error in listener: %v", err))
		}
	}()
	return nil
}

// StopTrapListener stops the active trap listener.
func (c *Client) StopTrapListener() {
	c.trapMu.Lock()
	listener := c.trapListener
	c.trapMu.Unlock()

	if listener == nil {
		log.Println("Trap listener is not running, cannot stop.")
		return
	}
	// Wait for the socket to exist before closing it.
	//
	// gosnmp's Close returns EARLY and does nothing when `conn` is still nil —
	// and it has already set `finish`, so it will never do anything on a second
	// call either. A stop landing in the window between StartTrapListener
	// returning and the socket being bound therefore reported success and left
	// a listener running with nothing able to stop it. Measured: still running
	// 2.1 s after Close returned in 73 ms.
	//
	// Listening() closes once the socket is bound. A bind that FAILS never
	// closes it, hence the bound wait: there is nothing to stop in that case
	// anyway.
	select {
	case <-listener.Listening():
	case <-time.After(stopBindGrace):
	}

	// Close OUTSIDE the lock: it waits for the listen goroutine to unwind, and
	// that goroutine takes the same lock to clear the field.
	log.Println("Stopping trap listener...")
	listener.Close()
}

// stopBindGrace is how long a stop waits for the socket to exist before giving
// up on closing it. Binding is a local syscall; anything slower than this has
// failed.
const stopBindGrace = 2 * time.Second

// TrapListenerRunning reports whether a listener is currently bound.
func (c *Client) TrapListenerRunning() bool {
	c.trapMu.Lock()
	defer c.trapMu.Unlock()
	return c.trapListener != nil
}

func (c *Client) handleTrap(packet *gosnmp.SnmpPacket, addr *net.UDPAddr) {
	log.Printf("Received trap from %s (Version: %s)", addr.IP.String(), packet.Version)

	vars := make([]Result, 0)

	if packet.Version == gosnmp.Version1 && packet.PDUType == gosnmp.Trap {
		vars = append(vars, Result{Oid: "snmpTrapOID.0", Type: "SNMPv1 Trap", Value: packet.Enterprise})
		vars = append(vars, Result{Oid: "genericTrap", Type: "INTEGER", Value: packet.GenericTrap})
		vars = append(vars, Result{Oid: "specificTrap", Type: "INTEGER", Value: packet.SpecificTrap})
		vars = append(vars, Result{Oid: "timestamp", Type: "TimeTicks", Value: packet.Timestamp})
		vars = append(vars, Result{Oid: "agentAddress", Type: "IPAddress", Value: packet.AgentAddress})
	}

	for _, variable := range packet.Variables {
		vars = append(vars, Result{
			Oid:   variable.Name,
			Type:  variable.Type.String(),
			Value: formatSnmpValue(variable),
		})
	}

	pduType := "Trap"
	if packet.PDUType == gosnmp.InformRequest {
		pduType = "Inform"
	}

	source := addr.IP.String()
	ts := time.Now().UTC().Format(time.RFC3339)

	trapData := map[string]interface{}{
		"source":    source,
		"version":   packet.Version.String(),
		"variables": vars,
		"pduType":   pduType,
		"timestamp": ts,
	}

	// Journal FIRST, emit second. EventsEmit only reaches a live webview: with
	// the window closed — or simply before the frontend has subscribed — every
	// received trap used to disappear with no error and no trace. Persisting
	// first is what makes background trap collection possible at all.
	c.recordTrap(source, ts, pduType, packet, vars)

	runtime.EventsEmit(c.ctx, "newTrap", trapData)
}

// snmpTrapOIDInstance is snmpTrapOID.0, the second varbind RFC 3416 requires in
// every v2c/v3 notification.
const snmpTrapOIDInstance = "1.3.6.1.6.3.1.1.4.1.0"

// trapOIDFrom picks the trap OID out of a notification's varbinds.
//
// This used to look for the TEXT "snmpTrapOID" in each varbind name, and gosnmp
// does no MIB translation — a varbind name is always numeric. Measured:
// Name=".1.3.6.1.6.3.1.1.4.1.0" contains snmpTrapOID = false. So EVERY v2c and
// v3 trap was journalled with an empty OID: an OID-prefix rule could not match
// one, {{trapOid}} rendered nothing, and every trap from a host shared a single
// dedup key regardless of what it reported.
//
// The name form is still accepted, because handleTrap synthesises exactly one
// varbind called "snmpTrapOID.0" for v1, which has no such varbind on the wire.
func trapOIDFrom(vars []Result) string {
	for _, v := range vars {
		name := strings.TrimLeft(v.Oid, ".")
		if name == snmpTrapOIDInstance || strings.HasPrefix(v.Oid, "snmpTrapOID") {
			return fmt.Sprintf("%v", v.Value)
		}
	}
	return ""
}

// recordTrap writes a received trap to the event journal. The full varbind list
// goes to the payload side so listing the journal never reads it.
func (c *Client) recordTrap(source, ts, pduType string, packet *gosnmp.SnmpPacket, vars []Result) {
	kind := events.KindTrapReceived
	if pduType == "Inform" {
		kind = events.KindTrapInform
	}

	// A trap's identity — for deduplication, for an OID-prefix route rule and
	// for {{trapOid}} in a message template — is its source plus its trap OID.
	trapOID := trapOIDFrom(vars)

	payload, err := json.Marshal(vars)
	if err != nil {
		payload = []byte("[]")
	}

	summary := fmt.Sprintf("%s from %s (%s, %d varbinds)", pduType, source,
		packet.Version.String(), len(vars))

	ev := events.Event{
		Ts:       ts,
		Category: events.CategoryTrap,
		Kind:     kind,
		Severity: events.SevMinor.String(),
		State:    events.StateOneshot,
		Source:   source,
		OID:      trapOID,
		DedupKey: "trap|" + source + "|" + trapOID,
		TitleKey: "events.kind." + kind,
		Params: map[string]any{
			"source":   source,
			"version":  packet.Version.String(),
			"pduType":  pduType,
			"varbinds": len(vars),
			"trapOid":  trapOID,
		},
		Summary: summary,
	}

	if err := c.recorder.Record(ev, string(payload)); err != nil {
		log.Printf("Failed to journal trap from %s: %v", source, err)
	}
}

// InformResult reports what came back from an INFORM.
//
// A trap is fire-and-forget; an INFORM is acknowledged, and the acknowledgement
// is the entire reason to send one. Reporting only "no error" would throw away
// the one thing that distinguishes it from a trap.
type InformResult struct {
	Acknowledged   bool   `json:"acknowledged"`
	ResponseTimeMs int64  `json:"responseTimeMs"`
	Error          string `json:"error,omitempty"`
}

// SendTrap sends an SNMP trap to a target.
func (c *Client) SendTrap(target string, port int, community, version, trapOid string, variables []TrapVariable) error {
	_, err := c.sendNotification(target, port, community, version, trapOid, variables, false)
	return err
}

// SendInform sends an INFORM and waits for the receiver to acknowledge it.
//
// v1 is refused rather than silently downgraded to a trap: RFC 1157 has no
// InformRequest PDU at all, so there is nothing to send, and a caller who
// asked for a confirmed notification must not be told one was delivered.
//
// The error names v2c and nothing else. It used to offer v3 as well, which
// sendNotification then rejects with "supports v1 and v2c only" — sending a
// caller who followed the advice straight into a contradicting error. The
// sender has never carried V3Params; when it does, this message can grow.
func (c *Client) SendInform(target string, port int, community, version, trapOid string, variables []TrapVariable) InformResult {
	start := time.Now()
	if version == "v1" {
		return InformResult{Error: "SNMPv1 has no INFORM: use v2c, or send a trap"}
	}
	acked, err := c.sendNotification(target, port, community, version, trapOid, variables, true)
	res := InformResult{Acknowledged: acked, ResponseTimeMs: time.Since(start).Milliseconds()}
	if err != nil {
		res.Error = err.Error()
	}
	return res
}

func (c *Client) sendNotification(target string, port int, community, version, trapOid string, variables []TrapVariable, inform bool) (bool, error) {
	g := &gosnmp.GoSNMP{
		Target:    netaddr.NormaliseTarget(target),
		Port:      normalisePort(port, DefaultTrapPort),
		Community: community,
		Timeout:   5 * time.Second,
	}

	switch version {
	case "v1":
		g.Version = gosnmp.Version1
	case "v2c":
		g.Version = gosnmp.Version2c
	default:
		return false, fmt.Errorf("trap sending supports v1 and v2c only")
	}

	if err := g.Connect(); err != nil {
		return false, fmt.Errorf("connect failed: %v", err)
	}
	defer g.Conn.Close()

	trap := gosnmp.SnmpTrap{
		Variables: []gosnmp.SnmpPDU{},
		IsInform:  inform,
	}

	if inform && g.Version == gosnmp.Version1 {
		return false, fmt.Errorf("SNMPv1 has no INFORM")
	}

	if g.Version == gosnmp.Version2c {
		trap.Variables = append(trap.Variables, gosnmp.SnmpPDU{
			Name:  ".1.3.6.1.6.3.1.1.4.1.0",
			Type:  gosnmp.ObjectIdentifier,
			Value: trapOid,
		})
	} else {
		trap.Enterprise = trapOid
		// agent-addr is a NetworkAddress in RFC1157, which is four octets and
		// has no IPv6 form at all. The wildcard says "look at the source
		// address of the datagram", which is the only honest answer over IPv6.
		trap.AgentAddress = "0.0.0.0"
		trap.GenericTrap = 6
		trap.SpecificTrap = 0
	}

	for _, v := range variables {
		pdu := gosnmp.SnmpPDU{Name: v.Oid}
		switch strings.ToLower(v.Type) {
		case "integer":
			val, _ := strconv.Atoi(v.Value)
			pdu.Type = gosnmp.Integer
			pdu.Value = val
		case "octetstring", "string":
			pdu.Type = gosnmp.OctetString
			pdu.Value = []byte(v.Value)
		case "oid", "objectidentifier":
			pdu.Type = gosnmp.ObjectIdentifier
			pdu.Value = v.Value
		case "timeticks":
			val, _ := strconv.ParseUint(v.Value, 10, 32)
			pdu.Type = gosnmp.TimeTicks
			pdu.Value = uint32(val)
		default:
			pdu.Type = gosnmp.OctetString
			pdu.Value = []byte(v.Value)
		}
		trap.Variables = append(trap.Variables, pdu)
	}

	packet, err := g.SendTrap(trap)
	if err != nil {
		return false, err
	}
	if !inform {
		return false, nil
	}
	// A response arrived. Whether it says yes is a separate question: an
	// unknown trap OID or a refused varbind comes back as an error status, and
	// treating that as delivered is exactly the mistake an INFORM exists to
	// prevent.
	if packet == nil {
		return false, fmt.Errorf("no response to the inform")
	}
	if packet.Error != gosnmp.NoError {
		return false, fmt.Errorf("the receiver refused the inform: %s", packet.Error.String())
	}
	return true, nil
}
