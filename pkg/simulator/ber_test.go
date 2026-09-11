package simulator

import (
	"bytes"
	"testing"

	"github.com/gosnmp/gosnmp"
)

func TestPeekReadsWhatAMessageClaims(t *testing.T) {
	msg, err := (&gosnmp.SnmpPacket{
		Version:       gosnmp.Version3,
		MsgFlags:      gosnmp.Reportable,
		SecurityModel: gosnmp.UserSecurityModel,
		SecurityParameters: &gosnmp.UsmSecurityParameters{
			UserName:                 "ops",
			AuthoritativeEngineID:    string(testEngineID),
			AuthoritativeEngineBoots: 4,
			AuthoritativeEngineTime:  1234,
		},
		MsgID:      77,
		MsgMaxSize: 65507,
		PDUType:    gosnmp.GetRequest,
		RequestID:  4242,
	}).MarshalMsg()
	if err != nil {
		t.Fatal(err)
	}

	h, err := peekV3(msg)
	if err != nil {
		t.Fatal(err)
	}
	if h.msgID != 77 || h.flags != gosnmp.Reportable || h.model != int64(gosnmp.UserSecurityModel) ||
		!bytes.Equal(h.engineID, testEngineID) || h.boots != 4 || h.time != 1234 ||
		h.user != "ops" || h.requestID != 4242 {
		t.Errorf("%+v", h)
	}
}

// RFC 3412 gives msgMaxSize a floor of 484.
func TestPeekRefusesAFieldOutOfItsRange(t *testing.T) {
	msg, err := (&gosnmp.SnmpPacket{
		Version:            gosnmp.Version3,
		SecurityModel:      gosnmp.UserSecurityModel,
		SecurityParameters: &gosnmp.UsmSecurityParameters{},
		MsgMaxSize:         100,
		PDUType:            gosnmp.GetRequest,
	}).MarshalMsg()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := peekV3(msg); err == nil {
		t.Error("read a msgMaxSize of 100")
	}
}

// seeds are real messages of every shape the peek meets, for the fuzzer to
// start from: v1, v2c, a v3 discovery, and a signed and encrypted v3 message.
func seeds(t testing.TB) [][]byte {
	var out [][]byte
	for _, ver := range []gosnmp.SnmpVersion{gosnmp.Version1, gosnmp.Version2c} {
		msg, err := (&gosnmp.GoSNMP{Version: ver, Community: "public"}).SnmpEncodePacket(
			gosnmp.GetRequest, []gosnmp.SnmpPDU{{Name: sysDescrOID, Type: gosnmp.Null}}, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, msg)
	}
	discovery, err := (&gosnmp.SnmpPacket{
		Version:            gosnmp.Version3,
		MsgFlags:           gosnmp.Reportable,
		SecurityModel:      gosnmp.UserSecurityModel,
		SecurityParameters: &gosnmp.UsmSecurityParameters{},
		PDUType:            gosnmp.GetRequest,
		RequestID:          1,
	}).MarshalMsg()
	if err != nil {
		t.Fatal(err)
	}
	out = append(out, discovery)

	u, err := resolveUser(User{Name: "ops", SecLevel: "AuthPriv", AuthProto: "SHA256", AuthPass: "authpass1", PrivProto: "AES", PrivPass: "privpass1"}, testEngineID)
	if err != nil {
		t.Fatal(err)
	}
	a := &Agent{engineID: testEngineID, boots: 1, maxSize: maxMessage}
	pkt := a.v3Packet(u, u.name, gosnmp.AuthPriv, 3, now())
	pkt.PDUType = gosnmp.GetResponse
	pkt.Variables = []gosnmp.SnmpPDU{{Name: sysDescrOID, Type: gosnmp.OctetString, Value: sysDescr}}
	secure, err := pkt.MarshalMsg()
	if err != nil {
		t.Fatal(err)
	}
	return append(out, secure)
}

// The peek reads lengths straight off the network. handle recovers from a
// panic, but only by dropping the message — the reader in front of every
// untrusted length should never need it.
func FuzzPeek(f *testing.F) {
	for _, s := range seeds(f) {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, msg []byte) {
		_, _ = peekVersion(msg)
		_, _ = peekV3(msg)
	})
}
