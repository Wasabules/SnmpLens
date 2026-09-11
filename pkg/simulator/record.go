package simulator

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"

	"github.com/gosnmp/gosnmp"
)

// A Recording is what a device answered, written as a .snmprec walk while it is
// walked: the walk of the package a real device's model is made of
// (RecordedPackage). A recording is read back by the reader every walk goes
// through (readWalk), so the two must agree, and the tests hold them together.
type Recording struct {
	walk    bytes.Buffer
	objects int
	leftOut int
}

// Add records one varbind the device answered. What a package never serves is
// not written at all: the recorded device's snmp group, engine, USM users, VACM
// groups and community table stay on the device rather than in a file somebody
// may pass on. A varbind that holds no value — an exception, a NULL — is passed
// over, and so is a type the format has no tag for.
func (r *Recording) Add(pdu gosnmp.SnmpPDU) error {
	id, err := parseOID(pdu.Name)
	if err != nil {
		return nil
	}
	if slices.ContainsFunc(agentSubtrees, id.hasPrefix) {
		r.leftOut++
		return nil
	}
	line, ok := snmprecOf(id, pdu)
	if !ok {
		return nil
	}
	if r.objects == MaxRecordedObjects {
		return fmt.Errorf("the device answers more than %d objects, which is all a package keeps", MaxRecordedObjects)
	}
	r.walk.WriteString(line)
	r.walk.WriteByte('\n')
	r.objects++
	return nil
}

// Objects is how many objects have been recorded.
func (r *Recording) Objects() int { return r.objects }

// LeftOut is how many of the agent's own objects were not.
func (r *Recording) LeftOut() int { return r.leftOut }

// snmprecTags are the tags snmpsim writes each type as.
var snmprecTags = map[gosnmp.Asn1BER]int{
	gosnmp.Integer: 2, gosnmp.OctetString: 4, gosnmp.ObjectIdentifier: 6, gosnmp.IPAddress: 64,
	gosnmp.Counter32: 65, gosnmp.Gauge32: 66, gosnmp.TimeTicks: 67, gosnmp.Opaque: 68,
	gosnmp.Counter64: 70, gosnmp.Uinteger32: 66,
}

// snmprecOf is one varbind as a line of a .snmprec: an OCTET STRING as text
// when every octet is printable and in hex otherwise, so that the octets read
// back are the octets the device sent.
func snmprecOf(id oid, pdu gosnmp.SnmpPDU) (string, bool) {
	tag, ok := snmprecTags[pdu.Type]
	if !ok {
		return "", false
	}
	name := strings.TrimPrefix(id.String(), ".")
	switch pdu.Type {
	case gosnmp.OctetString, gosnmp.Opaque:
		b, ok := pdu.Value.([]byte)
		if !ok {
			return "", false // an Opaque gosnmp decoded as a float
		}
		if pdu.Type == gosnmp.OctetString && printable(b) {
			return fmt.Sprintf("%s|%d|%s", name, tag, b), true
		}
		return fmt.Sprintf("%s|%dx|%s", name, tag, hex.EncodeToString(b)), true
	case gosnmp.ObjectIdentifier:
		s, _ := pdu.Value.(string)
		o, err := parseOID(s)
		if err != nil {
			return "", false
		}
		return fmt.Sprintf("%s|%d|%s", name, tag, strings.TrimPrefix(o.String(), ".")), true
	case gosnmp.IPAddress:
		s, _ := pdu.Value.(string)
		a, err := netip.ParseAddr(s)
		if err != nil || !a.Is4() {
			return "", false
		}
		return fmt.Sprintf("%s|%d|%s", name, tag, a), true
	}
	return fmt.Sprintf("%s|%d|%s", name, tag, gosnmp.ToBigInt(pdu.Value).String()), true
}

// printable reports whether every octet is printable ASCII, which is what a
// .snmprec may carry as text.
func printable(b []byte) bool {
	for _, c := range b {
		if c < 0x20 || c > 0x7e {
			return false
		}
	}
	return true
}

// RecordedModel is what a model made from a recording is called.
type RecordedModel struct {
	Name, Description, Vendor, Category string
}

// RecordedPackage is the package a recording makes: model.json, its id made
// from its name and its system group left to the walk, which recorded the
// device's own; and the walk. It is checked as any package is, so what it
// returns is what an import would accept.
func RecordedPackage(meta RecordedModel, r *Recording) ([]PackageFile, CustomModel, error) {
	if r.objects == 0 {
		return nil, CustomModel{}, errors.New("the device answered nothing to walk")
	}
	model, err := json.MarshalIndent(struct {
		Kind          string `json:"kind"`
		FormatVersion int    `json:"formatVersion"`
		ID            string `json:"id"`
		Name          string `json:"name"`
		Description   string `json:"description,omitempty"`
		Vendor        string `json:"vendor,omitempty"`
		Category      string `json:"category,omitempty"`
	}{CustomModelKind, CustomModelFormat, ModelSlug(meta.Name), strings.TrimSpace(meta.Name),
		strings.TrimSpace(meta.Description), strings.TrimSpace(meta.Vendor), meta.Category}, "", "  ")
	if err != nil {
		return nil, CustomModel{}, err
	}
	files := []PackageFile{
		{Name: PackageModelFile, Data: append(model, '\n')},
		{Name: "walks/device.snmprec", Data: slices.Clone(r.walk.Bytes())},
	}
	m, _, err := ParseCustomPackage(files)
	if err != nil {
		return nil, CustomModel{}, err
	}
	return files, m, nil
}

// latinFolds are the accented letters a name is likely to hold, and what a slug
// writes each as.
var latinFolds = map[rune]string{
	'à': "a", 'á': "a", 'â': "a", 'ä': "a", 'ã': "a", 'å': "a", 'ç': "c",
	'è': "e", 'é': "e", 'ê': "e", 'ë': "e", 'ì': "i", 'í': "i", 'î': "i", 'ï': "i", 'ñ': "n",
	'ò': "o", 'ó': "o", 'ô': "o", 'ö': "o", 'õ': "o", 'ø': "o",
	'ù': "u", 'ú': "u", 'û': "u", 'ü': "u", 'ý': "y", 'ÿ': "y", 'æ': "ae", 'œ': "oe", 'ß': "ss",
}

// ModelSlug makes a custom model's id from a name: lower-case letters and
// digits, one hyphen for whatever runs between them, the accents of a Latin
// name taken off rather than made into hyphens.
func ModelSlug(name string) string {
	var b strings.Builder
	gap := false
	for _, r := range strings.ToLower(name) {
		fold, ok := latinFolds[r]
		switch {
		case ok:
			b.WriteString(fold)
			gap = false
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			gap = false
		case b.Len() > 0 && !gap:
			b.WriteByte('-')
			gap = true
		}
	}
	slug := strings.TrimRight(b.String(), "-")
	if len(slug) > 48 {
		slug = strings.TrimRight(slug[:48], "-")
	}
	if slug == "" {
		return "recorded-device"
	}
	return slug
}
