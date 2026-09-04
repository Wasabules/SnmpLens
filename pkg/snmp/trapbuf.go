package snmp

import (
	"fmt"
	"log"
	"net"
	"reflect"
	"unsafe"

	"github.com/gosnmp/gosnmp"
)

// TrapReadBuffer is the kernel receive buffer asked for on the trap socket.
//
// This is the single largest thing deciding whether a trap storm is recorded at
// all, and it dwarfs anything the handler's speed can buy. Measured on the real
// listener with a burst of 2000 datagrams and the handler left exactly as it is:
//
//	default buffer ....  417 journalled — 79% lost
//	8 MiB ............. 2000 journalled — none lost
//
// The loss is silent by nature: the kernel drops the datagram before Go ever
// sees it, so there is no error to report, no journal entry, and nothing to
// count. A burst shorter than the buffer's drain time simply costs nothing.
//
// 8 MiB holds roughly 30 000 typical trap datagrams. The operating system is
// free to give less than asked; it is never an error.
const TrapReadBuffer = 8 << 20

// raiseTrapReadBuffer enlarges the listener's socket buffer once it is bound.
//
// gosnmp keeps its *net.UDPConn in an unexported field and offers no accessor —
// WithBufferSize sets the per-read message size, which is a different thing and
// does not help here. So this reaches into the struct, and that is a real
// dependency on another package's internals.
//
// Two things make it safe to do anyway. It is FAIL-SOFT: anything unexpected
// leaves the default buffer and today's behaviour, never an error the operator
// has to act on. And the assumption is pinned by a test that fails loudly if the
// field is renamed or retyped, so a gosnmp upgrade that breaks it is caught by
// CI rather than by a trap storm that was quietly halved.
func raiseTrapReadBuffer(tl *gosnmp.TrapListener, size int) {
	conn, err := trapSocket(tl)
	if err != nil {
		log.Printf("trap listener: could not reach the socket to enlarge its read buffer (%v); "+
			"continuing with the system default", err)
		return
	}
	if err := conn.SetReadBuffer(size); err != nil {
		log.Printf("trap listener: the system refused a %d-byte read buffer (%v); "+
			"continuing with the default", size, err)
	}
}

// trapSocket returns the listener's bound UDP socket.
//
// Separate from the caller so a test can assert the contract with gosnmp holds,
// which is the whole reason the reflection above is acceptable.
func trapSocket(tl *gosnmp.TrapListener) (*net.UDPConn, error) {
	if tl == nil {
		return nil, fmt.Errorf("no listener")
	}

	field := reflect.ValueOf(tl).Elem().FieldByName("conn")
	if !field.IsValid() {
		return nil, fmt.Errorf("gosnmp.TrapListener no longer has a `conn` field")
	}
	if field.Type() != reflect.TypeOf((*net.UDPConn)(nil)) {
		return nil, fmt.Errorf("gosnmp.TrapListener.conn is now %s, not *net.UDPConn", field.Type())
	}
	if !field.CanAddr() {
		return nil, fmt.Errorf("gosnmp.TrapListener.conn is not addressable")
	}

	conn := *(**net.UDPConn)(unsafe.Pointer(field.UnsafeAddr())) // #nosec G103 -- read-only, guarded above and pinned by a test
	if conn == nil {
		return nil, fmt.Errorf("the listener is not bound yet")
	}
	return conn, nil
}
