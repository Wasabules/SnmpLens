package snmp

import (
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"
)

// boundListener starts a real gosnmp trap listener and waits for its socket.
func boundListener(t *testing.T, onTrap gosnmp.TrapHandlerFunc) (*gosnmp.TrapListener, int) {
	t.Helper()
	port := freePort(t)

	tl := gosnmp.NewTrapListener()
	tl.Params = &gosnmp.GoSNMP{Port: uint16(port), Version: gosnmp.Version2c}
	tl.OnNewTrap = onTrap

	go func() { _ = tl.Listen(fmt.Sprintf("127.0.0.1:%d", port)) }()
	t.Cleanup(tl.Close)

	select {
	case <-tl.Listening():
	case <-time.After(5 * time.Second):
		t.Fatal("the listener never bound")
	}
	return tl, port
}

// The whole reason reaching into gosnmp's unexported state is acceptable: the
// assumption is checked, so a gosnmp upgrade that renames or retypes the field
// fails CI rather than silently halving the next trap storm.
func TestTheTrapSocketIsStillReachable(t *testing.T) {
	tl, _ := boundListener(t, func(*gosnmp.SnmpPacket, *net.UDPAddr) {})

	conn, err := trapSocket(tl)
	if err != nil {
		t.Fatalf("gosnmp's internals changed and the read buffer can no longer be "+
			"raised — a trap storm will be silently truncated: %v", err)
	}
	if conn == nil {
		t.Fatal("trapSocket returned no error and no socket")
	}
	if conn.LocalAddr() == nil {
		t.Error("the socket is not bound")
	}
	if err := conn.SetReadBuffer(TrapReadBuffer); err != nil {
		t.Errorf("the system refused a %d-byte read buffer: %v", TrapReadBuffer, err)
	}
}

// Raising it must never be able to break the listener, whatever happens.
func TestRaisingTheReadBufferIsFailSoft(t *testing.T) {
	// No listener at all.
	raiseTrapReadBuffer(nil, TrapReadBuffer)

	// A listener that was never bound: there is no socket yet.
	unbound := gosnmp.NewTrapListener()
	raiseTrapReadBuffer(unbound, TrapReadBuffer)

	// An absurd size the system will refuse.
	tl, _ := boundListener(t, func(*gosnmp.SnmpPacket, *net.UDPAddr) {})
	raiseTrapReadBuffer(tl, -1)

	// And the listener still works afterwards.
	if _, err := trapSocket(tl); err != nil {
		t.Errorf("the listener was damaged: %v", err)
	}
}

// What the buffer is actually for.
//
// A burst arrives faster than the handler can drain it, and the kernel drops
// whatever does not fit — before Go sees it, so there is no error, no journal
// entry and nothing to count. Measured with the default buffer and a handler
// doing the real work: 417 of 2000 datagrams arrived. This test uses a handler
// that is deliberately slow, so the socket buffer is the only thing standing
// between a burst and silence.
func TestABurstSurvivesTheSocketBuffer(t *testing.T) {
	if testing.Short() {
		t.Skip("sends a few thousand datagrams")
	}

	const burst = 1500

	received := make(chan struct{}, burst*2)
	slow := func(*gosnmp.SnmpPacket, *net.UDPAddr) {
		// Stand in for journalling the trap, which is a write transaction.
		time.Sleep(300 * time.Microsecond)
		received <- struct{}{}
	}

	tl, port := boundListener(t, slow)
	raiseTrapReadBuffer(tl, TrapReadBuffer)

	conn, err := net.Dial("udp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	packet := trapDatagram(t)
	sent := 0
	for i := 0; i < burst; i++ {
		if _, err := conn.Write(packet); err != nil {
			break
		}
		sent++
	}

	// Let the handler drain.
	deadline := time.After(20 * time.Second)
	count := 0
drain:
	for count < sent {
		select {
		case <-received:
			count++
		case <-time.After(1500 * time.Millisecond):
			break drain
		case <-deadline:
			break drain
		}
	}

	kept := float64(count) / float64(sent) * 100
	t.Logf("%d of %d datagrams reached the handler (%.0f%%)", count, sent, kept)

	// Deliberately generous. The point is to catch the buffer being lost
	// entirely — the measured no-buffer case was 21% — not to pin a number that
	// depends on how busy the machine is.
	if kept < 70 {
		t.Errorf("only %.0f%% of a %d-datagram burst survived; the socket read "+
			"buffer is not being raised, and the loss is silent", kept, sent)
	}
}

// trapDatagram captures the exact bytes gosnmp puts on the wire for a trap, by
// sending one to a socket we hold. Replaying real bytes means the listener
// parses them and calls the handler, which is what the test is measuring —
// hand-rolled BER would risk being rejected before it ever gets there.
func trapDatagram(t *testing.T) []byte {
	t.Helper()

	sink, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer sink.Close()
	addr := sink.LocalAddr().(*net.UDPAddr)

	sender := &gosnmp.GoSNMP{
		Target: "127.0.0.1", Port: uint16(addr.Port), Version: gosnmp.Version2c,
		Community: "public", Timeout: 2 * time.Second, Retries: 0,
	}
	if err := sender.Connect(); err != nil {
		t.Fatalf("could not connect the capture sender: %v", err)
	}
	defer sender.Conn.Close()

	go func() {
		_, _ = sender.SendTrap(gosnmp.SnmpTrap{
			Variables: []gosnmp.SnmpPDU{
				{Name: "1.3.6.1.2.1.1.3.0", Type: gosnmp.TimeTicks, Value: uint32(1234)},
				{Name: "1.3.6.1.6.3.1.1.4.1.0", Type: gosnmp.ObjectIdentifier, Value: "1.3.6.1.6.3.1.1.5.3"},
			},
		})
	}()

	buf := make([]byte, 4096)
	if err := sink.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	n, _, err := sink.ReadFrom(buf)
	if err != nil {
		t.Fatalf("no trap was captured: %v", err)
	}
	return buf[:n]
}
