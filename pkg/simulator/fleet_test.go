package simulator

import (
	"errors"
	"net"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"
)

// startOnFreePort starts d on 127.0.0.1 at a port nothing holds. A port found
// free can be taken before the device binds it — `go test ./...` runs every
// package at once — so a failed bind tries another rather than failing the test.
func startOnFreePort(t *testing.T, f *Fleet, d Device) Device {
	t.Helper()
	d.Address = "127.0.0.1"
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		c, lerr := net.ListenPacket("udp", "127.0.0.1:0")
		if lerr != nil {
			t.Skipf("no UDP socket: %v", lerr)
		}
		d.Port = c.LocalAddr().(*net.UDPAddr).Port
		c.Close()
		if err = f.Start(d); err == nil {
			return d
		}
	}
	t.Fatalf("could not start the device: %v", err)
	return d
}

func deviceManager(t *testing.T, d Device) *gosnmp.GoSNMP {
	t.Helper()
	u := d.Users[0]
	g := &gosnmp.GoSNMP{
		Target:        d.Address,
		Port:          uint16(d.Port),
		Version:       gosnmp.Version3,
		Timeout:       2 * time.Second,
		SecurityModel: gosnmp.UserSecurityModel,
		MsgFlags:      gosnmp.AuthPriv,
		SecurityParameters: &gosnmp.UsmSecurityParameters{UserName: u.Name,
			AuthenticationProtocol: gosnmp.SHA256, AuthenticationPassphrase: u.AuthPass,
			PrivacyProtocol: gosnmp.AES, PrivacyPassphrase: u.PrivPass},
	}
	return connect(t, g)
}

// A device runs as its model, named as configured, and reports the boot count
// it was started with — the count the caller raised and kept.
func TestAFleetRunsADeviceAsItsModel(t *testing.T) {
	f := NewFleet()
	t.Cleanup(f.StopAll)
	d := validDevice(t)
	d.EngineBoots = 3
	d = startOnFreePort(t, f, d)

	res, err := deviceManager(t, d).Get([]string{".1.3.6.1.2.1.1.5.0", ".1.3.6.1.6.3.10.2.1.2.0"})
	if err != nil {
		t.Fatal(err)
	}
	if got := text(res.Variables[0]); got != "srv-01" {
		t.Errorf("sysName = %q", got)
	}
	if got := res.Variables[1].Value; got != 3 {
		t.Errorf("snmpEngineBoots = %v, want 3", got)
	}

	if err := f.Start(d); !errors.Is(err, ErrRunning) {
		t.Errorf("starting it twice: %v, want ErrRunning", err)
	}
	if _, running := f.Status(d.ID); !running {
		t.Error("a running device reported as stopped")
	}
	if !f.Stop(d.ID) {
		t.Error("stopping a running device reported nothing stopped")
	}
	if _, running := f.Status(d.ID); running {
		t.Error("a stopped device reported as running")
	}
}

// Two devices cannot answer on one address: the second is refused, and the
// first keeps running.
func TestTwoDevicesCannotShareAnAddress(t *testing.T) {
	f := NewFleet()
	t.Cleanup(f.StopAll)
	first := startOnFreePort(t, f, validDevice(t))

	second := first
	second.ID = "second-device"
	if err := f.Start(second); err == nil {
		t.Fatal("a second device started on an address already answered")
	}
	if _, running := f.Status(first.ID); !running {
		t.Error("the first device stopped when the second was refused")
	}
}

// The fleet holds the loopback rule too: a device that got into a file some
// other way does not start on the network.
func TestAFleetDoesNotStartADeviceOnTheNetwork(t *testing.T) {
	d := validDevice(t)
	d.Address = "0.0.0.0"
	if err := NewFleet().Start(d); !errors.Is(err, ErrNotLoopback) {
		t.Errorf("err = %v, want ErrNotLoopback", err)
	}
}
