package simulator

import (
	"errors"
	"math"

	"github.com/gosnmp/gosnmp"
)

// The BER this file reads is the little an agent needs to learn what a message
// CLAIMS before anything in it is trusted (X.690, as RFC 3417 restricts it).
// Encoding, and every decode that matters, stay gosnmp's.

const (
	tagInteger  = 0x02
	tagOctets   = 0x04
	tagSequence = 0x30
)

var (
	errMalformed = errors.New("not a well-formed SNMP message")
	// errBadVersion is a well-formed message in a version SNMP no longer has,
	// such as v2u — counted apart from garbage (snmpInBadVersions).
	errBadVersion = errors.New("not an SNMP version")
)

// element is one TLV.
type element struct {
	tag   byte
	value []byte
	next  int // offset of what follows it
}

// readElement reads the TLV at off. Every length is held to what is actually
// there: the bytes came from the network.
func readElement(b []byte, off int) (element, error) {
	if off < 0 || off+2 > len(b) {
		return element{}, errMalformed
	}
	tag := b[off]
	if tag&0x1f == 0x1f {
		// A multi-byte tag, which SNMP never uses.
		return element{}, errMalformed
	}
	n := int(b[off+1])
	off += 2
	if n&0x80 != 0 {
		size := n & 0x7f
		// Zero is the indefinite form, which RFC 3417 8 rules out; more than
		// four octets of length describe something no datagram holds.
		if size == 0 || size > 4 || off+size > len(b) {
			return element{}, errMalformed
		}
		n = 0
		for _, c := range b[off : off+size] {
			n = n<<8 | int(c)
		}
		off += size
	}
	if n < 0 || n > len(b)-off {
		return element{}, errMalformed
	}
	return element{tag: tag, value: b[off : off+n], next: off + n}, nil
}

// reader walks the elements of one constructed value in order and keeps the
// first error, so a parse reads as the grammar it follows.
type reader struct {
	b   []byte
	off int
	err error
}

func (r *reader) element() element {
	if r.err != nil {
		return element{}
	}
	e, err := readElement(r.b, r.off)
	if err != nil {
		r.err = err
		return element{}
	}
	r.off = e.next
	return e
}

// sequence enters the next element, which must be a SEQUENCE.
func (r *reader) sequence() *reader {
	e := r.element()
	if r.err == nil && e.tag != tagSequence {
		r.err = errMalformed
	}
	return &reader{b: e.value, err: r.err}
}

func (r *reader) integer() int64 {
	e := r.element()
	if r.err != nil {
		return 0
	}
	if e.tag != tagInteger || len(e.value) == 0 || len(e.value) > 8 {
		r.err = errMalformed
		return 0
	}
	v := int64(int8(e.value[0]))
	for _, c := range e.value[1:] {
		v = v<<8 | int64(c)
	}
	return v
}

func (r *reader) octets() []byte {
	e := r.element()
	if r.err == nil && e.tag != tagOctets {
		r.err = errMalformed
	}
	return e.value
}

// peekVersion reads the version a message says it is, which decides who reads
// the rest of it.
func peekVersion(msg []byte) (gosnmp.SnmpVersion, error) {
	body := (&reader{b: msg}).sequence()
	v := body.integer()
	if body.err != nil {
		return 0, body.err
	}
	switch v {
	case 0:
		return gosnmp.Version1, nil
	case 1:
		return gosnmp.Version2c, nil
	case 3:
		return gosnmp.Version3, nil
	}
	return 0, errBadVersion
}

// v3Header is what an SNMPv3 message claims about itself — the SNMPv3Message of
// RFC 3412 and the UsmSecurityParameters of RFC 3414 — read before any of it is
// trusted, since deciding whether to trust it is what these fields are for.
type v3Header struct {
	msgID    uint32
	flags    gosnmp.SnmpV3MsgFlags
	model    int64
	engineID []byte
	boots    int64
	time     int64
	user     string
	// requestID is the request-id of a scoped PDU that is not encrypted: what a
	// Report echoes so the manager can match it to its request.
	requestID uint32
}

// level is the security level the message claims.
func (h *v3Header) level() gosnmp.SnmpV3MsgFlags { return h.flags & gosnmp.AuthPriv }

func peekV3(msg []byte) (*v3Header, error) {
	body := (&reader{b: msg}).sequence() // SNMPv3Message
	version := body.integer()
	global := body.sequence() // msgGlobalData
	msgID := global.integer()
	maxSize := global.integer()
	flags := global.octets()
	model := global.integer()
	// msgSecurityParameters is an OCTET STRING wrapping the USM's own SEQUENCE.
	params := body.octets()
	usm := (&reader{b: params, err: body.err}).sequence()
	h := &v3Header{
		model:    model,
		engineID: usm.octets(),
		boots:    usm.integer(),
		time:     usm.integer(),
		user:     string(usm.octets()),
	}
	usm.octets() // msgAuthenticationParameters
	usm.octets() // msgPrivacyParameters
	data := body.element()
	for _, r := range []*reader{body, global, usm} {
		if r.err != nil {
			return nil, r.err
		}
	}
	// The ranges RFC 3412 6 gives these fields.
	if version != 3 || msgID < 0 || msgID > math.MaxInt32 ||
		maxSize < 484 || maxSize > math.MaxInt32 || len(flags) != 1 {
		return nil, errMalformed
	}
	h.msgID = uint32(msgID)
	h.flags = gosnmp.SnmpV3MsgFlags(flags[0])

	// A plaintext scoped PDU gives up its request-id; an encrypted one is an
	// OCTET STRING and keeps it.
	if data.tag == tagSequence {
		scoped := &reader{b: data.value}
		scoped.octets() // contextEngineID
		scoped.octets() // contextName
		// A PDU's tag is context-specific and constructed: 0xa0 to 0xa8.
		if pdu := scoped.element(); scoped.err == nil && pdu.tag&0xe0 == 0xa0 {
			id := (&reader{b: pdu.value}).integer()
			if id >= 0 && id <= math.MaxInt32 {
				h.requestID = uint32(id)
			}
		}
	}
	return h, nil
}
