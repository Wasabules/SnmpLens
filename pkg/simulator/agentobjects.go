package simulator

import (
	"fmt"
	"sync/atomic"

	"github.com/gosnmp/gosnmp"
)

// agentObjects are what only the agent can answer, because it counts them: the
// snmp group of SNMPv2-MIB (RFC 3418) — what it has received, refused and
// answered since it started — and for SNMPv3 the snmpEngine group, the MPD and
// USM statistics its Reports name, and snmpUnknownContexts. They are live: a
// GET of snmpInPkts counts itself.
//
// Every device has them, the models' own objects beside, so a model — built-in,
// or a file somebody wrote — cannot answer them in its own words.
func agentObjects(v3 bool, engineID []byte, boots uint32, authenTraps bool) []Object {
	var out []Object
	count := func(oid string, f func(*counters) uint32) {
		out = append(out, Object{OID: oid, Type: gosnmp.Counter32, Value: statistic(f)})
	}
	load := func(get func(*counters) *atomic.Uint32) func(*counters) uint32 {
		return func(c *counters) uint32 { return get(c).Load() }
	}
	nothing := func(*counters) uint32 { return 0 }

	const snmp = "1.3.6.1.2.1.11."
	count(snmp+"1.0", load(func(c *counters) *atomic.Uint32 { return &c.packets }))
	count(snmp+"2.0", func(c *counters) uint32 { return c.outGetResponses.Load() + c.outTraps.Load() })
	count(snmp+"3.0", load(func(c *counters) *atomic.Uint32 { return &c.badVersions }))
	count(snmp+"4.0", load(func(c *counters) *atomic.Uint32 { return &c.badCommunities }))
	count(snmp+"5.0", nothing) // a community used for what it may not do: nothing is writable
	count(snmp+"6.0", load(func(c *counters) *atomic.Uint32 { return &c.parseErrors }))
	// What arrived in a response, which an agent receives none of.
	for _, n := range []int{8, 9, 10, 11, 12} {
		count(fmt.Sprintf(snmp+"%d.0", n), nothing)
	}
	count(snmp+"13.0", load(func(c *counters) *atomic.Uint32 { return &c.inTotalReqVars }))
	count(snmp+"14.0", nothing) // no SET has succeeded
	count(snmp+"15.0", load(func(c *counters) *atomic.Uint32 { return &c.inGets }))
	count(snmp+"16.0", load(func(c *counters) *atomic.Uint32 { return &c.inGetNexts }))
	count(snmp+"17.0", load(func(c *counters) *atomic.Uint32 { return &c.inSets }))
	count(snmp+"18.0", nothing)
	count(snmp+"19.0", nothing)
	count(snmp+"20.0", nothing)
	count(snmp+"21.0", load(func(c *counters) *atomic.Uint32 { return &c.outNoSuchNames }))
	count(snmp+"22.0", nothing)
	count(snmp+"24.0", nothing)
	for _, n := range []int{25, 26, 27} {
		count(fmt.Sprintf(snmp+"%d.0", n), nothing) // requests the agent sent: none
	}
	count(snmp+"28.0", load(func(c *counters) *atomic.Uint32 { return &c.outGetResponses }))
	count(snmp+"29.0", load(func(c *counters) *atomic.Uint32 { return &c.outTraps }))
	enable := 2
	if authenTraps {
		enable = 1
	}
	out = append(out, Object{OID: snmp + "30.0", Type: gosnmp.Integer, Value: Const(enable)})
	count(snmp+"31.0", nothing)
	count(snmp+"32.0", nothing)
	if !v3 {
		return out
	}

	out = append(out, engineObjects(engineID, boots)...)
	for oid, get := range map[string]func(*counters) *atomic.Uint32{
		".1.3.6.1.6.3.11.2.1.1.0": func(c *counters) *atomic.Uint32 { return &c.unknownSecurityModels },
		".1.3.6.1.6.3.11.2.1.2.0": func(c *counters) *atomic.Uint32 { return &c.invalidMsgs },
		oidUnknownPDUHandlers:     func(c *counters) *atomic.Uint32 { return &c.unknownPDUHandlers },
		oidUnknownContexts:        func(c *counters) *atomic.Uint32 { return &c.unknownContexts },
		oidUnsupportedSecLevels:   func(c *counters) *atomic.Uint32 { return &c.unsupportedSecLevels },
		oidNotInTimeWindows:       func(c *counters) *atomic.Uint32 { return &c.notInTimeWindows },
		oidUnknownUserNames:       func(c *counters) *atomic.Uint32 { return &c.unknownUserNames },
		oidUnknownEngineIDs:       func(c *counters) *atomic.Uint32 { return &c.unknownEngineIDs },
		oidWrongDigests:           func(c *counters) *atomic.Uint32 { return &c.wrongDigests },
		oidDecryptionErrors:       func(c *counters) *atomic.Uint32 { return &c.decryptionErrors },
	} {
		count(oid, load(get))
	}
	count(".1.3.6.1.6.3.12.1.4.0", nothing) // snmpUnavailableContexts
	return out
}

// statistic reads one of the agent's own counters at the moment it is asked.
type statistic func(*counters) uint32

func (s statistic) read(c clock) any {
	if c.stats == nil {
		return uint32(0)
	}
	return s(c.stats)
}

func (statistic) check(t gosnmp.Asn1BER) error {
	if t != gosnmp.Counter32 {
		return fmt.Errorf("the agent's counters are Counter32, not %v", t)
	}
	return nil
}
