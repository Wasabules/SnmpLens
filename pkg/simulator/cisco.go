package simulator

import (
	"fmt"
	"time"

	"github.com/gosnmp/gosnmp"
)

// The Catalyst 2960, Cisco's access switch of the last fifteen years: Fast
// Ethernet ports, two gigabit uplinks and VLAN 1 — what the "Switch ports"
// preset draws and, with 24 ports, "Switch drawing (24 ports)". Its
// sysObjectID is Cisco's, so the "Cisco (IF-MIB)" preset is offered first for
// it, as it is for a real one.
//
// The ports are numbered from 1, as the drawing preset polls them, where a
// real 2960 numbers them from 10001; the names are a 2960's.
var (
	catalyst24 = model{
		ModelInfo:  ModelInfo{ID: "cisco-catalyst-24", Category: "network"},
		enterprise: 9, // Cisco
		build: func(id Identity) []Object {
			return buildCatalyst(id, 24, ".1.3.6.1.4.1.9.1.716") // catalyst296024TT
		},
		notifications: catalystNotifications(24),
	}
	catalyst48 = model{
		ModelInfo:  ModelInfo{ID: "cisco-catalyst-48", Category: "network"},
		enterprise: 9,
		build: func(id Identity) []Object {
			return buildCatalyst(id, 48, ".1.3.6.1.4.1.9.1.717") // catalyst296048TT
		},
		notifications: catalystNotifications(48),
	}
)

const catalystDescr = "Cisco IOS Software, C2960 Software (C2960-LANBASEK9-M), Version 15.0(2)SE11, RELEASE SOFTWARE (fc3)\r\n" +
	"Technical Support: http://www.cisco.com/techsupport\r\n" +
	"Copyright (c) 1986-2017 by Cisco Systems, Inc.\r\n" +
	"Compiled Sat 19-Aug-17 09:34 by prod_rel_team"

func buildCatalyst(id Identity, ports int, sysObjectID string) []Object {
	var o objects
	addSystem(&o, id, catalystDescr, sysObjectID, "noc@example.com", "Wiring closet, floor 2", 2) // layer 2
	ifs := make([]iface, 0, ports+3)
	for p := 1; p <= ports; p++ {
		f := iface{index: p, descr: fmt.Sprintf("FastEthernet0/%d", p), name: fmt.Sprintf("Fa0/%d", p),
			ifType: ifTypeEthernet, mtu: 1500, speed: 100_000_000, mac: deviceMAC(id.Seed, uint64(p))}
		// Most of an access switch's ports are in use and some are not, which is
		// also why no bundled preset watches ifOperStatus. The first is always up:
		// it is where the presets look for traffic.
		switch r := unit(id.Seed + 7000 + uint64(p)); {
		case p == 1:
			f.up, f.alias, f.inRate, f.outRate = true, "srv-01", 900_000, 2_400_000
		case r < 0.06:
			f.adminDown = true
		case r < 0.64:
			f.up, f.alias = true, fmt.Sprintf("desk 2-%02d", p)
			f.inRate = 4_000 + 120_000*unit(id.Seed+7100+uint64(p))
			f.outRate = 8_000 + 300_000*unit(id.Seed+7200+uint64(p))
			f.errorRate = 0.0005
		}
		ifs = append(ifs, f)
	}
	ifs = append(ifs,
		iface{index: ports + 1, descr: "GigabitEthernet0/1", name: "Gi0/1", alias: "uplink core-sw-01",
			ifType: ifTypeEthernet, mtu: 1500, speed: 1_000_000_000, mac: deviceMAC(id.Seed, uint64(ports+1)),
			up: true, inRate: 6_500_000, outRate: 2_800_000, errorRate: 0.001},
		iface{index: ports + 2, descr: "GigabitEthernet0/2", name: "Gi0/2", alias: "spare uplink",
			ifType: ifTypeEthernet, mtu: 1500, speed: 1_000_000_000, mac: deviceMAC(id.Seed, uint64(ports+2))},
		// VLAN 1 carries the switch's own address, and its base MAC — the one
		// its engine ID carries too.
		iface{index: ports + 3, descr: "Vlan1", name: "Vl1", ifType: ifTypePropVirtual, mtu: 1500,
			speed: 1_000_000_000, mac: deviceMAC(id.Seed, 0), up: true, inRate: 2_000, outRate: 1_500},
	)
	addInterfaces(&o, id.Seed, ifs)
	addCiscoCPU(&o, id.Seed, 3, 24)
	addCiscoConfigHistory(&o)
	return o
}

// catalystNotifications are a Catalyst's: linkDown about its spare uplink,
// which is down, linkUp about the one in use, and a configuration change.
func catalystNotifications(ports int) []Notification {
	return []Notification{linkNotification(false, ports+2), linkNotification(true, ports+1), ciscoConfigManEvent}
}

// isr4331 is a branch router: a WAN port, a LAN port, a backup WAN port that is
// down, the management port, and two eBGP sessions, one established and one
// not.
var isr4331 = model{
	ModelInfo:  ModelInfo{ID: "cisco-isr-4331", Category: "network"},
	enterprise: 9,
	build:      buildISR4331,
	notifications: []Notification{
		linkNotification(false, 3), // the backup WAN, down
		linkNotification(true, 1),  // the WAN
		bgpNotification("bgpEstablishedNotification", 1, bgpPeers[0].remote),
		bgpNotification("bgpBackwardTransNotification", 2, bgpPeers[1].remote),
		ciscoConfigManEvent,
	},
}

const isrDescr = "Cisco IOS XE Software, Version 17.09.04a\r\n" +
	"Cisco IOS Software [Cupertino], ISR Software (X86_64_LINUX_IOSD-UNIVERSALK9-M), Version 17.9.4a, RELEASE SOFTWARE (fc3)\r\n" +
	"Technical Support: http://www.cisco.com/techsupport\r\n" +
	"Copyright (c) 1986-2023 by Cisco Systems, Inc.\r\n" +
	"Compiled Fri 20-Oct-23 10:44 by mcpre"

func buildISR4331(id Identity) []Object {
	var o objects
	addSystem(&o, id, isrDescr, ".1.3.6.1.4.1.9.1.2068", // ciscoISR4331
		"noc@example.com", "Branch office, network room", 78)
	gig := func(index int, descr, name, alias string) iface {
		return iface{index: index, descr: descr, name: name, alias: alias, ifType: ifTypeEthernet, mtu: 1500,
			speed: 1_000_000_000, mac: deviceMAC(id.Seed, uint64(index))}
	}
	wan := gig(1, "GigabitEthernet0/0/0", "Gi0/0/0", "WAN transit 203.0.113.0/30")
	wan.up, wan.inRate, wan.outRate, wan.errorRate = true, 3_200_000, 900_000, 0.002
	lan := gig(2, "GigabitEthernet0/0/1", "Gi0/0/1", "LAN")
	lan.up, lan.inRate, lan.outRate = true, 850_000, 3_000_000
	mgmt := gig(4, "GigabitEthernet0", "Gi0", "management")
	mgmt.up, mgmt.inRate, mgmt.outRate = true, 3_000, 5_000
	addInterfaces(&o, id.Seed, []iface{
		wan,
		lan,
		gig(3, "GigabitEthernet0/0/2", "Gi0/0/2", "backup WAN"),
		mgmt,
		{index: 5, descr: "Null0", name: "Nu0", ifType: ifTypeOther, mtu: 1500, speed: 10_000_000_000, up: true},
		{index: 6, descr: "Loopback0", name: "Lo0", ifType: ifTypeSoftwareLoopback, mtu: 1514,
			speed: 8_000_000_000, up: true},
	})
	addCiscoCPU(&o, id.Seed, 2, 14)
	addBGP(&o, id.Seed)
	addCiscoConfigHistory(&o)
	return o
}

// addCiscoCPU adds CISCO-PROCESS-MIB's cpmCPUTotalTable for one processor: its
// load over five seconds, a minute and five minutes, the longer averages
// steadier than the shorter.
func addCiscoCPU(o *objects, seed uint64, lo, hi float64) {
	col := func(n int) string { return fmt.Sprintf("1.3.6.1.4.1.9.9.109.1.1.1.1.%d.1", n) }
	span := hi - lo
	o.add(col(6), gosnmp.Gauge32, Gauge(lo, hi, Swing{Period: 2 * time.Minute, Seed: seed + 9000}))
	o.add(col(7), gosnmp.Gauge32, Gauge(lo+span*0.2, hi-span*0.3, Swing{Period: 10 * time.Minute, Seed: seed + 9001}))
	o.add(col(8), gosnmp.Gauge32, Gauge(lo+span*0.3, hi-span*0.5, Swing{Period: 30 * time.Minute, Seed: seed + 9002}))
}

// ciscoConfigManEvent is CISCO-CONFIG-MAN-MIB's notice of a configuration
// change, carrying the history row that records it.
var ciscoConfigManEvent = Notification{
	Name: "ciscoConfigManEvent",
	OID:  ".1.3.6.1.4.1.9.9.43.2.0.1",
	// ccmHistoryEventCommandSource, …ConfigSource, …ConfigDestination
	Objects: []string{
		".1.3.6.1.4.1.9.9.43.1.1.6.1.3.1",
		".1.3.6.1.4.1.9.9.43.1.1.6.1.4.1",
		".1.3.6.1.4.1.9.9.43.1.1.6.1.5.1",
	},
}

// addCiscoConfigHistory adds the one history row ciscoConfigManEvent carries: a
// change typed at the command line into the running configuration.
func addCiscoConfigHistory(o *objects) {
	col := func(n int) string { return fmt.Sprintf("1.3.6.1.4.1.9.9.43.1.1.6.1.%d.1", n) }
	o.add(col(3), gosnmp.Integer, Const(1)) // command source: commandLine
	o.add(col(4), gosnmp.Integer, Const(2)) // configuration source: commandSource
	o.add(col(5), gosnmp.Integer, Const(3)) // configuration destination: running
}

// The router's two eBGP sessions: to its transit provider, established since the
// router started, and to a second provider that is not — which is what
// bgpBackwardTransNotification is about.
var bgpPeers = []struct {
	remote, local, identifier string
	as                        int
	established               bool
}{
	{remote: "203.0.113.1", local: "203.0.113.2", identifier: "203.0.113.1", as: 64500, established: true},
	{remote: "198.51.100.9", local: "0.0.0.0", identifier: "0.0.0.0", as: 64501},
}

// bgpNotification is one of BGP4-MIB's two (RFC 4273) about a peer: its
// address, its last error and its state.
func bgpNotification(name string, n int, peer string) Notification {
	col := func(c int) string { return fmt.Sprintf(".1.3.6.1.2.1.15.3.1.%d.%s", c, peer) }
	return Notification{Name: name, OID: fmt.Sprintf(".1.3.6.1.2.1.15.0.%d", n), Objects: []string{col(7), col(14), col(2)}}
}

// addBGP adds BGP4-MIB's scalars and its peer table, indexed by the peer's
// address.
func addBGP(o *objects, seed uint64) {
	o.add("1.3.6.1.2.1.15.1.0", gosnmp.OctetString, Const([]byte{0x10})) // bgpVersion: 4
	o.add("1.3.6.1.2.1.15.2.0", gosnmp.Integer, Const(65010))            // bgpLocalAs
	o.add("1.3.6.1.2.1.15.4.0", gosnmp.IPAddress, Const("10.255.0.1"))   // bgpIdentifier
	for i, p := range bgpPeers {
		col := func(c int) string { return fmt.Sprintf("1.3.6.1.2.1.15.3.1.%d.%s", c, p.remote) }
		n := uint64(i) * 8
		swing := Swing{Depth: 0.3, Period: 20 * time.Minute, Seed: seed + 60000 + n}
		start := func(k uint64) uint64 { return uint64(1e4 + 5e4*unit(seed+60100+n+k)) }
		o.add(col(1), gosnmp.IPAddress, Const(p.identifier))
		o.add(col(3), gosnmp.Integer, Const(2)) // admin status: start
		o.add(col(5), gosnmp.IPAddress, Const(p.local))
		o.add(col(7), gosnmp.IPAddress, Const(p.remote))
		o.add(col(9), gosnmp.Integer, Const(p.as))
		// In the established state, or out of it, since the router started.
		o.add(col(16), gosnmp.Gauge32, SecondsUp())
		o.add(col(17), gosnmp.Integer, Const(120)) // connect retry interval
		o.add(col(20), gosnmp.Integer, Const(180)) // hold time configured
		o.add(col(21), gosnmp.Integer, Const(60))  // keepalive configured
		o.add(col(22), gosnmp.Integer, Const(15))  // minimum AS origination interval
		o.add(col(23), gosnmp.Integer, Const(30))  // minimum route advertisement interval
		if p.established {
			o.add(col(2), gosnmp.Integer, Const(6)) // established
			o.add(col(4), gosnmp.Integer, Const(4))
			o.add(col(6), gosnmp.Integer, Const(36011))
			o.add(col(8), gosnmp.Integer, Const(179))
			o.add(col(10), gosnmp.Counter32, Counter(0.02, start(0), swing))
			o.add(col(11), gosnmp.Counter32, Counter(0.005, start(1), swing))
			// A keepalive a minute, and the updates.
			o.add(col(12), gosnmp.Counter32, Counter(1.0/60+0.02, start(2), swing))
			o.add(col(13), gosnmp.Counter32, Counter(1.0/60+0.005, start(3), swing))
			o.add(col(14), gosnmp.OctetString, Const([]byte{0, 0}))
			o.add(col(15), gosnmp.Counter32, Const(uint32(1)))
			o.add(col(18), gosnmp.Integer, Const(180))
			o.add(col(19), gosnmp.Integer, Const(60))
			o.add(col(24), gosnmp.Gauge32, Gauge(5, 240, swing))
		} else {
			o.add(col(2), gosnmp.Integer, Const(3)) // active: trying, and not getting through
			o.add(col(4), gosnmp.Integer, Const(0))
			o.add(col(6), gosnmp.Integer, Const(0))
			o.add(col(8), gosnmp.Integer, Const(0))
			o.add(col(10), gosnmp.Counter32, Const(uint32(1482)))
			o.add(col(11), gosnmp.Counter32, Const(uint32(37)))
			o.add(col(12), gosnmp.Counter32, Const(uint32(9163)))
			o.add(col(13), gosnmp.Counter32, Const(uint32(7702)))
			o.add(col(14), gosnmp.OctetString, Const([]byte{4, 0})) // hold timer expired
			o.add(col(15), gosnmp.Counter32, Const(uint32(3)))
			o.add(col(18), gosnmp.Integer, Const(0))
			o.add(col(19), gosnmp.Integer, Const(0))
			o.add(col(24), gosnmp.Gauge32, Const(uint32(0)))
		}
	}
}
