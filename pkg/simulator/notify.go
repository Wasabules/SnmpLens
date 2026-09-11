package simulator

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gosnmp/gosnmp"
)

// Bounds on what a device file may ask of a device.
const (
	// MaxDestinations is how many places one device sends its notifications to.
	MaxDestinations = 8
	// MaxSchedules is how many schedules one device keeps.
	MaxSchedules = 8
	// MaxEvery is the longest a schedule waits between two notifications: a day.
	MaxEvery = 86400
)

// What one device may send, whatever it is configured to. The cap is the
// device's rather than a schedule's because authenticationFailure is not
// scheduled: every refused request asks for one, a request is a datagram
// anything on this machine can send, and a destination may be on the network.
// Uncapped, a simulated device would turn each spoofed datagram into a
// notification sent elsewhere, as many times a second as anyone liked.
const (
	notifyRate       = 20 // notifications a second, on average
	notifyBurst      = 40
	authFailureRate  = 1
	authFailureBurst = 3
	// informTimeout and informRetries are how long an INFORM waits to be
	// acknowledged, and how many more times it is sent.
	informTimeout = 2 * time.Second
	informRetries = 1
	// queueDepth is how many notifications wait for one destination. One that
	// makes every INFORM wait out its timeout fills it, and what does not fit is
	// counted rather than waited for.
	queueDepth = 32
)

const (
	oidSysObjectID        = ".1.3.6.1.2.1.1.2.0"
	oidSysUpTime          = ".1.3.6.1.2.1.1.3.0"
	oidSnmpTrapOID        = ".1.3.6.1.6.3.1.1.4.1.0"
	oidSnmpTrapEnterprise = ".1.3.6.1.6.3.1.1.4.3.0"
	// oidSnmpTraps is where RFC 3418 puts the generic notifications, which
	// SNMPv1 numbers by generic-trap rather than by OID.
	oidSnmpTraps = ".1.3.6.1.6.3.1.1.5"
)

var (
	// ErrNotRunning is returned for a device that is not running.
	ErrNotRunning = errors.New("simulator: the device is not running")
	// ErrNoDestination is returned for a notification from a device that has
	// nowhere to send one.
	ErrNoDestination = errors.New("simulator: the device has nowhere to send notifications")
	// ErrTooFast is returned for a notification over the device's cap.
	ErrTooFast = errors.New("simulator: the device is already sending notifications as fast as it may")

	errQueueFull = errors.New("too many notifications are waiting for this destination")
)

// Notification is one a device can send: its name in the MIB that defines it —
// which is also how the interface shows it, since it is the name every manager
// shows — the snmpTrapOID it carries, and the objects it carries after that.
type Notification struct {
	Name string
	OID  string
	// Objects are read from the device's own tree as it is sent, so what
	// linkDown says about an interface is what a GET says about it at that
	// moment.
	Objects []string
}

// NotificationInfo is a notification as the interface lists it.
type NotificationInfo struct {
	Name string `json:"name"`
	OID  string `json:"oid"`
}

// The generic notifications of RFC 3418 that every device sends.
var (
	coldStart             = Notification{Name: "coldStart", OID: oidSnmpTraps + ".1"}
	warmStart             = Notification{Name: "warmStart", OID: oidSnmpTraps + ".2"}
	authenticationFailure = Notification{Name: "authenticationFailure", OID: oidSnmpTraps + ".5"}
)

// linkNotification is RFC 2863's linkDown or linkUp about the interface at
// index, with the three objects that RFC has it carry.
func linkNotification(up bool, index int) Notification {
	n := Notification{Name: "linkDown", OID: oidSnmpTraps + ".3"}
	if up {
		n = Notification{Name: "linkUp", OID: oidSnmpTraps + ".4"}
	}
	for _, column := range []int{1, 7, 8} { // ifIndex, ifAdminStatus, ifOperStatus
		n.Objects = append(n.Objects, fmt.Sprintf(".1.3.6.1.2.1.2.2.1.%d.%d", column, index))
	}
	return n
}

// Destination is where a device sends its notifications: a manager, SnmpLens on
// this machine or on another one. Unlike where a device ANSWERS, it may be any
// address — a simulated device that could only notify its own machine would be
// no use for testing a trap receiver elsewhere.
type Destination struct {
	// ID keys the destination's community in the credential store. It is given
	// when the destination is first saved, as a device's is.
	ID string `json:"id"`
	// Host is an IP address or a host name.
	Host string `json:"host"`
	Port int    `json:"port"`
	// Version is v1, v2c or v3.
	Version string `json:"version"`
	// Inform sends an InformRequest, which the receiver acknowledges, instead
	// of a trap. v2c and v3 only.
	Inform bool `json:"inform"`
	// Community is what a v1 or v2c notification carries. A secret.
	Community string `json:"community"`
	// User is the device's SNMPv3 user a v3 notification is sent as.
	User string `json:"user"`
	// EngineID is the receiver's snmpEngineID in hex, for a v3 INFORM: the
	// receiver is the authoritative engine for one, and a device given none
	// discovers it (RFC 3414 4). A v3 trap is sent as the device's own engine
	// and ignores it.
	EngineID string `json:"engineId"`
}

// Schedule sends a notification over and over.
type Schedule struct {
	// Notification is a name from the device's catalogue, or empty for one at
	// random each time.
	Notification string `json:"notification"`
	// Every is the seconds between two, 1 to MaxEvery.
	Every int `json:"every"`
	// Irregular draws each wait at random, from half of Every to half as much
	// again: the same rate on average, without the regularity no real event has.
	Irregular bool `json:"irregular"`
}

// Traps is what a device sends, where to and when.
type Traps struct {
	Destinations []Destination `json:"destinations"`
	// OnStart sends coldStart once the device answers, as an agent does when it
	// starts.
	OnStart bool `json:"onStart"`
	// OnAuthFailure sends authenticationFailure for a request refused for its
	// community, an unknown user or a wrong digest: snmpEnableAuthenTraps.
	OnAuthFailure bool       `json:"onAuthFailure"`
	Schedules     []Schedule `json:"schedules"`
}

// clone is t sharing nothing with it.
func (t Traps) clone() Traps {
	t.Destinations = slices.Clone(t.Destinations)
	t.Schedules = slices.Clone(t.Schedules)
	return t
}

// check reports the first thing wrong with t, for a device whose SNMPv3 users
// are users and which can send what catalogue lists.
func (t Traps) check(users []User, catalogue []Notification) error {
	if len(t.Destinations) > MaxDestinations {
		return fmt.Errorf("a device sends to at most %d destinations", MaxDestinations)
	}
	if len(t.Schedules) > MaxSchedules {
		return fmt.Errorf("a device keeps at most %d schedules", MaxSchedules)
	}
	ids := make(map[string]bool, len(t.Destinations))
	for _, d := range t.Destinations {
		if d.ID == "" || ids[d.ID] {
			return errors.New("every destination needs an ID of its own")
		}
		ids[d.ID] = true
		if err := d.check(users); err != nil {
			return fmt.Errorf("destination %s: %w", net.JoinHostPort(d.Host, strconv.Itoa(d.Port)), err)
		}
	}
	for _, s := range t.Schedules {
		if s.Every < 1 || s.Every > MaxEvery {
			return fmt.Errorf("a schedule waits 1 to %d seconds, not %d", MaxEvery, s.Every)
		}
		if s.Notification != "" && !slices.ContainsFunc(catalogue, func(n Notification) bool { return n.Name == s.Notification }) {
			return fmt.Errorf("%q is not a notification this device sends", s.Notification)
		}
	}
	return nil
}

func (d Destination) check(users []User) error {
	if err := checkNotifyHost(d.Host); err != nil {
		return err
	}
	if d.Port < 1 || d.Port > 65535 {
		return fmt.Errorf("%d is not a port", d.Port)
	}
	switch d.Version {
	case "v1", "v2c":
		if d.Inform && d.Version == "v1" {
			return errors.New("SNMPv1 has no InformRequest (RFC 1157)")
		}
		if d.Community == "" || len(d.Community) > maxCommunity {
			return fmt.Errorf("v1 and v2c need a community of 1 to %d octets", maxCommunity)
		}
	case "v3":
		if !slices.ContainsFunc(users, func(u User) bool { return u.Name == d.User }) {
			return fmt.Errorf("a v3 notification is sent as one of the device's SNMPv3 users, and %q is not one", d.User)
		}
	default:
		return fmt.Errorf("%q is not an SNMP version (v1, v2c or v3)", d.Version)
	}
	if d.EngineID != "" {
		if id, err := hex.DecodeString(d.EngineID); err != nil || len(id) < 5 || len(id) > 32 {
			return errors.New("a receiver's engine ID is 5 to 32 octets, in hex")
		}
	}
	return nil
}

var broadcast = netip.AddrFrom4([4]byte{255, 255, 255, 255})

// checkNotifyHost accepts an IP address or a host name, and refuses what a
// notification cannot be sent to: no address at all (0.0.0.0, ::) and a group.
func checkNotifyHost(host string) error {
	if ip, err := netip.ParseAddr(host); err == nil {
		ip = ip.Unmap()
		if ip.IsUnspecified() || ip.IsMulticast() || ip == broadcast {
			return fmt.Errorf("%s is not an address a notification can be sent to", host)
		}
		return nil
	}
	if !validHostName(host) {
		return fmt.Errorf("%q is neither an IP address nor a host name", host)
	}
	return nil
}

// validHostName is RFC 1123: dot-separated labels of letters, digits and
// hyphens, none starting or ending with a hyphen.
func validHostName(s string) bool {
	s = strings.TrimSuffix(s, ".")
	if s == "" || len(s) > 253 {
		return false
	}
	for _, label := range strings.Split(s, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-') {
				return false
			}
		}
	}
	return true
}

// Delivery is what became of one notification at one destination.
type Delivery struct {
	// ID is the destination's.
	ID string `json:"id"`
	// Destination is where it went, as host:port.
	Destination string `json:"destination"`
	Inform      bool   `json:"inform"`
	// Acknowledged is an INFORM the receiver answered.
	Acknowledged bool   `json:"acknowledged"`
	Error        string `json:"error,omitempty"`
}

// DestinationStats is what a device has sent one destination since it started.
type DestinationStats struct {
	ID string `json:"id"`
	// Sent counts the notifications that left: a trap written to the socket, or
	// an INFORM the receiver acknowledged.
	Sent uint32 `json:"sent"`
	// Failed counts the ones that did not, an INFORM nobody acknowledged
	// included.
	Failed uint32 `json:"failed"`
	// Dropped counts the ones never tried, the destination's queue being full.
	Dropped   uint32 `json:"dropped"`
	LastError string `json:"lastError"`
}

// notifier sends a running agent's notifications, and lives exactly as long as
// the agent runs.
type notifier struct {
	a             *Agent
	catalogue     []Notification
	schedules     []Schedule
	onStart       bool
	onAuthFailure bool
	// passphrases are the users', by name. An INFORM is localised to the
	// receiver's engine, and the agent holds keys for its own engine only.
	passphrases map[string]User
	out         []*outbound
	limit       *bucket
	authLimit   *bucket
	suppressed  atomic.Uint32
	ctx         context.Context
	cancel      context.CancelFunc
	running     atomic.Bool
	wg          sync.WaitGroup
}

// outbound is one destination and the one goroutine that sends to it, so an
// INFORM waiting on a receiver that never answers holds up that receiver and no
// other.
type outbound struct {
	Destination
	// dial is the host as dialled: "localhost" is made the loopback address of
	// the device's own family, without asking a resolver.
	dial string
	// local is where the notification leaves from. For a destination on this
	// machine that is the device's own address, so a receiver sees it come from
	// the device as it would from real equipment. For any other it is the
	// system's choice, since a loopback source cannot reach the network.
	local string
	// receiver is an INFORM receiver's engine ID, or empty to discover it.
	receiver string
	queue    chan job

	sent, failed, dropped atomic.Uint32
	mu                    sync.Mutex
	lastError             string
}

type job struct {
	n    Notification
	done chan<- Delivery // nil unless somebody waits for the outcome
}

// newNotifier prepares what cfg has the agent send, or returns nil when there
// is nowhere to send anything.
func newNotifier(a *Agent, cfg Config) (*notifier, error) {
	catalogue := cfg.Notifications
	if catalogue == nil {
		catalogue = []Notification{coldStart, warmStart, authenticationFailure}
	}
	var users []User
	if a.v3 {
		users = cfg.Users
	}
	if err := cfg.Traps.check(users, catalogue); err != nil {
		return nil, err
	}
	if len(cfg.Traps.Destinations) == 0 {
		return nil, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	nt := &notifier{
		a:             a,
		catalogue:     slices.Clone(catalogue),
		schedules:     slices.Clone(cfg.Traps.Schedules),
		onStart:       cfg.Traps.OnStart,
		onAuthFailure: cfg.Traps.OnAuthFailure,
		passphrases:   make(map[string]User, len(users)),
		limit:         newBucket(notifyRate, notifyBurst),
		authLimit:     newBucket(authFailureRate, authFailureBurst),
		ctx:           ctx,
		cancel:        cancel,
	}
	for _, u := range users {
		nt.passphrases[u.Name] = u
	}
	self := a.listen.Addr()
	for _, d := range cfg.Traps.Destinations {
		o := &outbound{Destination: d, dial: d.Host, queue: make(chan job, queueDepth)}
		dest, err := netip.ParseAddr(d.Host)
		if strings.EqualFold(d.Host, "localhost") {
			dest, err = netip.IPv6Loopback(), nil
			if self.Is4() {
				dest = netip.AddrFrom4([4]byte{127, 0, 0, 1})
			}
		}
		if dest = dest.Unmap(); err == nil && dest.IsLoopback() {
			o.dial = dest.String()
			if dest.Is4() == self.Is4() {
				o.local = netip.AddrPortFrom(self, 0).String()
			}
		}
		if d.EngineID != "" {
			id, _ := hex.DecodeString(d.EngineID) // check has read it
			o.receiver = string(id)
		}
		nt.out = append(nt.out, o)
	}
	return nt, nil
}

func (nt *notifier) start() {
	nt.running.Store(true)
	for _, o := range nt.out {
		nt.wg.Add(1)
		go nt.run(o)
	}
	for _, s := range nt.schedules {
		nt.wg.Add(1)
		go nt.every(s)
	}
	if nt.onStart {
		nt.fire(coldStart)
	}
}

// stop ends the schedules and the sends — an INFORM waiting for its
// acknowledgement included — and waits for them.
func (nt *notifier) stop() {
	nt.cancel()
	nt.wg.Wait()
}

// fire sends n to every destination if the device's cap allows it. It never
// blocks: the agent's receive loop is among its callers.
func (nt *notifier) fire(n Notification) {
	if !nt.limit.allow(time.Now()) {
		nt.suppressed.Add(1)
		return
	}
	nt.enqueue(n, nil)
}

// enqueue hands n to every destination's goroutine without waiting. What a full
// queue cannot take is counted, and told to whoever waits for the outcome.
func (nt *notifier) enqueue(n Notification, done chan<- Delivery) {
	for _, o := range nt.out {
		select {
		case o.queue <- job{n: n, done: done}:
		default:
			o.dropped.Add(1)
			if done != nil {
				done <- o.delivery(errQueueFull)
			}
		}
	}
}

func (nt *notifier) run(o *outbound) {
	defer nt.wg.Done()
	for {
		select {
		case <-nt.ctx.Done():
			return
		case j := <-o.queue:
			err := nt.deliver(o, j.n)
			if nt.ctx.Err() != nil {
				// Stopped in the middle of it, which is not an outcome.
				return
			}
			o.record(err)
			if j.done != nil {
				j.done <- o.delivery(err)
			}
		}
	}
}

// every runs one schedule until the agent stops.
func (nt *notifier) every(s Schedule) {
	defer nt.wg.Done()
	period := time.Duration(s.Every) * time.Second
	for {
		wait := period
		if s.Irregular {
			wait = time.Duration((0.5 + rand.Float64()) * float64(period))
		}
		t := time.NewTimer(wait)
		select {
		case <-nt.ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
		if n, ok := nt.pick(s.Notification); ok {
			nt.fire(n)
		}
	}
}

// find returns the notification named name.
func (nt *notifier) find(name string) (Notification, bool) {
	i := slices.IndexFunc(nt.catalogue, func(n Notification) bool { return n.Name == name })
	if i < 0 {
		return Notification{}, false
	}
	return nt.catalogue[i], true
}

// pick is the notification a schedule names, or one at random when it names
// none.
func (nt *notifier) pick(name string) (Notification, bool) {
	if name != "" {
		return nt.find(name)
	}
	if len(nt.catalogue) == 0 {
		return Notification{}, false
	}
	return nt.catalogue[rand.IntN(len(nt.catalogue))], true
}

// deliver sends n to o now and, for an INFORM, waits for the acknowledgement.
func (nt *notifier) deliver(o *outbound, n Notification) error {
	a := nt.a
	c := clock{started: a.started, now: time.Now()}
	g := &gosnmp.GoSNMP{
		Target:    o.dial,
		Port:      uint16(o.Port),
		Transport: "udp",
		LocalAddr: o.local,
		Timeout:   informTimeout,
		Retries:   informRetries,
		Context:   nt.ctx,
	}
	var trap gosnmp.SnmpTrap
	switch o.Version {
	case "v1":
		g.Version, g.Community = gosnmp.Version1, o.Community
		t, err := a.v1Trap(n, c)
		if err != nil {
			return err
		}
		trap = t
	case "v2c":
		g.Version, g.Community = gosnmp.Version2c, o.Community
		trap.Variables = a.varbinds(n, c)
	case "v3":
		u := a.users[o.User]
		g.Version, g.SecurityModel, g.MsgFlags = gosnmp.Version3, gosnmp.UserSecurityModel, u.level
		// The originator's context, as RFC 3413 3.2 has it.
		g.ContextEngineID = string(a.engineID)
		g.SecurityParameters = nt.security(o, u, c)
		trap.Variables = a.varbinds(n, c)
	}
	trap.IsInform = o.Inform
	if err := g.Connect(); err != nil {
		return err
	}
	// gosnmp looks at its context between two attempts and not during one, so a
	// device stopping while an INFORM waits for its acknowledgement closes the
	// socket under it. Otherwise Stop would wait the timeout out.
	release := context.AfterFunc(nt.ctx, func() { g.Conn.Close() })
	defer func() {
		release()
		g.Conn.Close()
	}()
	res, err := g.SendTrap(trap)
	switch {
	case err != nil:
		return err
	case !o.Inform:
		return nil
	case res == nil:
		return errors.New("the INFORM was not acknowledged")
	case res.Error != gosnmp.NoError:
		return fmt.Errorf("the receiver refused the INFORM: %v", res.Error)
	}
	return nil
}

// security is how a v3 notification to o is signed and sealed.
//
// A trap is sent as the device's own engine, which is the authoritative one for
// it (RFC 3414 1.5.1): with the keys the agent localised to its engine ID, its
// boots and its time. An INFORM is authoritative at the RECEIVER, so it goes with
// the passphrases, which gosnmp localises to the engine ID given — or to the one
// it discovers first, when none was.
func (nt *notifier) security(o *outbound, u *user, c clock) *gosnmp.UsmSecurityParameters {
	if !o.Inform {
		sp := u.params(nt.a.engineID, u.level)
		sp.AuthoritativeEngineBoots = nt.a.boots
		sp.AuthoritativeEngineTime = nt.a.engineTime(c)
		return sp
	}
	p := nt.passphrases[u.name]
	sp := &gosnmp.UsmSecurityParameters{
		UserName:               u.name,
		AuthoritativeEngineID:  o.receiver,
		AuthenticationProtocol: gosnmp.NoAuth,
		PrivacyProtocol:        gosnmp.NoPriv,
	}
	if u.level&gosnmp.AuthNoPriv != 0 {
		sp.AuthenticationProtocol, sp.AuthenticationPassphrase = u.auth, p.AuthPass
	}
	if u.level == gosnmp.AuthPriv {
		sp.PrivacyProtocol, sp.PrivacyPassphrase = u.priv, p.PrivPass
	}
	return sp
}

// varbinds is what a v2c or v3 notification carries (RFC 3416 4.2.6):
// sysUpTime.0 and snmpTrapOID.0, then the notification's objects as the device
// answers them at this instant, then — for a generic one — snmpTrapEnterprise.0
// with the device's sysObjectID, which is where RFC 3584 3.2 finds an SNMPv1
// enterprise for it and what net-snmp sends.
func (a *Agent) varbinds(n Notification, c clock) []gosnmp.SnmpPDU {
	vars := []gosnmp.SnmpPDU{
		{Name: oidSysUpTime, Type: gosnmp.TimeTicks, Value: c.uptime()},
		{Name: oidSnmpTrapOID, Type: gosnmp.ObjectIdentifier, Value: n.OID},
	}
	vars = append(vars, a.objectsOf(n, c)...)
	if _, generic := genericTrap(n.OID); generic {
		if id, ok := a.sysObjectID(c); ok {
			vars = append(vars, gosnmp.SnmpPDU{Name: oidSnmpTrapEnterprise, Type: gosnmp.ObjectIdentifier, Value: id})
		}
	}
	return vars
}

// objectsOf reads n's objects from the tree; one the device does not have is
// left out.
func (a *Agent) objectsOf(n Notification, c clock) []gosnmp.SnmpPDU {
	var out []gosnmp.SnmpPDU
	for _, name := range n.Objects {
		id, err := parseOID(name)
		if err != nil {
			continue
		}
		if e := a.tree.get(id); e != nil {
			out = append(out, e.varbind(c))
		}
	}
	return out
}

func (a *Agent) sysObjectID(c clock) (string, bool) {
	id, _ := parseOID(oidSysObjectID)
	e := a.tree.get(id)
	if e == nil || e.typ != gosnmp.ObjectIdentifier {
		return "", false
	}
	s, ok := e.val.read(c).(string)
	return s, ok
}

// genericTrap is the SNMPv1 generic-trap of a notification RFC 3418 defines —
// snmpTraps.1 to .6 are coldStart(0) to egpNeighborLoss(5) — and false for any
// other.
func genericTrap(oid string) (int, bool) {
	rest, ok := strings.CutPrefix(oid, oidSnmpTraps+".")
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(rest)
	if err != nil || n < 1 || n > 6 {
		return 0, false
	}
	return n - 1, true
}

// v1Trap is n as an SNMPv1 Trap-PDU, by RFC 3584 3.2. A generic notification is
// its generic-trap under the device's sysObjectID. Any other is
// enterpriseSpecific(6), with its last arc as the specific-trap and the rest as
// the enterprise — less the zero before the last arc too, which is how SMIv2
// writes a notification an SNMPv1 trap once was.
func (a *Agent) v1Trap(n Notification, c clock) (gosnmp.SnmpTrap, error) {
	// agent-addr is an IPv4 address (RFC 1157). For a device on ::1 the
	// wildcard, which says to look at where the datagram came from.
	t := gosnmp.SnmpTrap{AgentAddress: "0.0.0.0", Timestamp: uint(c.uptime())}
	if ip := a.listen.Addr(); ip.Is4() {
		t.AgentAddress = ip.String()
	}
	if generic, ok := genericTrap(n.OID); ok {
		t.GenericTrap, t.Enterprise = generic, oidSnmpTraps
		if id, ok := a.sysObjectID(c); ok {
			t.Enterprise = id
		}
	} else {
		id, err := parseOID(n.OID)
		if err != nil {
			return t, err
		}
		cut := len(id) - 1
		if cut > 2 && id[cut-1] == 0 {
			cut--
		}
		t.GenericTrap, t.SpecificTrap, t.Enterprise = 6, int(id[len(id)-1]), id[:cut].String()
	}
	for _, v := range a.objectsOf(n, c) {
		if v.Type == gosnmp.Counter64 {
			return t, errors.New("SNMPv1 cannot carry a Counter64, and RFC 3584 3.2 then sends nothing")
		}
		t.Variables = append(t.Variables, v)
	}
	return t, nil
}

func (o *outbound) record(err error) {
	if err == nil {
		o.sent.Add(1)
		return
	}
	o.failed.Add(1)
	o.mu.Lock()
	o.lastError = err.Error()
	o.mu.Unlock()
}

func (o *outbound) delivery(err error) Delivery {
	d := Delivery{ID: o.ID, Destination: net.JoinHostPort(o.Host, strconv.Itoa(o.Port)), Inform: o.Inform}
	if err != nil {
		d.Error = err.Error()
	} else {
		d.Acknowledged = o.Inform
	}
	return d
}

func (nt *notifier) stats() []DestinationStats {
	out := make([]DestinationStats, len(nt.out))
	for i, o := range nt.out {
		o.mu.Lock()
		last := o.lastError
		o.mu.Unlock()
		out[i] = DestinationStats{ID: o.ID, Sent: o.sent.Load(), Failed: o.failed.Load(),
			Dropped: o.dropped.Load(), LastError: last}
	}
	return out
}

// Notify has the agent send the notification named name to each of its
// destinations now, and reports what became of it at each, in the order they
// are configured. An INFORM's acknowledgement is waited for.
func (a *Agent) Notify(name string) ([]Delivery, error) {
	nt := a.notify
	if nt == nil {
		return nil, ErrNoDestination
	}
	if !nt.running.Load() || nt.ctx.Err() != nil {
		return nil, ErrNotRunning
	}
	n, ok := nt.find(name)
	if !ok {
		return nil, fmt.Errorf("simulator: %q is not a notification this device sends", name)
	}
	if !nt.limit.allow(time.Now()) {
		nt.suppressed.Add(1)
		return nil, ErrTooFast
	}
	done := make(chan Delivery, len(nt.out))
	nt.enqueue(n, done)
	got := make(map[string]Delivery, len(nt.out))
	for range nt.out {
		select {
		case d := <-done:
			got[d.ID] = d
		case <-nt.ctx.Done():
			return nil, ErrNotRunning
		}
	}
	out := make([]Delivery, len(nt.out))
	for i, o := range nt.out {
		out[i] = got[o.ID]
	}
	return out, nil
}

// authFailed is told of a request refused as not authentic — a community this
// device does not have, a user it does not know, a wrong digest — and sends
// authenticationFailure when the device is configured to, about once a second
// at most whatever the rate of refusals.
func (a *Agent) authFailed() {
	if nt := a.notify; nt != nil && nt.onAuthFailure && nt.authLimit.allow(time.Now()) {
		nt.fire(authenticationFailure)
	}
}

// bucket lets rate events a second through on average, and burst at once.
type bucket struct {
	mu          sync.Mutex
	rate, burst float64
	tokens      float64
	last        time.Time
}

func newBucket(rate, burst float64) *bucket { return &bucket{rate: rate, burst: burst, tokens: burst} }

func (b *bucket) allow(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.last.IsZero() {
		b.tokens = min(b.burst, b.tokens+now.Sub(b.last).Seconds()*b.rate)
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
