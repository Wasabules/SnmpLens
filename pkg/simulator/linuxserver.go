package simulator

import (
	"fmt"
	"time"
)

// linuxServer is an Ubuntu host running net-snmp: the system group, three
// interfaces — the loopback, one busy, one cabled and down, which is what an
// interface wall looks like on a real server — and the host resources.
//
// It answers everything the bundled "Interfaces" and "Host resources" presets
// poll, and TestEveryModelFeedsItsPresets holds it to that: binding a preset
// to a simulated server and getting empty widgets would be a demonstration of
// the wrong thing.
var linuxServer = model{
	ModelInfo:  ModelInfo{ID: "linux-server", Category: "server"},
	enterprise: 8072, // net-snmp
	build:      buildLinuxServer,
	notifications: []Notification{
		linkNotification(false, 3), // eth1, cabled and down
		linkNotification(true, 2),  // eth0, the uplink
		// NET-SNMP-AGENT-MIB: what snmpd sends as it stops, and when it
		// restarts on a SIGHUP.
		{Name: "nsNotifyShutdown", OID: ".1.3.6.1.4.1.8072.4.0.2"},
		{Name: "nsNotifyRestart", OID: ".1.3.6.1.4.1.8072.4.0.3"},
	},
}

func buildLinuxServer(id Identity) []Object {
	var o objects
	addSystem(&o, id, fmt.Sprintf("Linux %s 6.8.0-45-generic #45-Ubuntu SMP PREEMPT_DYNAMIC x86_64", id.Name),
		".1.3.6.1.4.1.8072.3.2.10", "root@"+id.Name, "Server room, rack A1", 72)
	addInterfaces(&o, id.Seed, []iface{
		{index: 1, descr: "lo", ifType: ifTypeSoftwareLoopback, mtu: 65536, speed: 10_000_000,
			up: true, inRate: 20_000, outRate: 20_000},
		{index: 2, descr: "eth0", alias: "uplink", ifType: ifTypeEthernet, mtu: 1500, speed: 1_000_000_000,
			mac: deviceMAC(id.Seed, 1), up: true, inRate: 1_250_000, outRate: 310_000, errorRate: 0.004},
		{index: 3, descr: "eth1", alias: "spare", ifType: ifTypeEthernet, mtu: 1500, speed: 1_000_000_000,
			mac: deviceMAC(id.Seed, 2)},
	})
	addHostResources(&o, id.Seed, hostResources{
		memoryKiB: 8388608, // 8 GiB
		users:     [2]float64{1, 3},
		processes: [2]float64{180, 260},
		storage: []storageArea{
			{1, hrStorageRam, "Physical memory", 1024, 8388608, 0.55, 0.75, 4 * time.Minute},
			{3, hrStorageVirtualMemory, "Virtual memory", 1024, 10485760, 0.48, 0.62, 5 * time.Minute},
			{31, hrStorageFixedDisk, "/", 4096, 25600000, 0.41, 0.43, 6 * time.Hour},
			{36, hrStorageFixedDisk, "/home", 4096, 51200000, 0.63, 0.64, 9 * time.Hour},
		},
		// One row per CPU, indexed as net-snmp indexes hrDeviceTable.
		processors: []int{196608, 196609},
		cpuLo:      4,
		cpuHi:      55,
	})
	return o
}
