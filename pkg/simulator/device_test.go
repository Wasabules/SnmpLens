package simulator

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func validDevice(t *testing.T) Device {
	t.Helper()
	id := "a1b2c3d4e5f60718"
	engineID, err := NewEngineID("linux-server", id)
	if err != nil {
		t.Fatal(err)
	}
	return Device{
		ID:        id,
		Name:      "srv-01",
		Model:     "linux-server",
		Address:   "127.0.0.2",
		Port:      16161,
		Versions:  []string{"v2c", "v3"},
		Community: "public",
		Users: []User{{Name: "ops", SecLevel: "AuthPriv",
			AuthProto: "SHA256", AuthPass: "authpass-1", PrivProto: "AES", PrivPass: "privpass-1"}},
		EngineID: engineID,
	}
}

func TestADeviceIsHeldToTheRules(t *testing.T) {
	if err := validDevice(t).Validate(); err != nil {
		t.Fatalf("a valid device: %v", err)
	}
	for missing, change := range map[string]func(*Device){
		"a name":              func(d *Device) { d.Name = "  " },
		"a known model":       func(d *Device) { d.Model = "toaster" },
		"a port":              func(d *Device) { d.Port = 0 },
		"a version":           func(d *Device) { d.Versions = nil },
		"a known version":     func(d *Device) { d.Versions = []string{"v2"} },
		"a community":         func(d *Device) { d.Community = "" },
		"a user":              func(d *Device) { d.Users = nil },
		"a passphrase":        func(d *Device) { d.Users[0].PrivPass = "short" },
		"a distinct user":     func(d *Device) { d.Users = append(d.Users, d.Users[0]) },
		"an engine ID":        func(d *Device) { d.EngineID = "zz" },
		"a short engine ID":   func(d *Device) { d.EngineID = "8000" },
		"a loopback address":  func(d *Device) { d.Address = "192.168.1.10" },
		"a specific address":  func(d *Device) { d.Address = "0.0.0.0" },
		"an address at all":   func(d *Device) { d.Address = "" },
		"an address, no name": func(d *Device) { d.Address = "localhost.example" },
	} {
		d := validDevice(t)
		change(&d)
		if err := d.Validate(); err == nil {
			t.Errorf("a device without %s was accepted", missing)
		}
	}
	d := validDevice(t)
	d.Address = "10.0.0.5"
	if err := d.Validate(); !errors.Is(err, ErrNotLoopback) {
		t.Errorf("a network address: %v, want ErrNotLoopback", err)
	}
}

func TestTheSecretsComeApartAndGoBack(t *testing.T) {
	d := validDevice(t)
	bare := d.WithoutSecrets()
	raw, err := json.Marshal(bare)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"public", "authpass-1", "privpass-1"} {
		if strings.Contains(string(raw), secret) {
			t.Errorf("%q survived WithoutSecrets: %s", secret, raw)
		}
	}
	if d.Users[0].AuthPass != "authpass-1" {
		t.Error("WithoutSecrets blanked the original's passphrase too: the two share a slice")
	}
	if back := bare.WithSecrets(d.Secrets()); !reflect.DeepEqual(back, d) {
		t.Errorf("the round trip changed the device:\n%+v\n%+v", back, d)
	}
}

// A device keeps one engine ID for life, no two devices share one, and it
// carries its model's vendor in RFC 3411's MAC format.
func TestAnEngineIDBelongsToOneDevice(t *testing.T) {
	one, _ := NewEngineID("linux-server", "one")
	again, _ := NewEngineID("linux-server", "one")
	two, _ := NewEngineID("linux-server", "two")
	if one != again || one == two {
		t.Errorf("one=%s again=%s two=%s", one, again, two)
	}
	// net-snmp is enterprise 8072 = 0x1F88, high bit set, then format 3.
	if !strings.HasPrefix(one, "80001f8803") {
		t.Errorf("%s does not carry net-snmp's enterprise in the MAC format", one)
	}
	if _, err := NewEngineID("toaster", "one"); err == nil {
		t.Error("an engine ID was made for a model that does not exist")
	}
}
