package snmp

import (
	"fmt"
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
