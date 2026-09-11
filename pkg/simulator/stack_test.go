package simulator

import (
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"
)

// column is what every instance of a column holds, by instance.
func column(tr *tree, col string) map[string]any {
	prefix, _ := parseOID(col)
	out := map[string]any{}
	for e := tr.next(prefix, nil); e != nil && e.oid.hasPrefix(prefix); e = tr.next(e.oid, nil) {
		out[strings.TrimPrefix(e.oid[len(prefix):].String(), ".")] = e.val.read(clock{})
	}
	return out
}

// whole is a value read from the tree as a whole number, whatever its type.
func whole(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case uint32:
		return int(n)
	case uint64:
		return int(n)
	}
	return -1
}

// Every model's standard MIBs agree with each other, as a real agent's do:
// every interface one of them names is one ifTable lists, every entity sits in
// one that exists, every bridge port a station was learnt on is a port, and
// every count counts what it says it does.
func TestTheStandardMIBsAgree(t *testing.T) {
	for _, m := range models {
		t.Run(m.ID, func(t *testing.T) {
			tr, err := newTree(slices.Concat(m.build(Identity{Name: "dev-01", Seed: 11}), agentObjects(false, nil, 1, false)))
			if err != nil {
				t.Fatal(err)
			}
			scalar := func(oid string) any {
				id, _ := parseOID(oid)
				if e := tr.get(id); e != nil {
					return e.val.read(clock{})
				}
				return nil
			}

			interfaces := column(tr, "1.3.6.1.2.1.2.2.1.1")
			if n := whole(scalar("1.3.6.1.2.1.2.1.0")); n != len(interfaces) {
				t.Errorf("ifNumber is %d, and ifTable has %d rows", n, len(interfaces))
			}
			isInterface := func(v any) bool { _, ok := interfaces[strconv.Itoa(whole(v))]; return ok }
			for name, col := range map[string]string{
				"ipAdEntIfIndex":       "1.3.6.1.2.1.4.20.1.2",
				"ipNetToMediaIfIndex":  "1.3.6.1.2.1.4.22.1.1",
				"ipAddressIfIndex":     "1.3.6.1.2.1.4.34.1.3",
				"ipCidrRouteIfIndex":   "1.3.6.1.2.1.4.24.4.1.5",
				"dot1dBasePortIfIndex": "1.3.6.1.2.1.17.1.4.1.2",
				"dot3StatsIndex":       "1.3.6.1.2.1.10.7.2.1.1",
				"hrNetworkIfIndex":     "1.3.6.1.2.1.25.3.4.1.1",
			} {
				for inst, v := range column(tr, col) {
					if !isInterface(v) {
						t.Errorf("%s.%s names interface %v, which ifTable does not list", name, inst, v)
					}
				}
			}
			for inst, v := range column(tr, "1.3.6.1.2.1.47.1.3.2.1.2") {
				if !isInterface(strings.TrimPrefix(v.(string), ".1.3.6.1.2.1.2.2.1.1.")) &&
					!isInterface(whole(atoi(strings.TrimPrefix(v.(string), ".1.3.6.1.2.1.2.2.1.1.")))) {
					t.Errorf("entAliasMappingIdentifier.%s points at %v, no interface", inst, v)
				}
			}
			for inst := range column(tr, "1.0.8802.1.1.2.1.3.7.1.2") {
				if !isInterface(atoi(inst)) {
					t.Errorf("lldpLocPortNum %s is no interface", inst)
				}
			}
			for inst := range column(tr, "1.0.8802.1.1.2.1.4.1.1.9") {
				if local := strings.Split(inst, ".")[1]; !isInterface(atoi(local)) {
					t.Errorf("an LLDP neighbour is heard on port %s, which is no interface", local)
				}
			}

			entities := column(tr, "1.3.6.1.2.1.47.1.1.1.1.5")
			for inst, v := range column(tr, "1.3.6.1.2.1.47.1.1.1.1.4") {
				if n := whole(v); n != 0 {
					if _, ok := entities[strconv.Itoa(n)]; !ok {
						t.Errorf("entity %s is contained in %d, which is not an entity", inst, n)
					}
				}
			}
			if v := scalar("1.3.6.1.4.1.9.9.109.1.1.1.1.2.1"); v != nil && len(entities) > 0 {
				if _, ok := entities[strconv.Itoa(whole(v))]; !ok {
					t.Errorf("cpmCPUTotalPhysicalIndex is %v, which is not an entity", v)
				}
			}

			established := 0
			for _, v := range column(tr, "1.3.6.1.2.1.6.13.1.1") {
				if whole(v) == 5 {
					established++
				}
			}
			if n := whole(scalar("1.3.6.1.2.1.6.9.0")); n != established {
				t.Errorf("tcpCurrEstab is %d, and tcpConnTable has %d session(s) established", n, established)
			}
			if n, routes := whole(scalar("1.3.6.1.2.1.4.24.3.0")), len(column(tr, "1.3.6.1.2.1.4.24.4.1.1")); n != routes {
				t.Errorf("ipCidrRouteNumber is %d, and the table has %d route(s)", n, routes)
			}
			if running := column(tr, "1.3.6.1.2.1.25.4.2.1.1"); len(running) > 0 {
				if n := whole(scalar("1.3.6.1.2.1.25.1.6.0")); n != len(running) {
					t.Errorf("hrSystemProcesses is %d, and hrSWRunTable lists %d", n, len(running))
				}
			}
			if ports := column(tr, "1.3.6.1.2.1.17.1.4.1.1"); len(ports) > 0 {
				if n := whole(scalar("1.3.6.1.2.1.17.1.2.0")); n != len(ports) {
					t.Errorf("dot1dBaseNumPorts is %d, and the port table has %d", n, len(ports))
				}
				for inst, v := range column(tr, "1.3.6.1.2.1.17.4.3.1.2") {
					if p := whole(v); p != 0 {
						if _, ok := ports[strconv.Itoa(p)]; !ok {
							t.Errorf("station %s was learnt on port %d, which is not a bridge port", inst, p)
						}
					}
				}
				if n, vlans := whole(scalar("1.3.6.1.2.1.17.7.1.1.4.0")), len(column(tr, "1.3.6.1.2.1.17.7.1.4.3.1.1")); n != vlans {
					t.Errorf("dot1qNumVlans is %d, and there are %d VLAN(s)", n, vlans)
				}
			}
			if len(column(tr, "1.3.6.1.2.1.1.9.1.2")) == 0 {
				t.Error("sysORTable is empty")
			}
		})
	}
}

func atoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return -1
	}
	return n
}

// The snmp group counts what the agent does, as it does it: every datagram,
// every GET and GETNEXT, the varbinds it read, the refused community, the
// answers it sent.
func TestTheAgentCountsWhatItAnswers(t *testing.T) {
	a := startAgent(t, Config{Versions: []string{"v2c"}, Community: "public"})
	g := connect(t, manager(a, gosnmp.Version2c, "public"))
	read := func(oid string) uint64 {
		t.Helper()
		res, err := g.Get([]string{oid})
		if err != nil || res.Variables[0].Type != gosnmp.Counter32 {
			t.Fatalf("%s: %v %v", oid, res, err)
		}
		return gosnmp.ToBigInt(res.Variables[0].Value).Uint64()
	}
	const (
		inPkts       = ".1.3.6.1.2.1.11.1.0"
		badCommunity = ".1.3.6.1.2.1.11.4.0"
		reqVars      = ".1.3.6.1.2.1.11.13.0"
		gets         = ".1.3.6.1.2.1.11.15.0"
		getNexts     = ".1.3.6.1.2.1.11.16.0"
		responses    = ".1.3.6.1.2.1.11.28.0"
	)
	// Counted on arrival: a GET of snmpInPkts counts itself.
	first := read(inPkts)
	if again := read(inPkts); again != first+1 {
		t.Errorf("snmpInPkts went from %d to %d over one request", first, again)
	}
	before := read(gets)
	if after := read(gets); after != before+1 {
		t.Errorf("snmpInGetRequests went from %d to %d over one GET", before, after)
	}
	nexts, vars := read(getNexts), read(reqVars)
	if err := g.Walk(".1.3.6.1.2.1.1", func(gosnmp.SnmpPDU) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if n := read(getNexts); n <= nexts {
		t.Errorf("a walk left snmpInGetNexts at %d", n)
	}
	if n := read(reqVars); n < vars+4 {
		t.Errorf("snmpInTotalReqVars went from %d to %d over a walk of the system group", vars, n)
	}
	if n := read(responses); n < read(gets) {
		t.Errorf("%d responses for more GETs than that", n)
	}

	stranger := manager(a, gosnmp.Version2c, "private")
	stranger.Timeout = silent
	connect(t, stranger)
	_, _ = stranger.Get([]string{sysDescrOID})
	if n := read(badCommunity); n != 1 {
		t.Errorf("snmpInBadCommunityNames is %d after one wrong community", n)
	}
	res, err := g.Get([]string{".1.3.6.1.2.1.11.30.0"})
	if err != nil || gosnmp.ToBigInt(res.Variables[0].Value).Int64() != 2 {
		t.Errorf("snmpEnableAuthenTraps, with none configured: %v %v", res, err)
	}
}

// hrSystemDate is the time now, and it moves.
func TestTheSystemDateIsNow(t *testing.T) {
	now := time.Date(2026, 9, 11, 16, 5, 42, 300_000_000, time.FixedZone("CEST", 2*3600))
	got := dateAndTime{}.read(clock{now: now}).([]byte)
	want := []byte{0x07, 0xEA, 9, 11, 16, 5, 42, 3, '+', 2, 0}
	if !slices.Equal(got, want) {
		t.Errorf("DateAndTime of %v is %v, want %v", now, got, want)
	}
}
