package simulator

import (
	"fmt"
	"time"

	"github.com/gosnmp/gosnmp"
)

// hpLaserJet is an HP LaserJet on its Jetdirect card, answering the Printer MIB
// (RFC 3805): its page count, a black cartridge running low and the alert that
// says so, and the printer's status in the host resources, as printers report
// it.
var hpLaserJet = model{
	ModelInfo:  ModelInfo{ID: "hp-laserjet", Category: "printing"},
	enterprise: 11, // HP
	build:      buildHPLaserJet,
	notifications: []Notification{{
		Name: "printerV2Alert",
		OID:  ".1.3.6.1.2.1.43.18.2.0.1",
		// prtAlertIndex, …SeverityLevel, …Group, …GroupIndex, …Location, …Code
		Objects: []string{
			".1.3.6.1.2.1.43.18.1.1.1.1.1",
			".1.3.6.1.2.1.43.18.1.1.2.1.1",
			".1.3.6.1.2.1.43.18.1.1.4.1.1",
			".1.3.6.1.2.1.43.18.1.1.5.1.1",
			".1.3.6.1.2.1.43.18.1.1.6.1.1",
			".1.3.6.1.2.1.43.18.1.1.7.1.1",
		},
	}},
}

func buildHPLaserJet(id Identity) []Object {
	var o objects
	addSystem(&o, id, "HP ETHERNET MULTI-ENVIRONMENT,ROM none,JETDIRECT,JD153,EEPROM JSI23900051,CIDATE 06/17/2019",
		".1.3.6.1.4.1.11.2.3.9.1", // hpNetPrinter
		"helpdesk@example.com", "Floor 1, copy room", 72)
	addInterfaces(&o, id.Seed, []iface{
		{index: 1, descr: "HP ETHERNET MULTI-ENVIRONMENT", ifType: ifTypeEthernet, mtu: 1500,
			speed: 1_000_000_000, mac: deviceMAC(id.Seed, 1), up: true, inRate: 1_500, outRate: 900},
	})

	// Host resources: the printer as a device, and its status.
	o.add("1.3.6.1.2.1.25.1.1.0", gosnmp.TimeTicks, Uptime())
	o.add("1.3.6.1.2.1.25.2.2.0", gosnmp.Integer, Const(524288)) // 512 MiB
	device := func(c int) string { return fmt.Sprintf("1.3.6.1.2.1.25.3.2.1.%d.1", c) }
	o.add(device(1), gosnmp.Integer, Const(1))
	o.add(device(2), gosnmp.ObjectIdentifier, Const(".1.3.6.1.2.1.25.3.1.5")) // hrDevicePrinter
	o.add(device(3), gosnmp.OctetString, Const("HP LaserJet Pro M404dn"))
	o.add(device(5), gosnmp.Integer, Const(2))                                 // hrDeviceStatus: running
	o.add("1.3.6.1.2.1.25.3.5.1.1.1", gosnmp.Integer, Const(3))                // hrPrinterStatus: idle
	o.add("1.3.6.1.2.1.25.3.5.1.2.1", gosnmp.OctetString, Const([]byte{0x00})) // no error detected

	const prt = "1.3.6.1.2.1.43."
	o.add(prt+"5.1.1.16.1", gosnmp.OctetString, Const("HP LaserJet Pro M404dn"))
	o.add(prt+"5.1.1.17.1", gosnmp.OctetString, Const(fmt.Sprintf("PHB%07d", int(unit(id.Seed+41)*1e7))))
	// The marker, and the pages it has printed: about fourteen an hour.
	o.add(prt+"10.2.1.2.1.1", gosnmp.Integer, Const(4)) // electrophotographicLaser
	o.add(prt+"10.2.1.3.1.1", gosnmp.Integer, Const(7)) // counted in impressions
	o.add(prt+"10.2.1.4.1.1", gosnmp.Counter32, Counter(0.004, uint64(18_000+40_000*unit(id.Seed+42)),
		Swing{Depth: 0.9, Period: 2 * time.Hour, Seed: id.Seed + 40000}))
	o.add(prt+"10.2.1.15.1.1", gosnmp.Integer, Const(0)) // prtMarkerStatus: available and idle
	// The black cartridge, at 17 to 18 percent.
	supply := func(c int) string { return fmt.Sprintf(prt+"11.1.1.%d.1.1", c) }
	o.add(supply(1), gosnmp.Integer, Const(1))
	o.add(supply(2), gosnmp.Integer, Const(1))
	o.add(supply(3), gosnmp.Integer, Const(1))
	o.add(supply(4), gosnmp.Integer, Const(3)) // supplyThatIsConsumed
	o.add(supply(5), gosnmp.Integer, Const(3)) // toner
	o.add(supply(6), gosnmp.OctetString, Const("Black Cartridge HP 59A"))
	o.add(supply(7), gosnmp.Integer, Const(19)) // in percent
	o.add(supply(8), gosnmp.Integer, Const(100))
	o.add(supply(9), gosnmp.Integer, IntegerGauge(17, 18, Swing{Period: 6 * time.Hour, Seed: id.Seed + 40001}))
	// The alert the cartridge raised, which printerV2Alert carries.
	alert := func(c int) string { return fmt.Sprintf(prt+"18.1.1.%d.1.1", c) }
	o.add(alert(1), gosnmp.Integer, Const(1))
	o.add(alert(2), gosnmp.Integer, Const(4))    // severity: warning
	o.add(alert(3), gosnmp.Integer, Const(4))    // training: trained
	o.add(alert(4), gosnmp.Integer, Const(11))   // group: markerSupplies
	o.add(alert(5), gosnmp.Integer, Const(1))    // the supply's index
	o.add(alert(6), gosnmp.Integer, Const(-2))   // location: unknown
	o.add(alert(7), gosnmp.Integer, Const(1104)) // markerTonerAlmostEmpty
	o.add(alert(8), gosnmp.OctetString, Const("Black cartridge low"))
	o.add(alert(9), gosnmp.TimeTicks, Const(uint32(0)))
	return o
}
