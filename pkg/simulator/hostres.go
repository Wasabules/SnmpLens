package simulator

import (
	"fmt"
	"time"

	"github.com/gosnmp/gosnmp"
)

// The hrStorageTypes of HOST-RESOURCES-TYPES the models use.
const (
	hrStorageRam           = ".1.3.6.1.2.1.25.2.1.2"
	hrStorageVirtualMemory = ".1.3.6.1.2.1.25.2.1.3"
	hrStorageFixedDisk     = ".1.3.6.1.2.1.25.2.1.4"
)

// hostResources is what HOST-RESOURCES-MIB (RFC 2790) says of a machine.
type hostResources struct {
	memoryKiB int
	// users and processes are the ranges hrSystemNumUsers and
	// hrSystemProcesses swing in.
	users, processes [2]float64
	storage          []storageArea
	// processors are the hrDeviceIndex of each processor, which an agent
	// chooses: net-snmp numbers them from 196608, Windows after its disks.
	processors   []int
	cpuLo, cpuHi float64
}

type storageArea struct {
	index       int
	kind, descr string
	unit, size  int     // the allocation unit in bytes, and the size in units
	lo, hi      float64 // the share of the size in use
	period      time.Duration
}

// addHostResources adds the system scalars, the storage table and the
// processor table: everything the "Server (HOST-RESOURCES-MIB)" preset polls.
func addHostResources(o *objects, seed uint64, hr hostResources) {
	swing := func(n uint64, period time.Duration) Swing {
		return Swing{Period: period, Seed: seed + 1<<16 + n}
	}
	o.add("1.3.6.1.2.1.25.1.1.0", gosnmp.TimeTicks, Uptime())
	o.add("1.3.6.1.2.1.25.1.5.0", gosnmp.Gauge32, Gauge(hr.users[0], hr.users[1], swing(0, 20*time.Minute)))
	o.add("1.3.6.1.2.1.25.1.6.0", gosnmp.Gauge32, Gauge(hr.processes[0], hr.processes[1], swing(1, 7*time.Minute)))
	o.add("1.3.6.1.2.1.25.2.2.0", gosnmp.Integer, Const(hr.memoryKiB))

	for i, st := range hr.storage {
		col := func(n int) string { return fmt.Sprintf("1.3.6.1.2.1.25.2.3.1.%d.%d", n, st.index) }
		o.add(col(1), gosnmp.Integer, Const(st.index))
		o.add(col(2), gosnmp.ObjectIdentifier, Const(st.kind))
		o.add(col(3), gosnmp.OctetString, Const(st.descr))
		o.add(col(4), gosnmp.Integer, Const(st.unit))
		o.add(col(5), gosnmp.Integer, Const(st.size))
		o.add(col(6), gosnmp.Integer, IntegerGauge(st.lo*float64(st.size), st.hi*float64(st.size),
			swing(100+uint64(i), st.period)))
	}

	for i, index := range hr.processors {
		o.add(fmt.Sprintf("1.3.6.1.2.1.25.3.3.1.1.%d", index), gosnmp.ObjectIdentifier, Const(".0.0"))
		o.add(fmt.Sprintf("1.3.6.1.2.1.25.3.3.1.2.%d", index), gosnmp.Integer,
			IntegerGauge(hr.cpuLo, hr.cpuHi, swing(200+uint64(i), 90*time.Second)))
	}
}
