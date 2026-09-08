package snmp

import (
	"fmt"
	"sync"
	"time"

	"github.com/gosnmp/gosnmp"
)

// Get performs a concurrent SNMP GET operation on multiple targets.
func (c *Client) Get(targets []string, oid, community, version string, port, timeoutSec, retries int, v3 V3Params) []*BulkResult {
	return concurrentExecute(targets, func(t string) *BulkResult {
		start := time.Now()
		res, err := c.getSingle(t, oid, community, version, port, timeoutSec, retries, v3)
		elapsed := time.Since(start).Milliseconds()
		if err != nil {
			return &BulkResult{Target: t, Error: err.Error(), ResponseTimeMs: elapsed}
		}
		return &BulkResult{Target: t, Result: res, ResponseTimeMs: elapsed}
	})
}

func (c *Client) getSingle(target, oid, community, version string, port, timeoutSec, retries int, v3 V3Params) (*Result, error) {
	g, err := c.newGoSNMP(target, community, version, port, timeoutSec, retries, v3)
	if err != nil {
		return nil, err
	}
	if err = g.Connect(); err != nil {
		return nil, fmt.Errorf("connect failed: %v", err)
	}
	defer g.Conn.Close()

	result, err := g.Get([]string{oid})
	if err != nil {
		return nil, fmt.Errorf("get failed: %v", err)
	}
	if len(result.Variables) == 0 {
		return nil, fmt.Errorf("no result returned")
	}
	v := result.Variables[0]
	return &Result{Oid: v.Name, Type: v.Type.String(), Value: formatSnmpValue(v)}, nil
}

// GetNext performs a concurrent SNMP GETNEXT operation on multiple targets.
func (c *Client) GetNext(targets []string, oid, community, version string, port, timeoutSec, retries int, v3 V3Params) []*BulkResult {
	return concurrentExecute(targets, func(t string) *BulkResult {
		start := time.Now()
		res, err := c.getNextSingle(t, oid, community, version, port, timeoutSec, retries, v3)
		elapsed := time.Since(start).Milliseconds()
		if err != nil {
			return &BulkResult{Target: t, Error: err.Error(), ResponseTimeMs: elapsed}
		}
		return &BulkResult{Target: t, Result: res, ResponseTimeMs: elapsed}
	})
}

func (c *Client) getNextSingle(target, oid, community, version string, port, timeoutSec, retries int, v3 V3Params) (*Result, error) {
	g, err := c.newGoSNMP(target, community, version, port, timeoutSec, retries, v3)
	if err != nil {
		return nil, err
	}
	if err = g.Connect(); err != nil {
		return nil, fmt.Errorf("connect failed: %v", err)
	}
	defer g.Conn.Close()

	result, err := g.GetNext([]string{oid})
	if err != nil {
		return nil, fmt.Errorf("getnext failed: %v", err)
	}
	if len(result.Variables) == 0 {
		return nil, fmt.Errorf("no result returned")
	}
	v := result.Variables[0]
	return &Result{Oid: v.Name, Type: v.Type.String(), Value: formatSnmpValue(v)}, nil
}

// GetBulk performs a concurrent SNMP GETBULK operation on multiple targets.
func (c *Client) GetBulk(targets []string, oid, community, version string, port, timeoutSec, retries, nonRepeaters, maxRepetitions int, v3 V3Params) []*BulkResult {
	return concurrentExecute(targets, func(t string) *BulkResult {
		start := time.Now()
		res, err := c.getBulkSingle(t, oid, community, version, port, timeoutSec, retries, nonRepeaters, maxRepetitions, v3)
		elapsed := time.Since(start).Milliseconds()
		if err != nil {
			return &BulkResult{Target: t, Error: err.Error(), ResponseTimeMs: elapsed}
		}
		res.ResponseTimeMs = elapsed
		return res
	})
}

func (c *Client) getBulkSingle(target, oid, community, version string, port, timeoutSec, retries, nonRepeaters, maxRepetitions int, v3 V3Params) (*BulkResult, error) {
	g, err := c.newGoSNMP(target, community, version, port, timeoutSec, retries, v3)
	if err != nil {
		return nil, err
	}
	if err = g.Connect(); err != nil {
		return nil, fmt.Errorf("connect failed: %v", err)
	}
	defer g.Conn.Close()

	if g.Version == gosnmp.Version1 {
		return nil, fmt.Errorf("GETBULK is not supported for SNMP v1")
	}

	result, err := g.GetBulk([]string{oid}, uint8(nonRepeaters), uint32(maxRepetitions))
	if err != nil {
		return nil, fmt.Errorf("getbulk failed: %v", err)
	}

	results := make([]*Result, 0, len(result.Variables))
	for _, variable := range result.Variables {
		results = append(results, &Result{
			Oid:   variable.Name,
			Type:  variable.Type.String(),
			Value: formatSnmpValue(variable),
		})
	}

	iResults := make([]interface{}, len(results))
	for i, r := range results {
		iResults[i] = r
	}

	return &BulkResult{
		Target: target,
		Result: &Result{Oid: oid, Type: "GetBulkResponse", Value: iResults},
	}, nil
}

// Set performs a concurrent SNMP SET operation on multiple targets.
func (c *Client) Set(targets []string, oid, community, value, valueType, version string, port, timeoutSec, retries int, v3 V3Params) []*BulkResult {
	return concurrentExecute(targets, func(t string) *BulkResult {
		start := time.Now()
		res, err := c.setSingle(t, oid, community, value, valueType, version, port, timeoutSec, retries, v3)
		elapsed := time.Since(start).Milliseconds()
		if err != nil {
			return &BulkResult{Target: t, Error: err.Error(), ResponseTimeMs: elapsed}
		}
		return &BulkResult{Target: t, Result: res, ResponseTimeMs: elapsed}
	})
}

func (c *Client) setSingle(target, oid, community, value, valueType, version string, port, timeoutSec, retries int, v3 V3Params) (*Result, error) {
	g, err := c.newGoSNMP(target, community, version, port, timeoutSec, retries, v3)
	if err != nil {
		return nil, err
	}
	if err = g.Connect(); err != nil {
		return nil, fmt.Errorf("connect failed: %v", err)
	}
	defer g.Conn.Close()

	// One mapping of SMI syntax to ASN.1 tag, shared with SetMultiple: two of
	// them drift, and a wrong tag comes back as wrongType with nothing saying
	// which mapping produced it.
	pdu, err := buildPDU(oid, value, valueType)
	if err != nil {
		return nil, err
	}

	packet, err := g.Set([]gosnmp.SnmpPDU{pdu})
	if err != nil {
		return nil, fmt.Errorf("set failed: %v", err)
	}
	if packet.Error != gosnmp.NoError {
		return nil, fmt.Errorf("set failed with error: %s", packet.Error.String())
	}
	v := packet.Variables[0]
	return &Result{Oid: v.Name, Type: v.Type.String(), Value: formatSnmpValue(v)}, nil
}

// Walk performs a concurrent SNMP WALK operation on multiple targets.
func (c *Client) Walk(targets []string, oid, community, version string, port, timeoutSec, retries int, v3 V3Params) []*BulkResult {
	return concurrentExecute(targets, func(t string) *BulkResult {
		start := time.Now()
		walkResult, err := c.walkSingle(t, oid, community, version, port, timeoutSec, retries, v3)
		elapsed := time.Since(start).Milliseconds()
		if err != nil {
			return &BulkResult{Target: t, Error: err.Error(), ResponseTimeMs: elapsed}
		}
		walkResult.ResponseTimeMs = elapsed
		return walkResult
	})
}

func (c *Client) walkSingle(target, oid, community, version string, port, timeoutSec, retries int, v3 V3Params) (*BulkResult, error) {
	g, err := c.newGoSNMP(target, community, version, port, timeoutSec, retries, v3)
	if err != nil {
		return nil, err
	}
	if err = g.Connect(); err != nil {
		return nil, fmt.Errorf("connect failed: %v", err)
	}
	defer g.Conn.Close()

	results := []*Result{}
	callback := func(pdu gosnmp.SnmpPDU) error {
		results = append(results, &Result{
			Oid:   pdu.Name,
			Type:  pdu.Type.String(),
			Value: formatSnmpValue(pdu),
		})
		return nil
	}

	if g.Version == gosnmp.Version2c || g.Version == gosnmp.Version3 {
		err = g.BulkWalk(oid, callback)
	} else {
		err = g.Walk(oid, callback)
	}
	if err != nil {
		return nil, fmt.Errorf("walk failed: %v", err)
	}

	return &BulkResult{
		Target: target,
		Result: &Result{Oid: oid, Type: "WalkResponse", Value: results},
	}, nil
}

// MultiResult is one target's answer to a batched GET: one entry per OID that
// was asked for, keyed by the OID as it was REQUESTED rather than as the agent
// spelled it back.
//
// Agents normalise: ask for `1.3.6.1.2.1.1.3.0` and the varbind can come back
// as `.1.3.6.1.2.1.1.3.0`. Keying by the reply would then miss every lookup,
// so the request order is what maps a varbind to its OID, with the name used
// only to detect a reordered or short reply.
type MultiResult struct {
	Target         string             `json:"target"`
	Results        map[string]*Result `json:"results"`
	Errors         map[string]string  `json:"errors,omitempty"`
	ResponseTimeMs int64              `json:"responseTimeMs"`
}

// maxVarbindsPerGet bounds one request.
//
// gosnmp refuses more than its MaxOids (60 by default) and its own comment
// says a high value "can cause remote devices to fail". 30 is half that: large
// enough that a realistic dashboard preset is one or two round trips, small
// enough that the PDU stays well inside any agent's reply buffer.
const maxVarbindsPerGet = 30

// GetMany reads several OIDs from several targets, one connection per target.
//
// This replaces a loop that called Get once per OID. That loop opened a fresh
// socket and asked for ONE varbind each time, and the OIDs were walked
// SERIALLY while only the targets ran concurrently — so a session with ten
// OIDs against an unreachable device spent ten full timeouts, one after
// another, on the poll clock that runs with no window open. The interruption
// check sat between OIDs, so a stop could not land inside that.
//
// Everything an agent is asked for now travels in one PDU per chunk: for the
// usual session that is one round trip per target instead of one per OID.
//
// A connection failure is reported PER OID rather than once for the target.
// The scheduler breaks a series on a failed reading, and a target-level error
// that produced no per-OID entries would leave every series looking as though
// nothing had been polled at all — which is indistinguishable from a paused
// session.
func (c *Client) GetMany(targets []string, oids []string, community, version string, port, timeoutSec, retries int, v3 V3Params) []*MultiResult {
	out := make([]*MultiResult, 0, len(targets))
	if len(oids) == 0 {
		return out
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, target := range targets {
		wg.Add(1)
		go func(t string) {
			defer wg.Done()
			m := c.getManyOne(t, oids, community, version, port, timeoutSec, retries, v3)
			mu.Lock()
			out = append(out, m)
			mu.Unlock()
		}(target)
	}
	wg.Wait()

	// Restore the caller's order: the goroutines finish in whatever order the
	// network allows, and a caller that zips this against its target list would
	// otherwise attribute one device's readings to another.
	byTarget := make(map[string]*MultiResult, len(out))
	for _, m := range out {
		byTarget[m.Target] = m
	}
	ordered := make([]*MultiResult, 0, len(targets))
	for _, t := range targets {
		if m, ok := byTarget[t]; ok {
			ordered = append(ordered, m)
		}
	}
	return ordered
}

func (c *Client) getManyOne(target string, oids []string, community, version string, port, timeoutSec, retries int, v3 V3Params) *MultiResult {
	start := time.Now()
	m := &MultiResult{
		Target:  target,
		Results: make(map[string]*Result, len(oids)),
		Errors:  make(map[string]string),
	}
	failAll := func(err error) *MultiResult {
		for _, oid := range oids {
			m.Errors[oid] = err.Error()
		}
		m.ResponseTimeMs = time.Since(start).Milliseconds()
		return m
	}

	g, err := c.newGoSNMP(target, community, version, port, timeoutSec, retries, v3)
	if err != nil {
		return failAll(err)
	}
	if err := g.Connect(); err != nil {
		return failAll(fmt.Errorf("connect failed: %v", err))
	}
	defer g.Conn.Close()

	for from := 0; from < len(oids); from += maxVarbindsPerGet {
		to := from + maxVarbindsPerGet
		if to > len(oids) {
			to = len(oids)
		}
		chunk := oids[from:to]

		packet, err := g.Get(chunk)
		if err != nil {
			for _, oid := range chunk {
				m.Errors[oid] = fmt.Sprintf("get failed: %v", err)
			}
			continue
		}
		for i, oid := range chunk {
			if i >= len(packet.Variables) {
				m.Errors[oid] = "no result returned"
				continue
			}
			v := packet.Variables[i]
			m.Results[oid] = &Result{Oid: v.Name, Type: v.Type.String(), Value: formatSnmpValue(v)}
		}
	}

	m.ResponseTimeMs = time.Since(start).Milliseconds()
	return m
}
