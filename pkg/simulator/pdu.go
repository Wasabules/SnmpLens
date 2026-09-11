package simulator

import (
	"math"

	"github.com/gosnmp/gosnmp"
)

// maxBulk bounds the varbinds one GETBULK produces before it is cut to the
// message size. RFC 3416 4.2.3 lets an agent answer with fewer than asked, and
// max-repetitions is a 31-bit number a manager may set to anything.
const maxBulk = 1024

// answer is what a request earns: the varbinds to send back and, when the
// request as a whole failed, the error status and the 1-based index of the
// varbind that caused it.
type answer struct {
	vars   []gosnmp.SnmpPDU
	status gosnmp.SNMPError
	index  uint8
}

// process answers one request PDU from the tree: RFC 3416 4.2, and RFC 1157 4.1
// for SNMPv1, whose errors are different. It counts what SNMPv2-MIB's snmp
// group counts — the request on arrival, as net-snmp does, so that a GET of
// snmpInGetRequests counts itself.
func (a *Agent) process(ver gosnmp.SnmpVersion, req *gosnmp.SnmpPacket, c clock) answer {
	st := &a.stats
	switch req.PDUType {
	case gosnmp.GetRequest:
		st.inGets.Add(1)
	case gosnmp.GetNextRequest, gosnmp.GetBulkRequest:
		// The MIB has no counter of its own for GETBULK; net-snmp counts one
		// as a GETNEXT.
		st.inGetNexts.Add(1)
	case gosnmp.SetRequest:
		st.inSets.Add(1)
	}
	ans := a.dispatch(ver, req, c)
	if ans.status == gosnmp.NoError && req.PDUType != gosnmp.SetRequest {
		st.inTotalReqVars.Add(uint32(len(ans.vars)))
	}
	if ans.status == gosnmp.NoSuchName {
		st.outNoSuchNames.Add(1)
	}
	st.outGetResponses.Add(1)
	return ans
}

func (a *Agent) dispatch(ver gosnmp.SnmpVersion, req *gosnmp.SnmpPacket, c clock) answer {
	v1 := ver == gosnmp.Version1
	switch req.PDUType {
	case gosnmp.GetRequest:
		return a.get(v1, req.Variables, c)
	case gosnmp.GetNextRequest:
		return a.getNext(v1, req.Variables, c)
	case gosnmp.GetBulkRequest:
		return a.getBulk(req, c)
	}
	// A SET. Nothing in a simulated device is writable yet; SNMPv1 has no
	// notWritable, and RFC 3584 has a v1 agent say noSuchName instead.
	status := gosnmp.NotWritable
	if v1 {
		status = gosnmp.NoSuchName
	}
	return answer{vars: req.Variables, status: status, index: errIndex(0, len(req.Variables))}
}

// v1Hidden reports whether SNMPv1 cannot see e. v1 has no Counter64, and
// RFC 3584 has a v1 agent step over one on GETNEXT and answer noSuchName to a
// GET for it.
func v1Hidden(e *entry) bool { return e.typ == gosnmp.Counter64 }

func (a *Agent) get(v1 bool, vars []gosnmp.SnmpPDU, c clock) answer {
	out := make([]gosnmp.SnmpPDU, len(vars))
	for i, v := range vars {
		id, err := parseArcs(v.Name)
		var e *entry
		if err == nil {
			e = a.tree.get(id)
		}
		switch {
		case e != nil && !(v1 && v1Hidden(e)):
			out[i] = e.varbind(c)
		case v1:
			return answer{vars: vars, status: gosnmp.NoSuchName, index: errIndex(i, len(vars))}
		default:
			out[i] = gosnmp.SnmpPDU{Name: v.Name, Type: a.tree.missing(id)}
		}
	}
	return answer{vars: out}
}

func (a *Agent) getNext(v1 bool, vars []gosnmp.SnmpPDU, c clock) answer {
	var skip func(*entry) bool
	if v1 {
		skip = v1Hidden
	}
	out := make([]gosnmp.SnmpPDU, len(vars))
	for i, v := range vars {
		// A name that does not parse is placed before every object, so its
		// successor is the first one.
		id, _ := parseArcs(v.Name)
		switch e := a.tree.next(id, skip); {
		case e != nil:
			out[i] = e.varbind(c)
		case v1:
			return answer{vars: vars, status: gosnmp.NoSuchName, index: errIndex(i, len(vars))}
		default:
			out[i] = gosnmp.SnmpPDU{Name: v.Name, Type: gosnmp.EndOfMibView}
		}
	}
	return answer{vars: out}
}

// getBulk is RFC 3416 4.2.3: the first non-repeaters varbinds once each, as a
// GETNEXT, then the rest max-repetitions times, each repetition carrying on
// from the one before.
func (a *Agent) getBulk(req *gosnmp.SnmpPacket, c clock) answer {
	vars := req.Variables
	n := min(int(req.NonRepeaters), len(vars))
	out := a.getNext(false, vars[:n], c).vars
	repeaters := vars[n:]
	last := make([]oid, len(repeaters))
	names := make([]string, len(repeaters))
	for j, v := range repeaters {
		last[j], _ = parseArcs(v.Name)
		names[j] = v.Name
	}
	for rep := 0; rep < int(req.MaxRepetitions) && len(repeaters) > 0 && len(out)+len(repeaters) <= maxBulk; rep++ {
		ended := true
		for j := range repeaters {
			if e := a.tree.next(last[j], nil); e != nil {
				out = append(out, e.varbind(c))
				last[j], names[j], ended = e.oid, e.name, false
			} else {
				out = append(out, gosnmp.SnmpPDU{Name: names[j], Type: gosnmp.EndOfMibView})
			}
		}
		// Every column has run off the end: another repetition could only say
		// endOfMibView again.
		if ended {
			break
		}
	}
	return answer{vars: out}
}

// errIndex is the 1-based index of vars[i] as gosnmp carries it, in a byte: an
// error in the 256th varbind or later is reported against the 255th. A gosnmp
// manager sends at most 60 by default.
func errIndex(i, n int) uint8 {
	if n == 0 {
		return 0
	}
	return uint8(min(i+1, math.MaxUint8))
}

// bulkFloor is how far a GETBULK answer may be cut: down to its non-repeaters,
// not into them. Any other request may not be cut at all.
func bulkFloor(req *gosnmp.SnmpPacket) int {
	if req.PDUType != gosnmp.GetBulkRequest {
		return -1
	}
	return min(int(req.NonRepeaters), len(req.Variables))
}

// tooBig is the answer to a request whose response does not fit: an empty
// variable-bindings field (RFC 3416 4.2.1), or for SNMPv1 the request's own
// (RFC 1157 4.1.2).
func tooBig(ver gosnmp.SnmpVersion, req *gosnmp.SnmpPacket) answer {
	a := answer{status: gosnmp.TooBig}
	if ver == gosnmp.Version1 {
		a.vars = req.Variables
	}
	return a
}

// fit encodes ans within limit bytes.
//
// A GETBULK answer is cut to what fits, which RFC 3416 4.2.3 allows: the
// manager asks again from where the answer stopped. Anything else that does not
// fit earns tooBig, since a GET answered with varbinds missing would be an
// answer to a different question.
func fit(ans answer, floor int, big answer, limit int, encode func(answer) ([]byte, error)) ([]byte, error) {
	out, err := encode(ans)
	if err != nil || len(out) <= limit {
		return out, err
	}
	if floor >= 0 {
		// The longest prefix that fits. A binary search, because every probe
		// encodes the message again — and for v3 signs and encrypts it.
		var best []byte
		for lo, hi := floor, len(ans.vars)-1; lo <= hi; {
			mid := (lo + hi) / 2
			cut := ans
			cut.vars = ans.vars[:mid]
			b, err := encode(cut)
			if err != nil {
				return nil, err
			}
			if len(b) <= limit {
				best, lo = b, mid+1
			} else {
				hi = mid - 1
			}
		}
		if best != nil {
			return best, nil
		}
	}
	return encode(big)
}
