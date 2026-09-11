package simulator

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// MaxDeviceName bounds a device's name, which is also its sysName.
const MaxDeviceName = 64

// Device is one simulated device as configured: what it is (a model and a
// name), where it answers, and who may ask it.
//
// It holds its secrets — the community and the users' passphrases — because
// starting it needs them. Whatever writes a device somewhere writes
// WithoutSecrets() and keeps Secrets() in the credential store.
type Device struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Model     string   `json:"model"`
	Address   string   `json:"address"`
	Port      int      `json:"port"`
	Versions  []string `json:"versions"`
	Community string   `json:"community"`
	Users     []User   `json:"users"`
	// EngineID is hex, given once when the device is created (NewEngineID)
	// and never changed: managers cache it and localise their keys to it.
	EngineID string `json:"engineId"`
	// EngineBoots counts the device's starts. Whoever starts it raises the
	// count and keeps it (Fleet.Start).
	EngineBoots uint32 `json:"engineBoots"`
}

// Listen is the address the device answers on, in the form CheckListen reads.
func (d Device) Listen() string {
	return net.JoinHostPort(strings.TrimSpace(d.Address), strconv.Itoa(d.Port))
}

// Validate reports the first thing wrong with d, of everything that can be
// checked without deriving a key. It carries the loopback rule, so a device read
// back from a file is held to it exactly as one typed in is.
func (d Device) Validate() error {
	name := strings.TrimSpace(d.Name)
	if name == "" || utf8.RuneCountInString(name) > MaxDeviceName {
		return fmt.Errorf("a device's name is 1 to %d characters", MaxDeviceName)
	}
	if _, ok := findModel(d.Model); !ok {
		return fmt.Errorf("%q is not a device model", d.Model)
	}
	if d.Port < 1 || d.Port > 65535 {
		return fmt.Errorf("%d is not a port", d.Port)
	}
	if _, err := CheckListen(d.Listen()); err != nil {
		return err
	}
	var v1, v2c, v3 bool
	for _, v := range d.Versions {
		switch v {
		case "v1":
			v1 = true
		case "v2c":
			v2c = true
		case "v3":
			v3 = true
		default:
			return fmt.Errorf("%q is not an SNMP version (v1, v2c or v3)", v)
		}
	}
	if !v1 && !v2c && !v3 {
		return errors.New("a device answers at least one SNMP version")
	}
	if (v1 || v2c) && (d.Community == "" || len(d.Community) > maxCommunity) {
		return fmt.Errorf("v1 and v2c need a community of 1 to %d octets", maxCommunity)
	}
	if v3 {
		if len(d.Users) == 0 {
			return errors.New("v3 needs a user")
		}
		seen := make(map[string]bool, len(d.Users))
		for _, u := range d.Users {
			if _, err := checkUser(u); err != nil {
				return err
			}
			if seen[u.Name] {
				return fmt.Errorf("user %q is declared twice", u.Name)
			}
			seen[u.Name] = true
		}
	}
	if id, err := hex.DecodeString(d.EngineID); err != nil || len(id) < 5 || len(id) > 32 {
		return errors.New("a device's engine ID is 5 to 32 octets, in hex")
	}
	if d.EngineBoots >= maxBoots {
		return errors.New("the device's boots have reached their ceiling; it needs a new engine ID")
	}
	return nil
}

// DeviceSecrets is what a Device holds that no file may: the community, and
// each user's passphrases by user name.
type DeviceSecrets struct {
	Community string                 `json:"community"`
	Users     map[string]UserSecrets `json:"users"`
}

// UserSecrets are one user's passphrases.
type UserSecrets struct {
	AuthPass string `json:"authPass"`
	PrivPass string `json:"privPass"`
}

// Secrets is d's secrets and nothing else.
func (d Device) Secrets() DeviceSecrets {
	s := DeviceSecrets{Community: d.Community, Users: make(map[string]UserSecrets, len(d.Users))}
	for _, u := range d.Users {
		s.Users[u.Name] = UserSecrets{AuthPass: u.AuthPass, PrivPass: u.PrivPass}
	}
	return s
}

// WithoutSecrets is d with its secrets blanked, sharing nothing with d.
func (d Device) WithoutSecrets() Device {
	d.Community = ""
	d.Versions = slices.Clone(d.Versions)
	d.Users = slices.Clone(d.Users)
	for i := range d.Users {
		d.Users[i].AuthPass, d.Users[i].PrivPass = "", ""
	}
	return d
}

// WithSecrets is d with s filled back in.
func (d Device) WithSecrets(s DeviceSecrets) Device {
	d.Community = s.Community
	d.Versions = slices.Clone(d.Versions)
	d.Users = slices.Clone(d.Users)
	for i := range d.Users {
		p := s.Users[d.Users[i].Name]
		d.Users[i].AuthPass, d.Users[i].PrivPass = p.AuthPass, p.PrivPass
	}
	return d
}

// NewDeviceID is a new device's ID. It is random, and says nothing about the
// device.
func NewDeviceID() string {
	b := make([]byte, 8)
	rand.Read(b) // never fails since Go 1.24
	return hex.EncodeToString(b)
}

// NewEngineID is the engine ID a new device of a model is created with: the MAC
// format of EngineID, carrying the model's vendor and a MAC drawn from the
// device's ID — the same for the device's whole life, and different from any
// other device's.
func NewEngineID(modelID, deviceID string) (string, error) {
	m, ok := findModel(modelID)
	if !ok {
		return "", fmt.Errorf("%q is not a device model", modelID)
	}
	id, err := EngineID(m.enterprise, deviceMAC(deviceSeed(deviceID), 0))
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(id), nil
}

// deviceSeed is where a device's values take their motion from.
func deviceSeed(deviceID string) uint64 {
	h := sha256.Sum256([]byte(deviceID))
	return binary.BigEndian.Uint64(h[:8])
}

// config is the agent d runs as.
func (d Device) config() (Config, error) {
	m, ok := findModel(d.Model)
	if !ok {
		return Config{}, fmt.Errorf("%q is not a device model", d.Model)
	}
	engineID, err := hex.DecodeString(d.EngineID)
	if err != nil {
		return Config{}, fmt.Errorf("engine ID: %w", err)
	}
	return Config{
		Listen:      d.Listen(),
		Versions:    d.Versions,
		Community:   d.Community,
		Users:       d.Users,
		EngineID:    engineID,
		EngineBoots: d.EngineBoots,
		Objects:     m.build(Identity{Name: strings.TrimSpace(d.Name), Seed: deviceSeed(d.ID)}),
	}, nil
}
