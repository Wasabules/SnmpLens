package snmp

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/gosnmp/gosnmp"
)

// V3Params holds all security parameters for an SNMPv3 connection.
type V3Params struct {
	User        string `json:"User"`
	AuthProto   string `json:"AuthProto"`
	AuthPass    string `json:"AuthPass"`
	PrivProto   string `json:"PrivProto"`
	PrivPass    string `json:"PrivPass"`
	SecLevel    string `json:"SecLevel"`
	ContextName string `json:"ContextName"`
}

// Result represents the outcome of an SNMP operation.
type Result struct {
	Oid   string      `json:"oid"`
	Type  string      `json:"type"`
	Value interface{} `json:"value"`
}

// BulkResult wraps the result of an SNMP operation for a single target.
type BulkResult struct {
	Target         string  `json:"target"`
	Result         *Result `json:"result"`
	Error          string  `json:"error,omitempty"`
	ResponseTimeMs int64   `json:"responseTimeMs"`
}

// DebugEntry represents a single SNMP debug log entry.
type DebugEntry struct {
	Timestamp string `json:"timestamp"`
	Message   string `json:"message"`
}

// Client handles SNMP operations.
type Client struct {
	ctx          context.Context
	trapListener *gosnmp.TrapListener
	debugEnabled bool
	debugLog     []DebugEntry
	debugMu      sync.Mutex
}

// NewClient creates a new SNMP client.
func NewClient(ctx context.Context) *Client {
	return &Client{ctx: ctx}
}

// SetDebugMode enables or disables SNMP packet debug logging.
func (c *Client) SetDebugMode(enabled bool) {
	c.debugEnabled = enabled
}

// GetDebugLog returns a copy of the current debug log buffer.
func (c *Client) GetDebugLog() []DebugEntry {
	c.debugMu.Lock()
	defer c.debugMu.Unlock()
	out := make([]DebugEntry, len(c.debugLog))
	copy(out, c.debugLog)
	return out
}

// ClearDebugLog empties the debug log buffer.
func (c *Client) ClearDebugLog() {
	c.debugMu.Lock()
	defer c.debugMu.Unlock()
	c.debugLog = nil
}

// concurrentExecute runs fn for each target in parallel and collects BulkResults.
func concurrentExecute(targets []string, fn func(target string) *BulkResult) []*BulkResult {
	var wg sync.WaitGroup
	resultsChan := make(chan *BulkResult, len(targets))

	for _, target := range targets {
		wg.Add(1)
		go func(t string) {
			defer wg.Done()
			resultsChan <- fn(t)
		}(target)
	}

	wg.Wait()
	close(resultsChan)

	results := make([]*BulkResult, 0, len(targets))
	for res := range resultsChan {
		results = append(results, res)
	}
	return results
}

// newGoSNMP creates and configures a GoSNMP instance.
func (c *Client) newGoSNMP(target, community, version string, port, timeoutSec, retries int, v3 V3Params) (*gosnmp.GoSNMP, error) {
	g := &gosnmp.GoSNMP{
		Target:    target,
		Port:      uint16(port),
		Community: community,
		Timeout:   time.Duration(timeoutSec) * time.Second,
		Retries:   retries,
	}

	switch version {
	case "v1":
		g.Version = gosnmp.Version1
	case "v2c":
		g.Version = gosnmp.Version2c
	case "v3":
		g.Version = gosnmp.Version3
		g.ContextName = v3.ContextName

		secLevel, err := getSecurityLevel(v3.SecLevel)
		if err != nil {
			return nil, err
		}
		authProto, err := getAuthProtocol(v3.AuthProto)
		if err != nil {
			return nil, err
		}
		privProto, err := getPrivProtocol(v3.PrivProto)
		if err != nil {
			return nil, err
		}

		g.SecurityModel = gosnmp.UserSecurityModel
		g.MsgFlags = secLevel
		g.SecurityParameters = &gosnmp.UsmSecurityParameters{
			UserName:                 v3.User,
			AuthenticationProtocol:   authProto,
			AuthenticationPassphrase: v3.AuthPass,
			PrivacyProtocol:          privProto,
			PrivacyPassphrase:        v3.PrivPass,
		}
	default:
		return nil, fmt.Errorf("unsupported SNMP version: %s", version)
	}

	// Attach debug logger if enabled
	if c.debugEnabled {
		g.Logger = gosnmp.NewLogger(log.New(&ringLogWriter{client: c}, "", 0))
	}

	return g, nil
}

// ringLogWriter adapts ringLogger as io.Writer for log.New
type ringLogWriter struct {
	client *Client
}

func (w *ringLogWriter) Write(p []byte) (n int, err error) {
	msg := strings.TrimSpace(string(p))
	if msg == "" {
		return len(p), nil
	}
	w.client.debugMu.Lock()
	defer w.client.debugMu.Unlock()
	w.client.debugLog = append(w.client.debugLog, DebugEntry{
		Timestamp: time.Now().Format("15:04:05.000"),
		Message:   msg,
	})
	if len(w.client.debugLog) > 500 {
		w.client.debugLog = w.client.debugLog[len(w.client.debugLog)-500:]
	}
	return len(p), nil
}

// --- Security helpers ---

func getSecurityLevel(level string) (gosnmp.SnmpV3MsgFlags, error) {
	switch level {
	case "NoAuthNoPriv":
		return gosnmp.NoAuthNoPriv, nil
	case "AuthNoPriv":
		return gosnmp.AuthNoPriv, nil
	case "AuthPriv":
		return gosnmp.AuthPriv, nil
	default:
		return gosnmp.NoAuthNoPriv, fmt.Errorf("invalid security level: %s", level)
	}
}

func getAuthProtocol(proto string) (gosnmp.SnmpV3AuthProtocol, error) {
	switch strings.ToUpper(proto) {
	case "MD5":
		return gosnmp.MD5, nil
	case "SHA":
		return gosnmp.SHA, nil
	case "SHA224":
		return gosnmp.SHA224, nil
	case "SHA256":
		return gosnmp.SHA256, nil
	case "SHA384":
		return gosnmp.SHA384, nil
	case "SHA512":
		return gosnmp.SHA512, nil
	case "NONE", "":
		return gosnmp.NoAuth, nil
	default:
		return gosnmp.NoAuth, fmt.Errorf("invalid authentication protocol: %s", proto)
	}
}

func getPrivProtocol(proto string) (gosnmp.SnmpV3PrivProtocol, error) {
	switch strings.ToUpper(proto) {
	case "DES":
		return gosnmp.DES, nil
	case "AES", "AES128":
		return gosnmp.AES, nil
	case "AES192":
		return gosnmp.AES192, nil
	case "AES256":
		return gosnmp.AES256, nil
	case "AES192C":
		return gosnmp.AES192C, nil
	case "AES256C":
		return gosnmp.AES256C, nil
	case "NONE", "":
		return gosnmp.NoPriv, nil
	default:
		return gosnmp.NoPriv, fmt.Errorf("invalid privacy protocol: %s", proto)
	}
}

func formatSnmpValue(variable gosnmp.SnmpPDU) interface{} {
	switch variable.Type {
	case gosnmp.OctetString:
		return string(variable.Value.([]byte))
	case gosnmp.ObjectIdentifier:
		// OID values are strings (e.g. ".1.3.6.1.6.3.1.1.5.3") — return as-is
		return fmt.Sprintf("%v", variable.Value)
	case gosnmp.IPAddress:
		return fmt.Sprintf("%v", variable.Value)
	case gosnmp.NoSuchObject:
		return "noSuchObject"
	case gosnmp.NoSuchInstance:
		return "noSuchInstance"
	case gosnmp.EndOfMibView:
		return "endOfMibView"
	default:
		// Convert *big.Int to primitive types for reliable JSON serialization
		// through the Wails bridge. *big.Int can serialize as an object instead
		// of a number, breaking frontend numeric parsing and chart rendering.
		bi := gosnmp.ToBigInt(variable.Value)
		if bi.IsInt64() {
			return bi.Int64()
		}
		if bi.IsUint64() {
			return bi.Uint64()
		}
		// Fallback for extremely large values
		return bi.String()
	}
}
