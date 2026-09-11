package simulator

import (
	"cmp"
	"fmt"
	"math"
	"time"

	"github.com/gosnmp/gosnmp"
)

// The types of HOST-RESOURCES-TYPES the models use.
const (
	hrStorageOther         = ".1.3.6.1.2.1.25.2.1.1"
	hrStorageRam           = ".1.3.6.1.2.1.25.2.1.2"
	hrStorageVirtualMemory = ".1.3.6.1.2.1.25.2.1.3"
	hrStorageFixedDisk     = ".1.3.6.1.2.1.25.2.1.4"
	hrStorageFlashMemory   = ".1.3.6.1.2.1.25.2.1.9"

	hrDeviceProcessor   = ".1.3.6.1.2.1.25.3.1.3"
	hrDeviceNetwork     = ".1.3.6.1.2.1.25.3.1.4"
	hrDevicePrinter     = ".1.3.6.1.2.1.25.3.1.5"
	hrDeviceDiskStorage = ".1.3.6.1.2.1.25.3.1.6"

	hrFSOther     = ".1.3.6.1.2.1.25.3.9.1"
	hrFSNTFS      = ".1.3.6.1.2.1.25.3.9.9"
	hrFSLinuxExt2 = ".1.3.6.1.2.1.25.3.9.23"
)

// hostResources is what HOST-RESOURCES-MIB (RFC 2790) says of a machine.
type hostResources struct {
	memoryKiB int
	// users and processes are the ranges hrSystemNumUsers and
	// hrSystemProcesses swing in. A machine that lists its processes counts
	// them instead.
	users, processes [2]float64
	storage          []storageArea
	// processors are the hrDeviceIndex of each processor, which an agent
	// chooses: net-snmp numbers them from 196608, Windows after its disks.
	processors   []int
	cpuLo, cpuHi float64
	// cpu names the processors in hrDeviceTable.
	cpu string
	// boot is hrSystemInitialLoadDevice, and bootParams what it started with.
	boot       int
	bootParams string
	devices    []hrDevice
	fs         []hrFS
	procs      []process
	software   []software
}

type storageArea struct {
	index       int
	kind, descr string
	unit, size  int     // the allocation unit in bytes, and the size in units
	lo, hi      float64 // the share of the size in use
	period      time.Duration
}

// hrDevice is a device of hrDeviceTable other than a processor.
type hrDevice struct {
	index int
	kind  string // an hrDeviceType
	descr string
	// ifIndex is a network device's interface (hrNetworkTable).
	ifIndex int
	// diskKB is a disk's capacity (hrDiskStorageTable), media its
	// hrDiskStorageMedia: hardDisk(3), ramDisk(8).
	diskKB, media int
	removable     bool
}

// hrFS is a file system of hrFSTable, on the storage area storage.
type hrFS struct {
	mount, kind string
	storage     int
	bootable    bool
}

// process is a row of hrSWRunTable, and what it uses (hrSWRunPerfTable).
type process struct {
	pid              int
	name, path, args string
	// kind is hrSWRunType: operatingSystem(2), deviceDriver(3), application(4).
	kind  int
	cpu   float64 // the share of one processor it uses
	memKB int
}

// software is a row of hrSWInstalledTable.
type software struct {
	name      string
	installed time.Time
}

// noDate is the DateAndTime a file system that was never backed up reports.
var noDate = []byte{0, 0, 1, 1, 0, 0, 0, 0}

// addHostResources adds HOST-RESOURCES-MIB: the system scalars, the storage
// table, the devices — the processors among them — the file systems, and what
// is running and installed.
func addHostResources(o *objects, seed uint64, hr hostResources) {
	swing := func(n uint64, period time.Duration) Swing {
		return Swing{Period: period, Seed: seed + 1<<16 + n}
	}
	processes := Gauge(hr.processes[0], hr.processes[1], swing(1, 7*time.Minute))
	if len(hr.procs) > 0 {
		processes = Const(uint32(len(hr.procs)))
	}
	o.add("1.3.6.1.2.1.25.1.1.0", gosnmp.TimeTicks, Uptime())
	o.add("1.3.6.1.2.1.25.1.2.0", gosnmp.OctetString, DateAndTime())
	o.add("1.3.6.1.2.1.25.1.3.0", gosnmp.Integer, Const(cmp.Or(hr.boot, 1536)))
	o.add("1.3.6.1.2.1.25.1.4.0", gosnmp.OctetString, Const(hr.bootParams))
	o.add("1.3.6.1.2.1.25.1.5.0", gosnmp.Gauge32, Gauge(hr.users[0], hr.users[1], swing(0, 20*time.Minute)))
	o.add("1.3.6.1.2.1.25.1.6.0", gosnmp.Gauge32, processes)
	o.add("1.3.6.1.2.1.25.1.7.0", gosnmp.Integer, Const(0)) // no fixed limit
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
		o.add(col(7), gosnmp.Counter32, Const(uint32(0)))
	}

	device := func(index int, kind, descr string) {
		col := func(n int) string { return fmt.Sprintf("1.3.6.1.2.1.25.3.2.1.%d.%d", n, index) }
		o.add(col(1), gosnmp.Integer, Const(index))
		o.add(col(2), gosnmp.ObjectIdentifier, Const(kind))
		o.add(col(3), gosnmp.OctetString, Const(descr))
		o.add(col(4), gosnmp.ObjectIdentifier, Const(".0.0"))
		o.add(col(5), gosnmp.Integer, Const(2)) // running
		o.add(col(6), gosnmp.Counter32, Const(uint32(0)))
	}
	for i, index := range hr.processors {
		device(index, hrDeviceProcessor, cmp.Or(hr.cpu, "GenuineIntel: Intel(R) Xeon(R) CPU"))
		o.add(fmt.Sprintf("1.3.6.1.2.1.25.3.3.1.1.%d", index), gosnmp.ObjectIdentifier, Const(".0.0"))
		o.add(fmt.Sprintf("1.3.6.1.2.1.25.3.3.1.2.%d", index), gosnmp.Integer,
			IntegerGauge(hr.cpuLo, hr.cpuHi, swing(200+uint64(i), 90*time.Second)))
	}
	for _, d := range hr.devices {
		device(d.index, d.kind, d.descr)
		switch d.kind {
		case hrDeviceNetwork:
			o.add(fmt.Sprintf("1.3.6.1.2.1.25.3.4.1.1.%d", d.index), gosnmp.Integer, Const(d.ifIndex))
		case hrDevicePrinter:
			o.add(fmt.Sprintf("1.3.6.1.2.1.25.3.5.1.1.%d", d.index), gosnmp.Integer, Const(3)) // idle
			o.add(fmt.Sprintf("1.3.6.1.2.1.25.3.5.1.2.%d", d.index), gosnmp.OctetString, Const([]byte{0x00}))
		case hrDeviceDiskStorage:
			removable := 2
			if d.removable {
				removable = 1
			}
			col := func(n int) string { return fmt.Sprintf("1.3.6.1.2.1.25.3.6.1.%d.%d", n, d.index) }
			o.add(col(1), gosnmp.Integer, Const(1)) // readWrite
			o.add(col(2), gosnmp.Integer, Const(cmp.Or(d.media, 3)))
			o.add(col(3), gosnmp.Integer, Const(removable))
			// KBytes is an Integer32: a disk past 2 TB reports the ceiling, as
			// real agents do.
			o.add(col(4), gosnmp.Integer, Const(min(d.diskKB, math.MaxInt32)))
		}
	}

	for i, fs := range hr.fs {
		n := i + 1
		col := func(c int) string { return fmt.Sprintf("1.3.6.1.2.1.25.3.8.1.%d.%d", c, n) }
		bootable := 2
		if fs.bootable {
			bootable = 1
		}
		o.add(col(1), gosnmp.Integer, Const(n))
		o.add(col(2), gosnmp.OctetString, Const(fs.mount))
		o.add(col(3), gosnmp.OctetString, Const(""))
		o.add(col(4), gosnmp.ObjectIdentifier, Const(fs.kind))
		o.add(col(5), gosnmp.Integer, Const(1)) // readWrite
		o.add(col(6), gosnmp.Integer, Const(bootable))
		o.add(col(7), gosnmp.Integer, Const(fs.storage))
		o.add(col(8), gosnmp.OctetString, Const(noDate))
		o.add(col(9), gosnmp.OctetString, Const(noDate))
	}

	// hrSWRunTable and hrSWRunPerfTable, indexed by the process ID. A process
	// that uses the processor at all is running, the rest wait (runnable), as
	// net-snmp reads a sleeping process.
	if len(hr.procs) > 0 {
		os := hr.procs[0].pid
		for _, p := range hr.procs {
			if p.kind == 2 {
				os = p.pid
				break
			}
		}
		o.add("1.3.6.1.2.1.25.4.1.0", gosnmp.Integer, Const(os))
	}
	for i, p := range hr.procs {
		col := func(c int) string { return fmt.Sprintf("1.3.6.1.2.1.25.4.2.1.%d.%d", c, p.pid) }
		perf := func(c int) string { return fmt.Sprintf("1.3.6.1.2.1.25.5.1.1.%d.%d", c, p.pid) }
		status := 2
		if p.cpu >= 0.05 {
			status = 1
		}
		o.add(col(1), gosnmp.Integer, Const(p.pid))
		o.add(col(2), gosnmp.OctetString, Const(p.name))
		o.add(col(3), gosnmp.ObjectIdentifier, Const(".0.0"))
		o.add(col(4), gosnmp.OctetString, Const(p.path))
		o.add(col(5), gosnmp.OctetString, Const(p.args))
		o.add(col(6), gosnmp.Integer, Const(cmp.Or(p.kind, 4)))
		o.add(col(7), gosnmp.Integer, Const(status))
		// Centi-seconds of processor since it started, and its memory.
		n := 300 + uint64(i)
		o.add(perf(1), gosnmp.Integer, IntegerCounter(p.cpu*100, uint64(100+4e5*p.cpu*unit(seed+n)),
			Swing{Depth: 0.8, Period: 3 * time.Minute, Seed: seed + n}))
		o.add(perf(2), gosnmp.Integer, IntegerGauge(float64(p.memKB)*0.97, float64(p.memKB)*1.03, swing(n, 10*time.Minute)))
	}

	if len(hr.software) > 0 {
		o.add("1.3.6.1.2.1.25.6.1.0", gosnmp.TimeTicks, Const(uint32(0)))
		o.add("1.3.6.1.2.1.25.6.2.0", gosnmp.TimeTicks, Const(uint32(0)))
	}
	for i, s := range hr.software {
		n := i + 1
		col := func(c int) string { return fmt.Sprintf("1.3.6.1.2.1.25.6.3.1.%d.%d", c, n) }
		o.add(col(1), gosnmp.Integer, Const(n))
		o.add(col(2), gosnmp.OctetString, Const(s.name))
		o.add(col(3), gosnmp.ObjectIdentifier, Const(".0.0"))
		o.add(col(4), gosnmp.Integer, Const(4)) // application
		o.add(col(5), gosnmp.OctetString, Const(dateAndTimeOf(s.installed)))
	}
}
