package simulator

import (
	"bytes"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gosnmp/gosnmp"
)

// maxMessage is the largest message an agent sends: the largest payload one
// IPv4 UDP datagram carries. A v3 manager may ask for less (msgMaxSize).
const maxMessage = 65507

// maxCommunity bounds a community. gosnmp writes a community's length as one
// byte of short-form BER, which reads back only up to 127.
const maxCommunity = 127

// Config describes one simulated agent.
type Config struct {
	// Listen is the loopback address and UDP port the agent answers on:
	// "127.0.0.2:161", "[::1]:16161". Port 0 picks a free one.
	Listen string
	// Versions are the SNMP versions the agent answers, among "v1", "v2c" and
	// "v3". A message in any other is dropped, the way a device with that
	// version switched off drops it.
	Versions []string
	// Community is what v1 and v2c are read with.
	Community string
	// Users are the SNMPv3 users.
	Users []User
	// EngineID is the agent's snmpEngineID, 5 to 32 octets; see EngineID. It
	// must not change from one run to the next: a manager caches it and
	// localises its keys to it.
	EngineID []byte
	// EngineBoots is snmpEngineBoots for this run. It is the caller's to keep,
	// and to increase every time the device starts (RFC 3414); zero is taken as 1.
	EngineBoots uint32
	// Objects are what the agent answers for.
	Objects []Object
	// Notifications are what the agent can send: its model's catalogue, or
	// without one the generic notifications every agent has.
	Notifications []Notification
	// Traps is where the agent sends them, and when.
	Traps Traps
}

// Stats counts what an agent did with what it received — most of all what it
// did NOT answer, since silence is all a manager ever sees of that. The names
// are the MIB counters each one is.
type Stats struct {
	Packets               uint32 // datagrams received
	ParseErrors           uint32 // snmpInASNParseErrs: nothing this agent can read
	BadVersions           uint32 // snmpInBadVersions: a version this device does not answer
	BadCommunities        uint32 // snmpInBadCommunityNames
	UnknownSecurityModels uint32 // snmpUnknownSecurityModels
	InvalidMsgs           uint32 // snmpInvalidMsgs: privacy asked for without authentication
	UnknownEngineIDs      uint32 // usmStatsUnknownEngineIDs: every discovery lands here
	UnknownUserNames      uint32 // usmStatsUnknownUserNames
	UnsupportedSecLevels  uint32 // usmStatsUnsupportedSecLevels
	WrongDigests          uint32 // usmStatsWrongDigests
	NotInTimeWindows      uint32 // usmStatsNotInTimeWindows
	DecryptionErrors      uint32 // usmStatsDecryptionErrors
	UnknownContexts       uint32 // snmpUnknownContexts
	UnknownPDUHandlers    uint32 // snmpUnknownPDUHandlers
	Faults                uint32 // a message that crashed a decoder, or an answer that would not encode

	// Notifications is what the agent sent, per destination, in the order they
	// are configured.
	Notifications []DestinationStats
	// Suppressed counts the notifications the agent's cap held back.
	Suppressed uint32
}

type counters struct {
	packets, parseErrors, badVersions, badCommunities, unknownSecurityModels, invalidMsgs,
	unknownEngineIDs, unknownUserNames, unsupportedSecLevels, wrongDigests, notInTimeWindows,
	decryptionErrors, unknownContexts, unknownPDUHandlers, faults atomic.Uint32
	// What SNMPv2-MIB's snmp group counts besides: the requests by kind, the
	// varbinds read, the answers and the notifications sent.
	inGets, inGetNexts, inSets, inTotalReqVars, outGetResponses, outNoSuchNames, outTraps atomic.Uint32
}

// Agent is one simulated device answering on one socket.
//
// It runs once. A device that restarts is a new Agent with EngineBoots one
// higher, which is also what tells a manager holding its old clock to
// resynchronise.
type Agent struct {
	listen      netip.AddrPort
	v1, v2c, v3 bool
	community   []byte
	users       map[string]*user
	engineID    []byte
	boots       uint32
	tree        *tree
	maxSize     int
	// notify sends the agent's notifications; nil when it has nowhere to.
	notify *notifier

	mu   sync.Mutex
	conn *net.UDPConn
	done chan struct{}
	// started is when the agent started answering: its sysUpTime and its
	// snmpEngineTime count from it.
	started time.Time

	stats counters
}

// NewAgent checks cfg and prepares an agent; Start makes it answer.
func NewAgent(cfg Config) (*Agent, error) {
	listen, err := CheckListen(cfg.Listen)
	if err != nil {
		return nil, fmt.Errorf("simulator: %w", err)
	}
	a := &Agent{listen: listen, boots: max(cfg.EngineBoots, 1), maxSize: maxMessage}
	for _, v := range cfg.Versions {
		switch v {
		case "v1":
			a.v1 = true
		case "v2c":
			a.v2c = true
		case "v3":
			a.v3 = true
		default:
			return nil, fmt.Errorf("simulator: %q is not an SNMP version (v1, v2c or v3)", v)
		}
	}
	if !a.v1 && !a.v2c && !a.v3 {
		return nil, errors.New("simulator: an agent answers at least one SNMP version")
	}
	if a.v1 || a.v2c {
		if cfg.Community == "" || len(cfg.Community) > maxCommunity {
			return nil, fmt.Errorf("simulator: v1 and v2c need a community of 1 to %d octets", maxCommunity)
		}
		a.community = []byte(cfg.Community)
	}
	if a.v3 {
		if len(cfg.EngineID) < 5 || len(cfg.EngineID) > 32 {
			return nil, errors.New("simulator: an engine ID is 5 to 32 octets (RFC 3411)")
		}
		if a.boots >= maxBoots {
			return nil, errors.New("simulator: the engine's boots have reached their ceiling; it needs a new engine ID")
		}
		if len(cfg.Users) == 0 {
			return nil, errors.New("simulator: v3 needs at least one user")
		}
		a.engineID = bytes.Clone(cfg.EngineID)
		if a.users, err = newUsers(cfg.Users, a.engineID); err != nil {
			return nil, fmt.Errorf("simulator: %w", err)
		}
	}
	// What only the agent knows — its own counters, and for v3 its engine —
	// beside what the device answers.
	objects := slices.Concat(cfg.Objects, agentObjects(a.v3, a.engineID, a.boots, cfg.Traps.OnAuthFailure))
	if a.tree, err = newTree(objects); err != nil {
		return nil, fmt.Errorf("simulator: %w", err)
	}
	if a.notify, err = newNotifier(a, cfg); err != nil {
		return nil, fmt.Errorf("simulator: %w", err)
	}
	return a, nil
}

// engineObjects is the snmpEngine group (SNMP-FRAMEWORK-MIB): the engine's own
// ID, boots and time, which only the agent knows. It is where a manager reads
// back the engine it discovered.
func engineObjects(id []byte, boots uint32) []Object {
	return []Object{
		{OID: "1.3.6.1.6.3.10.2.1.1.0", Type: gosnmp.OctetString, Value: Const(bytes.Clone(id))},
		{OID: "1.3.6.1.6.3.10.2.1.2.0", Type: gosnmp.Integer, Value: Const(int(boots))},
		{OID: "1.3.6.1.6.3.10.2.1.3.0", Type: gosnmp.Integer, Value: engineSeconds{}},
		{OID: "1.3.6.1.6.3.10.2.1.4.0", Type: gosnmp.Integer, Value: Const(maxMessage)},
	}
}

// Start binds the socket and starts answering.
func (a *Agent) Start() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.done != nil {
		return errors.New("simulator: an agent runs once; a restarted device is a new Agent with EngineBoots + 1")
	}
	conn, err := net.ListenUDP("udp", net.UDPAddrFromAddrPort(a.listen))
	if err != nil {
		return fmt.Errorf("simulator: listen on %s: %w", a.listen, err)
	}
	if err := checkBound(conn.LocalAddr()); err != nil {
		conn.Close()
		return fmt.Errorf("simulator: %w", err)
	}
	a.conn = conn
	a.done = make(chan struct{})
	a.started = time.Now()
	go a.serve(conn, a.started, a.done)
	if a.notify != nil {
		a.notify.start()
	}
	return nil
}

// Stop closes the socket, waits for the message in hand to be answered, and
// ends the notifications — one waiting for its acknowledgement included.
func (a *Agent) Stop() {
	a.mu.Lock()
	conn, done := a.conn, a.done
	a.mu.Unlock()
	if conn == nil {
		return
	}
	conn.Close()
	<-done
	if a.notify != nil {
		a.notify.stop()
	}
}

// Addr is the address the agent answers on, with the port it was given when it
// asked for any.
func (a *Agent) Addr() netip.AddrPort {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.conn == nil {
		return a.listen
	}
	ap := a.conn.LocalAddr().(*net.UDPAddr).AddrPort()
	return netip.AddrPortFrom(ap.Addr().Unmap(), ap.Port())
}

// Stats returns the agent's counters as they stand.
func (a *Agent) Stats() Stats {
	s := &a.stats
	st := Stats{
		Packets:               s.packets.Load(),
		ParseErrors:           s.parseErrors.Load(),
		BadVersions:           s.badVersions.Load(),
		BadCommunities:        s.badCommunities.Load(),
		UnknownSecurityModels: s.unknownSecurityModels.Load(),
		InvalidMsgs:           s.invalidMsgs.Load(),
		UnknownEngineIDs:      s.unknownEngineIDs.Load(),
		UnknownUserNames:      s.unknownUserNames.Load(),
		UnsupportedSecLevels:  s.unsupportedSecLevels.Load(),
		WrongDigests:          s.wrongDigests.Load(),
		NotInTimeWindows:      s.notInTimeWindows.Load(),
		DecryptionErrors:      s.decryptionErrors.Load(),
		UnknownContexts:       s.unknownContexts.Load(),
		UnknownPDUHandlers:    s.unknownPDUHandlers.Load(),
		Faults:                s.faults.Load(),
	}
	if nt := a.notify; nt != nil {
		st.Notifications = nt.stats()
		st.Suppressed = nt.suppressed.Load()
	}
	return st
}

// serve answers datagrams one at a time until the socket closes. One at a time
// is what a device does, and it keeps a message's work — the stats, the clock —
// free of any lock.
func (a *Agent) serve(conn *net.UDPConn, started time.Time, done chan struct{}) {
	defer close(done)
	buf := make([]byte, 65535)
	for {
		n, from, err := conn.ReadFromUDPAddrPort(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			// A read error on an open socket costs that read, not the device. The
			// pause keeps one that repeats from spinning a core.
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if out := a.handle(buf[:n], clock{started: started, now: time.Now(), stats: &a.stats}); out != nil {
			_, _ = conn.WriteToUDPAddrPort(out, from)
		}
	}
}

// handle answers one datagram, or returns nil to stay silent.
func (a *Agent) handle(msg []byte, c clock) (out []byte) {
	defer func() {
		// The decoders are gosnmp's and they index into bytes that arrived from
		// the network. A panic left to run would end the process: one malformed
		// datagram must cost that datagram and nothing else.
		if r := recover(); r != nil {
			a.stats.faults.Add(1)
			out = nil
		}
	}()
	a.stats.packets.Add(1)
	ver, err := peekVersion(msg)
	switch {
	case errors.Is(err, errBadVersion):
		a.stats.badVersions.Add(1)
		return nil
	case err != nil:
		a.stats.parseErrors.Add(1)
		return nil
	}
	switch {
	case ver == gosnmp.Version1 && a.v1, ver == gosnmp.Version2c && a.v2c:
		return a.answerCommunity(ver, msg, c)
	case ver == gosnmp.Version3 && a.v3:
		return a.answerV3(msg, c)
	}
	a.stats.badVersions.Add(1)
	return nil
}

// answerCommunity answers an SNMPv1 or SNMPv2c message.
func (a *Agent) answerCommunity(ver gosnmp.SnmpVersion, msg []byte, c clock) []byte {
	req, err := (&gosnmp.GoSNMP{Version: ver}).SnmpDecodePacket(msg)
	if err != nil {
		a.stats.parseErrors.Add(1)
		return nil
	}
	// In constant time: the community is the only secret v1 and v2c have.
	if subtle.ConstantTimeCompare([]byte(req.Community), a.community) != 1 {
		a.stats.badCommunities.Add(1)
		a.authFailed()
		return nil
	}
	switch req.PDUType {
	case gosnmp.GetRequest, gosnmp.GetNextRequest, gosnmp.SetRequest:
	case gosnmp.GetBulkRequest:
		if ver == gosnmp.Version1 {
			// SNMPv1 has no GETBULK: this is not a v1 message.
			a.stats.parseErrors.Add(1)
			return nil
		}
	default:
		a.stats.unknownPDUHandlers.Add(1)
		return nil
	}
	out, err := fit(a.process(ver, req, c), bulkFloor(req), tooBig(ver, req), a.maxSize,
		func(ans answer) ([]byte, error) {
			resp := &gosnmp.SnmpPacket{
				Version:    ver,
				Community:  req.Community,
				PDUType:    gosnmp.GetResponse,
				RequestID:  req.RequestID,
				Error:      ans.status,
				ErrorIndex: ans.index,
				Variables:  ans.vars,
			}
			return resp.MarshalMsg()
		})
	if err != nil {
		a.stats.faults.Add(1)
		return nil
	}
	return out
}
