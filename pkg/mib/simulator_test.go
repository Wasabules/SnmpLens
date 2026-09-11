package mib

import (
	"strings"
	"testing"
	"time"

	"SnmpLens/pkg/simulator"
	"SnmpLens/pkg/simulator/simtest"

	"github.com/gosnmp/gosnmp"
)

// The conceptual tables the simulator serves from a bundled MIB, and what
// their index is made of.
var simulatedTables = []string{
	"1.3.6.1.2.1.1.9",    // sysORTable: an integer
	"1.3.6.1.2.1.2.2",    // ifTable
	"1.3.6.1.2.1.31.1.1", // ifXTable, which AUGMENTS it
	"1.3.6.1.2.1.31.1.2", // ifStackTable: two interfaces, zero allowed
	"1.3.6.1.2.1.4.20",   // ipAddrTable: an IpAddress
	"1.3.6.1.2.1.4.22",   // ipNetToMediaTable: an interface and an IpAddress
	"1.3.6.1.2.1.4.32",   // ipAddressPrefixTable: an InetAddress, its length first
	"1.3.6.1.2.1.4.34",   // ipAddressTable
	"1.3.6.1.2.1.6.13",   // tcpConnTable: four parts
	"1.3.6.1.2.1.7.5",    // udpTable
	"1.3.6.1.2.1.25.2.3", // hrStorageTable
	"1.3.6.1.2.1.25.3.2", // hrDeviceTable
	"1.3.6.1.2.1.25.3.3", // hrProcessorTable
	"1.3.6.1.2.1.25.3.4", // hrNetworkTable
	"1.3.6.1.2.1.25.3.5", // hrPrinterTable
	"1.3.6.1.2.1.25.3.6", // hrDiskStorageTable
	"1.3.6.1.2.1.25.3.8", // hrFSTable
	"1.3.6.1.2.1.25.4.2", // hrSWRunTable
	"1.3.6.1.2.1.25.5.1", // hrSWRunPerfTable, which AUGMENTS it
	"1.3.6.1.2.1.25.6.3", // hrSWInstalledTable
}

// Every table a simulated device serves from a bundled MIB decodes, row by row,
// with nothing left over, through SnmpLens's own decoder: the index each model
// writes is the one RFC 2578 7.7 reads back — an IpAddress as four
// sub-identifiers, an InetAddress with its length first, tcpConnTable's four
// parts. The rules come from the MIB, so a model that wrote an index its own
// way fails here rather than on screen, as a table of garbled rows.
func TestSimulatedTablesDecode(t *testing.T) {
	s := loadedService(t)
	for _, m := range simulator.Models() {
		t.Run(m.ID, func(t *testing.T) {
			d := simtest.Start(t, simulator.Device{Model: m.ID})
			g := &gosnmp.GoSNMP{Target: d.Address, Port: uint16(d.Port), Community: d.Community,
				Version: gosnmp.Version2c, Timeout: 2 * time.Second, Retries: 1}
			if err := g.Connect(); err != nil {
				t.Fatal(err)
			}
			defer g.Conn.Close()

			rows := 0
			for _, table := range simulatedTables {
				entry := "." + table + ".1."
				seen := map[string]bool{}
				var instances []string
				err := g.BulkWalk("."+table, func(v gosnmp.SnmpPDU) error {
					rest, ok := strings.CutPrefix(v.Name, entry)
					if !ok {
						return nil
					}
					// What follows the column is the row's instance.
					if _, inst, ok := strings.Cut(rest, "."); ok && !seen[inst] {
						seen[inst] = true
						instances = append(instances, inst)
					}
					return nil
				})
				if err != nil {
					t.Fatalf("%s: %v", table, err)
				}
				for _, dec := range s.DecodeIndexes(table, instances) {
					if dec.Error != "" || len(dec.Parts) == 0 {
						t.Errorf("%s, row %s: %q", table, dec.Raw, dec.Error)
					}
				}
				rows += len(instances)
			}
			if rows == 0 {
				t.Error("no table has a row")
			}
		})
	}
}

// tcpConnTable is the one whose index says the most, and a server's is read as
// what it is: sockets listening on every address, and a session established
// from a peer.
func TestASimulatedServersConnectionsDecode(t *testing.T) {
	s := loadedService(t)
	d := simtest.Start(t, simulator.Device{Model: "linux-server"})
	g := &gosnmp.GoSNMP{Target: d.Address, Port: uint16(d.Port), Community: d.Community,
		Version: gosnmp.Version2c, Timeout: 2 * time.Second, Retries: 1}
	if err := g.Connect(); err != nil {
		t.Fatal(err)
	}
	defer g.Conn.Close()
	var instances []string
	if err := g.BulkWalk(".1.3.6.1.2.1.6.13.1.1", func(v gosnmp.SnmpPDU) error {
		instances = append(instances, strings.TrimPrefix(v.Name, ".1.3.6.1.2.1.6.13.1.1."))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var listening, established int
	for _, dec := range s.DecodeIndexes("1.3.6.1.2.1.6.13", instances) {
		if len(dec.Parts) != 4 {
			t.Fatalf("row %s decodes to %d parts: %q", dec.Raw, len(dec.Parts), dec.Error)
		}
		switch {
		case dec.Parts[0].Display == "0.0.0.0" && dec.Parts[3].Display == "0":
			listening++
		case dec.Parts[3].Display != "0":
			established++
		}
	}
	if listening == 0 || established == 0 {
		t.Errorf("%d socket(s) listening and %d session(s) established, from %v", listening, established, instances)
	}
}
