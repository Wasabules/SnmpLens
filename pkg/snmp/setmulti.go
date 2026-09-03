package snmp

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/gosnmp/gosnmp"
)

// SetVar is one varbind of a multi-varbind SET.
type SetVar struct {
	Oid   string `json:"oid"`
	Value string `json:"value"`
	Type  string `json:"type"`
}

// SetMultiple writes several varbinds in ONE PDU.
//
// This is not a convenience. Creating a conceptual row means setting its
// RowStatus to createAndGo together with every column the agent requires in
// the same request: RFC 2579 says an agent may refuse createAndGo when the row
// would be incomplete, and sending the columns one at a time gives it a row
// that is incomplete at every step. A SET is also atomic per RFC 3416 — all
// varbinds apply or none do — which one-at-a-time throws away, leaving half a
// row behind on the first failure.
func (c *Client) SetMultiple(targets []string, vars []SetVar, community, version string, port, timeoutSec, retries int, v3 V3Params) []*BulkResult {
	return concurrentExecute(targets, func(t string) *BulkResult {
		start := time.Now()
		res, err := c.setMultipleSingle(t, vars, community, version, port, timeoutSec, retries, v3)
		elapsed := time.Since(start).Milliseconds()
		if err != nil {
			return &BulkResult{Target: t, Error: err.Error(), ResponseTimeMs: elapsed}
		}
		return &BulkResult{Target: t, Result: res, ResponseTimeMs: elapsed}
	})
}

func (c *Client) setMultipleSingle(target string, vars []SetVar, community, version string, port, timeoutSec, retries int, v3 V3Params) (*Result, error) {
	if len(vars) == 0 {
		return nil, fmt.Errorf("no varbinds to set")
	}

	pdus := make([]gosnmp.SnmpPDU, 0, len(vars))
	for _, v := range vars {
		pdu, err := buildPDU(v.Oid, v.Value, v.Type)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", v.Oid, err)
		}
		pdus = append(pdus, pdu)
	}

	g, err := c.newGoSNMP(target, community, version, port, timeoutSec, retries, v3)
	if err != nil {
		return nil, err
	}
	if err = g.Connect(); err != nil {
		return nil, fmt.Errorf("connect failed: %v", err)
	}
	defer g.Conn.Close()

	packet, err := g.Set(pdus)
	if err != nil {
		return nil, fmt.Errorf("set failed: %v", err)
	}
	if packet.Error != gosnmp.NoError {
		// error-index is 1-based and names WHICH varbind was refused, which is
		// the only way to tell "the agent rejected this table" from "column 3
		// is read-only" when a row has a dozen of them.
		if idx := int(packet.ErrorIndex); idx >= 1 && idx <= len(vars) {
			return nil, fmt.Errorf("set failed on %s: %s", vars[idx-1].Oid, packet.Error.String())
		}
		return nil, fmt.Errorf("set failed with error: %s", packet.Error.String())
	}
	if len(packet.Variables) == 0 {
		return nil, fmt.Errorf("the agent returned no varbinds")
	}
	v := packet.Variables[0]
	return &Result{Oid: v.Name, Type: v.Type.String(), Value: formatSnmpValue(v)}, nil
}

// buildPDU maps a textual value and SMI type onto a varbind.
//
// Shared with the single-varbind SET rather than copied: two mappings of MIB
// syntax to ASN.1 tag are two mappings that drift, and a wrong tag is refused
// by the agent as wrongType with nothing saying which of the two was used.
func buildPDU(oid, value, valueType string) (gosnmp.SnmpPDU, error) {
	pdu := gosnmp.SnmpPDU{Name: oid}

	// An exact wire type first. pkg/mib works one out from the column's BASE
	// type, which is the only place the answer actually is; the substring
	// matching below is for the manual SET form, where the user picks a name
	// from a list and "Gauge32" arriving as INTEGER is refused as wrongType.
	if t, ok := wireTypes[valueType]; ok {
		v, err := encodeWire(t, value)
		if err != nil {
			return pdu, err
		}
		pdu.Type = t
		pdu.Value = v
		return pdu, nil
	}

	lowerType := strings.ToLower(valueType)

	switch {
	case strings.Contains(lowerType, "integer") || strings.Contains(lowerType, "gauge") ||
		strings.Contains(lowerType, "unsigned") || strings.Contains(lowerType, "counter32") ||
		strings.Contains(lowerType, "timeticks") || lowerType == "truthvalue" ||
		lowerType == "testandincr" || lowerType == "rowstatus" || lowerType == "storagetype":
		val, err := strconv.Atoi(value)
		if err != nil {
			return pdu, fmt.Errorf("value '%s' is not a valid integer", value)
		}
		pdu.Type = gosnmp.Integer
		pdu.Value = val
	case strings.Contains(lowerType, "counter64"):
		val, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return pdu, fmt.Errorf("value '%s' is not a valid counter64", value)
		}
		pdu.Type = gosnmp.Counter64
		pdu.Value = val
	case strings.Contains(lowerType, "ipaddress"):
		pdu.Type = gosnmp.IPAddress
		pdu.Value = value
	case strings.Contains(lowerType, "octet") || strings.Contains(lowerType, "string") ||
		strings.Contains(lowerType, "displaystring") || strings.Contains(lowerType, "hexstring"):
		pdu.Type = gosnmp.OctetString
		pdu.Value = []byte(value)
	case strings.Contains(lowerType, "objectidentifier") || strings.Contains(lowerType, "oid"):
		pdu.Type = gosnmp.ObjectIdentifier
		pdu.Value = value
	default:
		if val, err := strconv.Atoi(value); err == nil {
			pdu.Type = gosnmp.Integer
			pdu.Value = val
		} else {
			pdu.Type = gosnmp.OctetString
			pdu.Value = []byte(value)
		}
	}
	return pdu, nil
}

// RowStatus values from RFC 2579. Only the three a manager sends are named
// here; the others are states an agent reports.
const (
	RowStatusCreateAndGo   = 4
	RowStatusCreateAndWait = 5
	RowStatusDestroy       = 6
)

// wireTypes maps the exact names pkg/mib emits onto ASN.1 tags.
var wireTypes = map[string]gosnmp.Asn1BER{
	"Integer":     gosnmp.Integer,
	"Gauge32":     gosnmp.Gauge32,
	"Counter32":   gosnmp.Counter32,
	"Counter64":   gosnmp.Counter64,
	"TimeTicks":   gosnmp.TimeTicks,
	"IpAddress":   gosnmp.IPAddress,
	"OctetString": gosnmp.OctetString,
	// 0x44, not OpaqueFloat (0x78): Opaque wraps arbitrary bytes, and the
	// float variants are a different tag with a different payload.
	"Opaque":           gosnmp.Opaque,
	"ObjectIdentifier": gosnmp.ObjectIdentifier,
}

// encodeWire turns the text a person typed into the Go value gosnmp expects
// for that tag.
func encodeWire(t gosnmp.Asn1BER, value string) (interface{}, error) {
	switch t {
	case gosnmp.Integer:
		n, err := strconv.ParseInt(value, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("value %q is not an integer", value)
		}
		return int(n), nil
	case gosnmp.Gauge32, gosnmp.Counter32, gosnmp.TimeTicks:
		n, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("value %q is not an unsigned 32-bit number", value)
		}
		return uint32(n), nil
	case gosnmp.Counter64:
		n, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("value %q is not an unsigned 64-bit number", value)
		}
		return n, nil
	case gosnmp.IPAddress:
		if net.ParseIP(value) == nil {
			return nil, fmt.Errorf("value %q is not an IP address", value)
		}
		return value, nil
	case gosnmp.ObjectIdentifier:
		return value, nil
	}
	return []byte(value), nil
}
