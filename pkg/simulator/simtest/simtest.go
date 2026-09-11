// Package simtest runs simulated devices for the length of a test, so that a
// test in any package talks to a real SNMP agent — the one the application
// ships — instead of skipping for want of one.
//
// It is for tests only. pkg/simulator's own tests cannot use it: it imports
// pkg/simulator, and the cycle would not build.
package simtest

import (
	"net"
	"strconv"
	"testing"

	"SnmpLens/pkg/simulator"
)

// Device is a simulated device that runs until its test ends.
type Device struct {
	simulator.Device
	fleet *simulator.Fleet
	t     testing.TB
}

// Start runs d, filling in what a test does not care about: an ID and a name,
// the Linux server model, v2c with the community "public", 127.0.0.1 at a free
// port, and the model's engine ID. A port that is found free can be taken
// before the device binds it — `go test ./...` runs every package at once — so
// a failed bind tries another, unless d names its own port.
func Start(t testing.TB, d simulator.Device) *Device {
	t.Helper()
	if d.Model == "" {
		d.Model = "linux-server"
	}
	if d.ID == "" {
		d.ID = simulator.NewDeviceID()
	}
	if d.Name == "" {
		d.Name = "sim-" + d.ID[:6]
	}
	if len(d.Versions) == 0 {
		d.Versions = []string{"v2c"}
	}
	if d.Community == "" {
		d.Community = "public"
	}
	if d.Address == "" {
		d.Address = "127.0.0.1"
	}
	if d.EngineID == "" {
		id, err := simulator.NewEngineID(d.Model, d.ID)
		if err != nil {
			t.Fatalf("simtest: %v", err)
		}
		d.EngineID = id
	}
	if d.EngineBoots == 0 {
		d.EngineBoots = 1
	}
	s := &Device{Device: d, fleet: simulator.NewFleet(), t: t}
	t.Cleanup(s.fleet.StopAll)

	fixed := d.Port != 0
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		if !fixed {
			s.Port = FreePort(t, d.Address)
		}
		if err = s.fleet.Start(s.Device); err == nil || fixed {
			break
		}
	}
	if err != nil {
		t.Fatalf("simtest: the %s device would not start on %s: %v", d.Model, s.Target(), err)
	}
	return s
}

// Target is the device as a target is written in SnmpLens: "127.0.0.1:1161".
func (d *Device) Target() string {
	return net.JoinHostPort(d.Address, strconv.Itoa(d.Port))
}

// Stop stops the device. It answers nothing until Restart.
func (d *Device) Stop() { d.fleet.Stop(d.ID) }

// Restart starts the device again on the same address and port, one boot
// later, as a device coming back from a reboot does: its uptime starts over,
// and a manager holding its SNMPv3 clock has to resynchronise.
func (d *Device) Restart() {
	d.t.Helper()
	d.fleet.Stop(d.ID)
	d.EngineBoots++
	if err := d.fleet.Start(d.Device); err != nil {
		d.t.Fatalf("simtest: the device would not start again: %v", err)
	}
}

// Notify has the device send the notification named name now.
func (d *Device) Notify(name string) ([]simulator.Delivery, error) {
	return d.fleet.Notify(d.ID, name)
}

// Stats is what the device has counted since it last started.
func (d *Device) Stats() simulator.Stats {
	s, _ := d.fleet.Status(d.ID)
	return s
}

// FreePort is a UDP port nothing holds on address, at the moment of asking.
func FreePort(t testing.TB, address string) int {
	t.Helper()
	c, err := net.ListenPacket("udp", net.JoinHostPort(address, "0"))
	if err != nil {
		t.Skipf("simtest: no UDP socket on %s: %v", address, err)
	}
	defer c.Close()
	return c.LocalAddr().(*net.UDPAddr).Port
}

// OwnAddress is 127.0.0.n when this machine answers on it — Windows and Linux
// answer on the whole of 127.0.0.0/8, macOS on 127.0.0.1 alone unless aliases
// were added — and skips the test otherwise. A device on an address of its own
// is how a test tells its traffic from anything else on 127.0.0.1.
func OwnAddress(t testing.TB, n int) string {
	t.Helper()
	address := "127.0.0." + strconv.Itoa(n)
	c, err := net.ListenPacket("udp", net.JoinHostPort(address, "0"))
	if err != nil {
		t.Skipf("simtest: this machine does not answer on %s", address)
	}
	c.Close()
	return address
}
