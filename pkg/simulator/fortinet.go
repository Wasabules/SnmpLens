package simulator

import (
	"fmt"
	"time"

	"github.com/gosnmp/gosnmp"
)

// fortiGate60F is a FortiGate 60F on FortiOS 7.2, a branch firewall: two WAN
// ports, the backup one down; the DMZ; the internal switch; the FortiLink port,
// with no FortiSwitch behind it; and the SSL VPN tunnel. FORTINET-FORTIGATE-MIB
// gives the system scalars a FortiGate is watched by — processor, memory, disk,
// sessions — and FORTINET-CORE-MIB the serial number its notifications carry.
// Its sysObjectID is fgt60F, under fgModel, which is how a monitoring tool
// recognises a FortiGate.
var fortiGate60F = model{
	ModelInfo:  ModelInfo{ID: "fortigate-60f", Category: "security"},
	enterprise: 12356, // Fortinet
	build:      buildFortiGate60F,
	notifications: []Notification{
		linkNotification(false, 2), // wan2, the backup, down
		linkNotification(true, 1),  // wan1
		// FORTINET-CORE-MIB: a processor or memory threshold crossed.
		{Name: "fnTrapCpuThreshold", OID: ".1.3.6.1.4.1.12356.100.1.3.0.101",
			Objects: []string{"." + fnSysSerial, ".1.3.6.1.2.1.1.5.0"}},
		{Name: "fnTrapMemThreshold", OID: ".1.3.6.1.4.1.12356.100.1.3.0.102",
			Objects: []string{"." + fnSysSerial, ".1.3.6.1.2.1.1.5.0", "." + fnGenTrapMsg}},
	},
}

const (
	fnSysSerial = "1.3.6.1.4.1.12356.100.1.1.1.0"
	// fnGenTrapMsg is accessible-for-notify: the text a notification carries.
	fnGenTrapMsg = "1.3.6.1.4.1.12356.100.1.3.1.1.0"
)

func buildFortiGate60F(id Identity) []Object {
	var o objects
	addSystem(&o, id, "FortiGate-60F", ".1.3.6.1.4.1.12356.101.1.644", // fgt60F
		"secops@example.com", "Branch office, network room", 78)
	port := func(index int, descr, alias string) iface {
		return iface{index: index, descr: descr, alias: alias, ifType: ifTypeEthernet, mtu: 1500,
			speed: 1_000_000_000, mac: deviceMAC(id.Seed, uint64(index))}
	}
	wan1 := port(1, "wan1", "ISP fibre")
	wan1.up, wan1.inRate, wan1.outRate, wan1.errorRate = true, 4_800_000, 1_100_000, 0.001
	dmz := port(3, "dmz", "DMZ servers")
	dmz.up, dmz.inRate, dmz.outRate = true, 600_000, 2_100_000
	internal := port(4, "internal", "LAN")
	internal.up, internal.inRate, internal.outRate = true, 1_300_000, 5_200_000
	addInterfaces(&o, id.Seed, []iface{
		wan1,
		port(2, "wan2", "ISP backup"),
		dmz,
		internal,
		port(5, "fortilink", ""),
		// A tunnel has no speed of its own.
		{index: 6, descr: "ssl.root", alias: "SSL VPN", ifType: ifTypeTunnel, mtu: 1500, up: true,
			inRate: 45_000, outRate: 110_000},
	})

	o.add(fnSysSerial, gosnmp.OctetString, Const(fmt.Sprintf("FGT60FTK2%07d", int(unit(id.Seed+1)*1e7))))
	o.addForNotify(fnGenTrapMsg, gosnmp.OctetString, Const("Memory usage has exceeded the configured threshold."))

	const sys = "1.3.6.1.4.1.12356.101.4.1."
	swing := func(n uint64, period time.Duration) Swing { return Swing{Period: period, Seed: id.Seed + 30000 + n} }
	o.add(sys+"1.0", gosnmp.OctetString, Const("v7.2.8,build1639,240313 (GA.M)")) // fgSysVersion
	o.add(sys+"3.0", gosnmp.Gauge32, Gauge(6, 38, swing(0, 3*time.Minute)))       // fgSysCpuUsage, %
	o.add(sys+"4.0", gosnmp.Gauge32, Gauge(47, 58, swing(1, 25*time.Minute)))     // fgSysMemUsage, %
	o.add(sys+"5.0", gosnmp.Gauge32, Const(uint32(1_906_380)))                    // fgSysMemCapacity, KB
	o.add(sys+"6.0", gosnmp.Gauge32, Gauge(212, 236, swing(2, 6*time.Hour)))      // fgSysDiskUsage, MB
	o.add(sys+"7.0", gosnmp.Gauge32, Const(uint32(1_907)))                        // fgSysDiskCapacity, MB
	o.add(sys+"8.0", gosnmp.Gauge32, Gauge(900, 4_200, swing(3, 8*time.Minute)))  // fgSysSesCount
	// fgSysUpTime counts hundredths of a second in 64 bits, where sysUpTime
	// wraps after 497 days.
	o.add(sys+"20.0", gosnmp.Counter64, Counter64(100, 0, Swing{}))
	return o
}
