package simulator

import (
	"bytes"
	"net"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"
)

func TestAGetIsAnsweredFromTheTree(t *testing.T) {
	a := startAgent(t, Config{Versions: []string{"v2c"}, Community: "public"})
	g := connect(t, manager(a, gosnmp.Version2c, "public"))

	res, err := g.Get([]string{sysDescrOID, ".1.3.6.1.2.1.1.2.0", ".1.3.6.1.2.1.1.3.0"})
	if err != nil {
		t.Fatal(err)
	}
	if got := text(res.Variables[0]); got != sysDescr {
		t.Errorf("sysDescr = %q, want %q", got, sysDescr)
	}
	if got := res.Variables[1].Value; got != ".1.3.6.1.4.1.8072.3.2.10" {
		t.Errorf("sysObjectID = %v", got)
	}
	if got := res.Variables[2].Type; got != gosnmp.TimeTicks {
		t.Errorf("sysUpTime is %v, want TimeTicks", got)
	}
}

// RFC 3416 4.2.1 tells a missing instance of an object the agent has from an
// object it does not have at all.
func TestAMissingObjectIsToldFromAMissingInstance(t *testing.T) {
	a := startAgent(t, Config{Versions: []string{"v2c"}, Community: "public"})
	g := connect(t, manager(a, gosnmp.Version2c, "public"))

	for name, want := range map[string]gosnmp.Asn1BER{
		".1.3.6.1.2.1.1.1.1":     gosnmp.NoSuchInstance, // sysDescr has no instance 1
		".1.3.6.1.2.1.1.1":       gosnmp.NoSuchInstance, // sysDescr named without its instance
		".1.3.6.1.2.1.2.2.1.2.9": gosnmp.NoSuchInstance, // the ifDescr of an interface that is not there
		".1.3.6.1.2.1.1":         gosnmp.NoSuchObject,   // the system group is not an object
		".1.3.6.1.4.1.99999.1.0": gosnmp.NoSuchObject,
	} {
		res, err := g.Get([]string{name})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := res.Variables[0].Type; got != want {
			t.Errorf("%s: %v, want %v", name, got, want)
		}
	}
}

func TestAWalkVisitsEveryObjectInTheAgentsOrder(t *testing.T) {
	a := startAgent(t, Config{Versions: []string{"v2c"}, Community: "public"})
	g := connect(t, manager(a, gosnmp.Version2c, "public"))

	for name, walk := range map[string]func(string, gosnmp.WalkFunc) error{
		"GETNEXT": g.Walk,
		"GETBULK": g.BulkWalk,
	} {
		var got []string
		err := walk(".1.3.6.1", func(p gosnmp.SnmpPDU) error {
			got = append(got, p.Name)
			return nil
		})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if want := walkOrder(false); !slices.Equal(got, want) {
			t.Errorf("%s walked\n%v\nwant\n%v", name, got, want)
		}
	}
}

// SNMPv1 has errors of its own, and no Counter64 (RFC 1157, RFC 3584).
func TestSNMPv1(t *testing.T) {
	a := startAgent(t, Config{Versions: []string{"v1"}, Community: "public"})
	g := connect(t, manager(a, gosnmp.Version1, "public"))

	res, err := g.Get([]string{sysDescrOID})
	if err != nil || text(res.Variables[0]) != sysDescr {
		t.Fatalf("GET sysDescr: %v, %v", res, err)
	}

	// One object missing fails the whole request, and says which.
	res, err = g.Get([]string{sysDescrOID, ".1.3.6.1.2.1.1.1.1"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Error != gosnmp.NoSuchName || res.ErrorIndex != 2 {
		t.Errorf("a missing second object: %v at %d, want noSuchName at 2", res.Error, res.ErrorIndex)
	}

	// A Counter64 does not exist for v1, to a GET...
	res, err = g.Get([]string{".1.3.6.1.2.1.31.1.1.1.6.1"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Error != gosnmp.NoSuchName || res.ErrorIndex != 1 {
		t.Errorf("a Counter64 over v1: %v at %d, want noSuchName at 1", res.Error, res.ErrorIndex)
	}

	// ...nor to a walk, which steps over it.
	var walked []string
	err = g.Walk(".1.3.6.1", func(p gosnmp.SnmpPDU) error {
		walked = append(walked, p.Name)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := slices.DeleteFunc(walkOrder(false), func(n string) bool {
		return strings.HasPrefix(n, ".1.3.6.1.2.1.31.1.1.1.6.")
	})
	if !slices.Equal(walked, want) {
		t.Errorf("v1 walked\n%v\nwant\n%v", walked, want)
	}
}

func TestASetIsRefused(t *testing.T) {
	a := startAgent(t, Config{Versions: []string{"v1", "v2c"}, Community: "public"})
	for ver, want := range map[gosnmp.SnmpVersion]gosnmp.SNMPError{
		gosnmp.Version2c: gosnmp.NotWritable,
		gosnmp.Version1:  gosnmp.NoSuchName, // v1 has no notWritable (RFC 3584)
	} {
		g := connect(t, manager(a, ver, "public"))
		res, err := g.Set([]gosnmp.SnmpPDU{{Name: ".1.3.6.1.2.1.1.5.0", Type: gosnmp.OctetString, Value: "renamed"}})
		if err != nil {
			t.Fatalf("%v: %v", ver, err)
		}
		if res.Error != want || res.ErrorIndex != 1 {
			t.Errorf("%v: %v at %d, want %v at 1", ver, res.Error, res.ErrorIndex, want)
		}
	}
}

// encode is a request as a gosnmp manager would put it on the wire.
func encode(t *testing.T, pdu gosnmp.PDUType, oids []string, maxRepetitions uint32) []byte {
	t.Helper()
	vars := make([]gosnmp.SnmpPDU, len(oids))
	for i, o := range oids {
		vars[i] = gosnmp.SnmpPDU{Name: o, Type: gosnmp.Null}
	}
	msg, err := (&gosnmp.GoSNMP{Version: gosnmp.Version2c, Community: "public"}).SnmpEncodePacket(pdu, vars, 0, maxRepetitions)
	if err != nil {
		t.Fatal(err)
	}
	return msg
}

func decode(t *testing.T, msg []byte) *gosnmp.SnmpPacket {
	t.Helper()
	if msg == nil {
		t.Fatal("no answer")
	}
	res, err := (&gosnmp.GoSNMP{Version: gosnmp.Version2c}).SnmpDecodePacket(msg)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func now() clock {
	t := time.Now()
	return clock{started: t, now: t}
}

// A GETBULK that would not fit is cut from its end (RFC 3416 4.2.3), and is
// still an answer.
func TestAGetBulkIsCutToFitTheMessage(t *testing.T) {
	a := newAgent(t, Config{Versions: []string{"v2c"}, Community: "public"})
	req := encode(t, gosnmp.GetBulkRequest, []string{".1.3.6.1"}, 50)
	// Half of what the whole answer measures, rather than a guessed size: 400
	// bytes, the guess this started with, held the whole walk but for its
	// closing endOfMibView — a cut, but not one that shows anything.
	whole := decode(t, a.handle(req, now()))
	a.maxSize = len(a.handle(req, now())) / 2

	out := a.handle(req, now())
	if len(out) > a.maxSize {
		t.Fatalf("%d bytes, over the %d allowed", len(out), a.maxSize)
	}
	res := decode(t, out)
	got := names(res.Variables)
	if res.Error != gosnmp.NoError || len(got) == 0 || len(got) >= len(whole.Variables) {
		t.Fatalf("%v with %d of %d varbinds; want a shorter answer that is still an answer",
			res.Error, len(got), len(whole.Variables))
	}
	if !slices.Equal(got, walkOrder(false)[:len(got)]) {
		t.Errorf("cut somewhere other than its end: %v", got)
	}
}

// Anything else that does not fit is tooBig, with nothing in it (RFC 3416
// 4.2.1): a GET answered with varbinds missing answers another question.
func TestAGetThatDoesNotFitIsTooBig(t *testing.T) {
	a := newAgent(t, Config{Versions: []string{"v2c"}, Community: "public"})
	a.maxSize = 200

	res := decode(t, a.handle(encode(t, gosnmp.GetRequest, sampleOrder, 0), now()))
	if res.Error != gosnmp.TooBig || len(res.Variables) != 0 {
		t.Errorf("%v with %d varbinds, want tooBig with none", res.Error, len(res.Variables))
	}
}

func TestTheWrongCommunityGetsNoAnswer(t *testing.T) {
	a := startAgent(t, Config{Versions: []string{"v2c"}, Community: "public"})
	g := manager(a, gosnmp.Version2c, "private")
	g.Timeout = silent
	connect(t, g)

	if _, err := g.Get([]string{sysDescrOID}); err == nil {
		t.Fatal("answered the wrong community")
	}
	if n := a.Stats().BadCommunities; n != 1 {
		t.Errorf("BadCommunities = %d, want 1", n)
	}
}

func TestAVersionTheDeviceDoesNotAnswerGetsNoAnswer(t *testing.T) {
	a := startAgent(t, Config{Versions: []string{"v2c"}, Community: "public"})
	g := manager(a, gosnmp.Version1, "public")
	g.Timeout = silent
	connect(t, g)

	if _, err := g.Get([]string{sysDescrOID}); err == nil {
		t.Fatal("a v2c-only device answered v1")
	}
	if n := a.Stats().BadVersions; n != 1 {
		t.Errorf("BadVersions = %d, want 1", n)
	}
}

// A malformed datagram costs that datagram, and the agent answers the next.
func TestGarbageCostsOnlyItself(t *testing.T) {
	a := startAgent(t, Config{Versions: []string{"v2c"}, Community: "public"})
	conn, err := net.DialUDP("udp", nil, net.UDPAddrFromAddrPort(a.Addr()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	valid := encode(t, gosnmp.GetRequest, []string{sysDescrOID}, 0)
	junk := [][]byte{
		{0x30},
		{0x30, 0x84, 0xff, 0xff, 0xff, 0xff}, // a length far past the datagram
		{0x30, 0x80, 0x02, 0x01, 0x01},       // the indefinite form
		{0x30, 0x03, 0x02, 0x01, 0x02},       // version 2, which SNMP does not have
		bytes.Repeat([]byte{0xff}, 64),
		valid[:len(valid)-3], // a real request, cut short
	}
	for _, j := range junk {
		if _, err := conn.Write(j); err != nil {
			t.Fatal(err)
		}
	}

	g := connect(t, manager(a, gosnmp.Version2c, "public"))
	res, err := g.Get([]string{sysDescrOID})
	if err != nil || text(res.Variables[0]) != sysDescr {
		t.Fatalf("after the garbage: %v, %v", res, err)
	}
	s := a.Stats()
	if s.Packets != uint32(len(junk))+1 || s.BadVersions != 1 || s.ParseErrors+s.Faults != uint32(len(junk))-1 {
		t.Errorf("%+v", s)
	}
}
