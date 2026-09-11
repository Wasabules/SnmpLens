package simulator

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

// ErrNotLoopback is returned for a listen address that is not a loopback one.
var ErrNotLoopback = errors.New("a simulated agent listens on a loopback address only (127.0.0.0/8 or ::1)")

// CheckListen parses the address a simulated agent is to answer on, and refuses
// any that is not a loopback address.
//
// The first version of the simulator is reachable from this machine and from
// no other. That is decided here, in Go, and not by a default in a form: a
// device file is JSON that someone else may have written, and an import able to
// set "0.0.0.0:161" would put a device with a known community on the network of
// whoever opened it. The socket is checked again once bound (Agent.Start), so a
// path that skips this function does not skip the rule.
//
// A host name is refused rather than resolved, "localhost" excepted, which is
// taken to mean 127.0.0.1 without asking the resolver: a check that resolves is
// a check the hosts file decides. Port 0 asks for any free port.
func CheckListen(addr string) (netip.AddrPort, error) {
	host, port, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return netip.AddrPort{}, fmt.Errorf("listen address %q: %w", addr, err)
	}
	p, err := strconv.ParseUint(port, 10, 16)
	if err != nil {
		return netip.AddrPort{}, fmt.Errorf("listen address %q: %q is not a port", addr, port)
	}
	if strings.EqualFold(host, "localhost") {
		host = "127.0.0.1"
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return netip.AddrPort{}, fmt.Errorf("listen address %q: %q is not an IP address, and %w", addr, host, ErrNotLoopback)
	}
	if ip.Zone() != "" {
		return netip.AddrPort{}, fmt.Errorf("listen address %q: a loopback address takes no zone", addr)
	}
	// An IPv4-mapped address is the address it maps: ::ffff:127.0.0.1 is
	// loopback and ::ffff:10.0.0.1 is not.
	ip = ip.Unmap()
	if !ip.IsLoopback() {
		return netip.AddrPort{}, fmt.Errorf("listen address %q: %w", addr, ErrNotLoopback)
	}
	return netip.AddrPortFrom(ip, uint16(p)), nil
}

// checkBound applies the rule to the address a socket actually holds.
//
// Not a formality: an Agent that did not come from NewAgent has a zero
// address, and a zero address binds EVERY interface.
func checkBound(addr net.Addr) error {
	if u, ok := addr.(*net.UDPAddr); ok && u.IP.IsLoopback() {
		return nil
	}
	return fmt.Errorf("bound %v: %w", addr, ErrNotLoopback)
}
