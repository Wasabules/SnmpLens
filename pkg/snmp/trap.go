package snmp

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"SnmpLens/pkg/events"

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
func (c *Client) StartTrapListener(port int, v3 V3Params) error {
	if c.trapListener != nil {
		return fmt.Errorf("trap listener is already running")
	}

	params := &gosnmp.GoSNMP{
		Port:    uint16(port),
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

	c.trapListener = gosnmp.NewTrapListener()
	c.trapListener.OnNewTrap = c.handleTrap
	c.trapListener.Params = params
	c.trapListener.Params.Logger = gosnmp.NewLogger(log.New(os.Stdout, "", 0))

	go func() {
		defer func() {
			c.trapListener = nil
		}()
		log.Printf("Starting trap listener on port %d", port)
		err := c.trapListener.Listen(fmt.Sprintf("0.0.0.0:%d", port))
		if err != nil && !strings.Contains(err.Error(), "closed") {
			log.Printf("Error in trap listener: %v", err)
			runtime.EventsEmit(c.ctx, "trapError", fmt.Sprintf("Error in listener: %v", err))
		}
	}()
	return nil
}

// StopTrapListener stops the active trap listener.
func (c *Client) StopTrapListener() {
	if c.trapListener == nil {
		log.Println("Trap listener is not running, cannot stop.")
		return
	}
	log.Println("Stopping trap listener...")
	c.trapListener.Close()
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

// recordTrap writes a received trap to the event journal. The full varbind list
// goes to the payload side so listing the journal never reads it.
func (c *Client) recordTrap(source, ts, pduType string, packet *gosnmp.SnmpPacket, vars []Result) {
	kind := events.KindTrapReceived
	if pduType == "Inform" {
		kind = events.KindTrapInform
	}

	// A trap's identity for deduplication is its source plus its trap OID.
	trapOID := ""
	for _, v := range vars {
		if strings.Contains(v.Oid, "snmpTrapOID") {
			trapOID = fmt.Sprintf("%v", v.Value)
			break
		}
	}

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

// SendTrap sends an SNMP trap to a target.
func (c *Client) SendTrap(target string, port int, community, version, trapOid string, variables []TrapVariable) error {
	g := &gosnmp.GoSNMP{
		Target:    target,
		Port:      uint16(port),
		Community: community,
		Timeout:   5 * time.Second,
	}

	switch version {
	case "v1":
		g.Version = gosnmp.Version1
	case "v2c":
		g.Version = gosnmp.Version2c
	default:
		return fmt.Errorf("trap sending supports v1 and v2c only")
	}

	if err := g.Connect(); err != nil {
		return fmt.Errorf("connect failed: %v", err)
	}
	defer g.Conn.Close()

	trap := gosnmp.SnmpTrap{
		Variables: []gosnmp.SnmpPDU{},
	}

	if g.Version == gosnmp.Version2c {
		trap.Variables = append(trap.Variables, gosnmp.SnmpPDU{
			Name:  ".1.3.6.1.6.3.1.1.4.1.0",
			Type:  gosnmp.ObjectIdentifier,
			Value: trapOid,
		})
	} else {
		trap.Enterprise = trapOid
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

	_, err := g.SendTrap(trap)
	return err
}
