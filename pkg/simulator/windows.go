package simulator

import (
	"fmt"
	"time"
)

// windowsServer is a Windows Server 2022 host with the SNMP service: the system
// group as Windows answers it, the loopback and two adapters, and the host
// resources — its drives by letter, its memory and four processors, numbered as
// Windows numbers them. It fits the "Interfaces" and "Host resources" presets.
var windowsServer = model{
	ModelInfo:     ModelInfo{ID: "windows-server", Category: "server"},
	enterprise:    311, // Microsoft
	build:         buildWindowsServer,
	notifications: []Notification{linkNotification(false, 7), linkNotification(true, 6)},
}

func buildWindowsServer(id Identity) []Object {
	var o objects
	addSystem(&o, id,
		"Hardware: Intel64 Family 6 Model 85 Stepping 7 AT/AT COMPATIBLE - Software: Windows Version 6.3 (Build 20348 Multiprocessor Free)",
		".1.3.6.1.4.1.311.1.1.3.1.2", // a Windows server
		"Administrator", "Server room, rack B2", 76)
	addInterfaces(&o, id.Seed, []iface{
		{index: 1, descr: "Software Loopback Interface 1", name: "loopback_0", ifType: ifTypeSoftwareLoopback,
			mtu: 1500, speed: 1_073_741_824, up: true},
		{index: 6, descr: "Intel(R) Ethernet Server Adapter I350-T2", name: "ethernet_32768", ifType: ifTypeEthernet,
			mtu: 1500, speed: 1_000_000_000, mac: deviceMAC(id.Seed, 1), up: true,
			inRate: 2_100_000, outRate: 4_600_000, errorRate: 0.001},
		{index: 7, descr: "Intel(R) Ethernet Server Adapter I350-T2 #2", name: "ethernet_32769", ifType: ifTypeEthernet,
			mtu: 1500, speed: 1_000_000_000, mac: deviceMAC(id.Seed, 2)},
	})
	// A volume serial number, as Windows writes one into hrStorageDescr.
	serial := func(n uint64) string { return fmt.Sprintf("%08x", uint32(mix(id.Seed+n))) }
	addHostResources(&o, id.Seed, hostResources{
		memoryKiB: 16777216, // 16 GiB
		users:     [2]float64{1, 3},
		processes: [2]float64{110, 140},
		storage: []storageArea{
			{1, hrStorageFixedDisk, `C:\ Label:  Serial Number ` + serial(1), 4096, 32768000, 0.46, 0.52, 3 * time.Hour},
			{2, hrStorageFixedDisk, `D:\ Label:Data  Serial Number ` + serial(2), 4096, 131072000, 0.61, 0.62, 12 * time.Hour},
			{3, hrStorageVirtualMemory, "Virtual Memory", 65536, 327680, 0.35, 0.55, 6 * time.Minute},
			{4, hrStorageRam, "Physical Memory", 65536, 262144, 0.5, 0.7, 4 * time.Minute},
		},
		processors: []int{5, 6, 7, 8},
		cpuLo:      3,
		cpuHi:      45,
	})
	return o
}
