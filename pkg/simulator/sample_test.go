package simulator

import (
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"
)

const (
	sysDescrOID = ".1.3.6.1.2.1.1.1.0"
	sysDescr    = "Linux sim-01 6.8.0 #1 SMP x86_64"
)

// sampleOrder is the order an agent walks sampleObjects in, written out rather
// than computed: ifInOctets (column 10) after ifDescr (column 2), and ifAlias
// (18) after ifHCInOctets (6), are where comparing the OIDs as TEXT would put
// them the other way round.
var sampleOrder = []string{
	".1.3.6.1.2.1.1.1.0",
	".1.3.6.1.2.1.1.2.0",
	".1.3.6.1.2.1.1.3.0",
	".1.3.6.1.2.1.1.5.0",
	".1.3.6.1.2.1.2.1.0",
	".1.3.6.1.2.1.2.2.1.1.1",
	".1.3.6.1.2.1.2.2.1.1.2",
	".1.3.6.1.2.1.2.2.1.2.1",
	".1.3.6.1.2.1.2.2.1.2.2",
	".1.3.6.1.2.1.2.2.1.10.1",
	".1.3.6.1.2.1.2.2.1.10.2",
	".1.3.6.1.2.1.31.1.1.1.1.1",
	".1.3.6.1.2.1.31.1.1.1.1.2",
	".1.3.6.1.2.1.31.1.1.1.6.1",
	".1.3.6.1.2.1.31.1.1.1.6.2",
	".1.3.6.1.2.1.31.1.1.1.18.1",
	".1.3.6.1.2.1.31.1.1.1.18.2",
}

// sampleObjects is a small device, declared interface by interface rather than
// in the order it is walked: the system group, and two interfaces with a 32-bit
// and a 64-bit counter each — enough for a walk to cross from one table to the
// next, and for SNMPv1 to have a Counter64 to step over.
func sampleObjects() []Object {
	objects := []Object{
		{OID: "1.3.6.1.2.1.1.1.0", Type: gosnmp.OctetString, Value: Const(sysDescr)},
		{OID: "1.3.6.1.2.1.1.2.0", Type: gosnmp.ObjectIdentifier, Value: Const(".1.3.6.1.4.1.8072.3.2.10")},
		{OID: "1.3.6.1.2.1.1.3.0", Type: gosnmp.TimeTicks, Value: Uptime()},
		{OID: "1.3.6.1.2.1.1.5.0", Type: gosnmp.OctetString, Value: Const("sim-01")},
		{OID: "1.3.6.1.2.1.2.1.0", Type: gosnmp.Integer, Value: Const(2)},
	}
	for i := 1; i <= 2; i++ {
		objects = append(objects,
			Object{OID: fmt.Sprintf("1.3.6.1.2.1.2.2.1.1.%d", i), Type: gosnmp.Integer, Value: Const(i)},
			Object{OID: fmt.Sprintf("1.3.6.1.2.1.2.2.1.2.%d", i), Type: gosnmp.OctetString, Value: Const(fmt.Sprintf("eth%d", i-1))},
			Object{OID: fmt.Sprintf("1.3.6.1.2.1.2.2.1.10.%d", i), Type: gosnmp.Counter32, Value: Const(uint32(1000 * i))},
			Object{OID: fmt.Sprintf("1.3.6.1.2.1.31.1.1.1.1.%d", i), Type: gosnmp.OctetString, Value: Const(fmt.Sprintf("eth%d", i-1))},
			Object{OID: fmt.Sprintf("1.3.6.1.2.1.31.1.1.1.6.%d", i), Type: gosnmp.Counter64, Value: Const(uint64(i) << 40)},
			Object{OID: fmt.Sprintf("1.3.6.1.2.1.31.1.1.1.18.%d", i), Type: gosnmp.OctetString, Value: Const(fmt.Sprintf("uplink %d", i))},
		)
	}
	return objects
}

var testEngineID = mustEngineID(8072, "02:00:00:00:00:01")

func mustEngineID(enterprise uint32, mac string) []byte {
	hw, err := net.ParseMAC(mac)
	if err != nil {
		panic(err)
	}
	id, err := EngineID(enterprise, hw)
	if err != nil {
		panic(err)
	}
	return id
}

// newAgent makes an agent from cfg, filling in what a test does not care about.
func newAgent(t *testing.T, cfg Config) *Agent {
	t.Helper()
	if cfg.Listen == "" {
		cfg.Listen = "127.0.0.1:0"
	}
	if cfg.Objects == nil {
		cfg.Objects = sampleObjects()
	}
	if cfg.EngineID == nil {
		cfg.EngineID = testEngineID
	}
	a, err := NewAgent(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// startAgent makes an agent and runs it for the length of the test.
func startAgent(t *testing.T, cfg Config) *Agent {
	t.Helper()
	a := newAgent(t, cfg)
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Stop)
	return a
}

// manager is a gosnmp manager aimed at a. It never retries, so one request is
// one message and the agent's counters read as counts.
func manager(a *Agent, ver gosnmp.SnmpVersion, community string) *gosnmp.GoSNMP {
	ap := a.Addr()
	return &gosnmp.GoSNMP{
		Target:    ap.Addr().String(),
		Port:      ap.Port(),
		Version:   ver,
		Community: community,
		Timeout:   2 * time.Second,
		Retries:   0,
	}
}

// v3Manager is a gosnmp SNMPv3 manager aimed at a.
func v3Manager(a *Agent, user string, level gosnmp.SnmpV3MsgFlags,
	auth gosnmp.SnmpV3AuthProtocol, authPass string,
	priv gosnmp.SnmpV3PrivProtocol, privPass string) *gosnmp.GoSNMP {
	g := manager(a, gosnmp.Version3, "")
	g.SecurityModel = gosnmp.UserSecurityModel
	g.MsgFlags = level
	g.SecurityParameters = &gosnmp.UsmSecurityParameters{
		UserName:                 user,
		AuthenticationProtocol:   auth,
		AuthenticationPassphrase: authPass,
		PrivacyProtocol:          priv,
		PrivacyPassphrase:        privPass,
	}
	return g
}

// connect opens g's socket for the length of the test.
func connect(t *testing.T, g *gosnmp.GoSNMP) *gosnmp.GoSNMP {
	t.Helper()
	if err := g.Connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { g.Conn.Close() })
	return g
}

// silent is how long a test waits for an answer that must not come.
const silent = 300 * time.Millisecond

func names(vars []gosnmp.SnmpPDU) []string {
	out := make([]string, len(vars))
	for i, v := range vars {
		out[i] = v.Name
	}
	return out
}

func text(v gosnmp.SnmpPDU) string {
	b, _ := v.Value.([]byte)
	return string(b)
}
