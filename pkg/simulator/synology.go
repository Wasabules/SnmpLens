package simulator

import (
	"fmt"
	"time"

	"github.com/gosnmp/gosnmp"
)

// synologyNAS is a DS920+ running DSM, whose agent is net-snmp — so its
// sysObjectID is net-snmp's Linux one, as a real DiskStation's is — with
// Synology's own system, disk and RAID tables beside the host resources and
// the interfaces.
var synologyNAS = model{
	ModelInfo:     ModelInfo{ID: "synology-nas", Category: "storage"},
	enterprise:    8072, // net-snmp
	build:         buildSynologyNAS,
	notifications: []Notification{linkNotification(false, 3), linkNotification(true, 2)},
}

func buildSynologyNAS(id Identity) []Object {
	var o objects
	addSystem(&o, id, fmt.Sprintf("Linux %s 4.4.302+ #69057 SMP Fri Jan 12 17:02:28 CST 2024 x86_64", id.Name),
		".1.3.6.1.4.1.8072.3.2.10", "admin@"+id.Name, "Office, storage shelf", 76)
	addInterfaces(&o, id.Seed, []iface{
		{index: 1, descr: "lo", ifType: ifTypeSoftwareLoopback, mtu: 65536, speed: 10_000_000,
			up: true, inRate: 5_000, outRate: 5_000},
		{index: 2, descr: "eth0", ifType: ifTypeEthernet, mtu: 1500, speed: 1_000_000_000,
			mac: deviceMAC(id.Seed, 1), up: true, inRate: 3_800_000, outRate: 1_100_000, errorRate: 0.0002},
		{index: 3, descr: "eth1", ifType: ifTypeEthernet, mtu: 1500, speed: 1_000_000_000,
			mac: deviceMAC(id.Seed, 2)},
	})
	addHostResources(&o, id.Seed, hostResources{
		memoryKiB: 4194304, // 4 GiB
		users:     [2]float64{0, 1},
		processes: [2]float64{260, 320},
		storage: []storageArea{
			{1, hrStorageRam, "Physical memory", 1024, 4194304, 0.4, 0.6, 4 * time.Minute},
			{3, hrStorageVirtualMemory, "Virtual memory", 1024, 6291456, 0.3, 0.45, 5 * time.Minute},
			{31, hrStorageFixedDisk, "/", 4096, 614400, 0.55, 0.56, 6 * time.Hour},
			{51, hrStorageFixedDisk, "/volume1", 65536, 332640000, 0.37, 0.38, 12 * time.Hour},
		},
		processors: []int{196608, 196609, 196610, 196611},
		cpuLo:      2,
		cpuHi:      30,
	})

	// SYNOLOGY-SYSTEM-MIB, -DISK-MIB and -RAID-MIB. "Normal" is 1 throughout.
	const syno = "1.3.6.1.4.1.6574."
	swing := func(n uint64, period time.Duration) Swing { return Swing{Period: period, Seed: id.Seed + 30000 + n} }
	o.add(syno+"1.1.0", gosnmp.Integer, Const(1))                                       // systemStatus
	o.add(syno+"1.2.0", gosnmp.Integer, IntegerGauge(39, 45, swing(0, 40*time.Minute))) // °C
	o.add(syno+"1.3.0", gosnmp.Integer, Const(1))                                       // powerStatus
	o.add(syno+"1.4.1.0", gosnmp.Integer, Const(1))                                     // systemFanStatus
	o.add(syno+"1.4.2.0", gosnmp.Integer, Const(1))                                     // cpuFanStatus
	o.add(syno+"1.5.1.0", gosnmp.OctetString, Const("DS920+"))
	o.add(syno+"1.5.2.0", gosnmp.OctetString, Const(fmt.Sprintf("20B0PDN%06d", int(unit(id.Seed+31)*1e6))))
	o.add(syno+"1.5.3.0", gosnmp.OctetString, Const("DSM 7.2.1-69057 Update 5"))
	o.add(syno+"1.5.4.0", gosnmp.Integer, Const(2)) // upgradeAvailable: unavailable

	// Four disks, numbered from 0 as DSM numbers them.
	for disk := 0; disk < 4; disk++ {
		col := func(c int) string { return fmt.Sprintf(syno+"2.1.1.%d.%d", c, disk) }
		o.add(col(1), gosnmp.Integer, Const(disk))
		o.add(col(2), gosnmp.OctetString, Const(fmt.Sprintf("Disk %d", disk+1)))
		o.add(col(3), gosnmp.OctetString, Const("ST8000VN004-2M2101"))
		o.add(col(4), gosnmp.OctetString, Const("SATA"))
		o.add(col(5), gosnmp.Integer, Const(1)) // diskStatus
		o.add(col(6), gosnmp.Integer, IntegerGauge(33, 38, swing(10+uint64(disk), time.Hour)))
		o.add(col(7), gosnmp.OctetString, Const("data"))
		o.add(col(8), gosnmp.Integer, Const(0))   // retries
		o.add(col(9), gosnmp.Integer, Const(0))   // bad sectors
		o.add(col(10), gosnmp.Integer, Const(0))  // identify failures
		o.add(col(11), gosnmp.Integer, Const(-1)) // remaining life: a hard disk reports none
		o.add(col(12), gosnmp.OctetString, Const(fmt.Sprintf("Drive %d", disk+1)))
		o.add(col(13), gosnmp.Integer, Const(1)) // diskHealthStatus
	}

	// One volume over the four disks, SHR: 21.8 TB, 62% free. The sizes are
	// Counter64 in the MIB, so SNMPv1 does not see them.
	raid := func(c int) string { return fmt.Sprintf(syno+"3.1.1.%d.0", c) }
	o.add(raid(1), gosnmp.Integer, Const(0))
	o.add(raid(2), gosnmp.OctetString, Const("Volume 1"))
	o.add(raid(3), gosnmp.Integer, Const(1)) // raidStatus
	o.add(raid(4), gosnmp.Counter64, Const(uint64(13_516_000_000_000)))
	o.add(raid(5), gosnmp.Counter64, Const(uint64(21_800_000_000_000)))
	o.add(raid(6), gosnmp.Integer, Const(0)) // hot spares
	return o
}
