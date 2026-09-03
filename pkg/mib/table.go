package mib

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/sleepinggenius2/gosmi"
	"github.com/sleepinggenius2/gosmi/models"
	"github.com/sleepinggenius2/gosmi/types"
)

// Conceptual tables.
//
// A walk returns a flat list of OIDs. Turning that into a table means matching
// each OID to its column and splitting what is left into the INDEX values the
// MIB declares. The second half was missing: the instance was kept as the raw
// sub-OID string, so tcpConnTable — whose INDEX is four objects — showed one
// opaque "192.168.1.1.161.10.0.0.5.5000" per row, a table keyed by a name
// showed decimal bytes, and sorting by index sorted 10 before 9.
//
// The rules are RFC 2578 section 7.7, and none of them are guessable from the
// OID: how many sub-identifiers an index object consumes depends on its
// SYNTAX, on whether its size is fixed, and on whether the row says IMPLIED.
// That lives in the MIB and nowhere else.

// TableInfo describes a conceptual table and everything needed to read, sort
// and edit it.
type TableInfo struct {
	Oid     string        `json:"oid"`
	Name    string        `json:"name"`
	RowOid  string        `json:"rowOid"`
	RowName string        `json:"rowName"`
	Columns []TableColumn `json:"columns"`
	Index   []IndexPart   `json:"index"`
	Implied bool          `json:"implied"`

	// RowStatusOid is the column whose SYNTAX is RowStatus, when the table has
	// one. Its presence is what makes a table creatable: RFC 2579 gives no
	// other way to bring a conceptual row into existence.
	RowStatusOid string `json:"rowStatusOid,omitempty"`
	// Augments names the row this one extends, when it does. Such a row is
	// indexed by the other's INDEX — gosmi resolves that for us.
	Augments string `json:"augments,omitempty"`
}

// TableColumn is one column of a conceptual table.
type TableColumn struct {
	Name       string           `json:"name"`
	Oid        string           `json:"oid"`
	Syntax     string           `json:"syntax"`
	Access     string           `json:"access"`
	IsIndex    bool             `json:"isIndex"`
	Writable   bool             `json:"writable"`
	EnumValues map[string]int64 `json:"enumValues,omitempty"`
}

// IndexPart is one object of a row's INDEX clause.
type IndexPart struct {
	Name    string `json:"name"`
	Oid     string `json:"oid"`
	Syntax  string `json:"syntax"`
	Implied bool   `json:"implied"`
}

// DecodedIndex is one row's instance sub-OID, split into its declared parts.
type DecodedIndex struct {
	Raw   string       `json:"raw"`
	Parts []IndexValue `json:"parts"`
	Error string       `json:"error,omitempty"`
}

// IndexValue is one INDEX object's value for one row.
type IndexValue struct {
	Name string `json:"name"`
	// Display is what a person should read: an address as an address, a
	// DisplayString as text, an enum by its label.
	Display string `json:"display"`
	// Sort is what to order by. Numbers sorting as numbers is most of the
	// reason a decoded index beats the raw string.
	Sort float64 `json:"sort"`
	// Numeric says whether Sort means anything.
	Numeric bool `json:"numeric"`
}

// Table returns the conceptual table containing oid, which may name the table,
// its row, or any of its columns — the three things someone might have
// selected in the tree.
func (s *Service) Table(oid string) (*TableInfo, error) {
	gosmiMu.RLock()
	defer gosmiMu.RUnlock()
	return tableInfo(oid)
}

func tableInfo(oid string) (*TableInfo, error) {
	node, err := lookupNode(oid)
	if err != nil {
		return nil, err
	}

	// Walk up to the table: a column's parent is the row, the row's is the
	// table. Selecting ifDescr and getting ifTable is the useful behaviour.
	switch node.Kind {
	case types.NodeColumn:
		if node, err = parentNode(node); err != nil {
			return nil, fmt.Errorf("%s: no row above this column", oid)
		}
		if node, err = parentNode(node); err != nil {
			return nil, fmt.Errorf("%s: no table above this row", oid)
		}
	case types.NodeRow:
		if node, err = parentNode(node); err != nil {
			return nil, fmt.Errorf("%s: no table above this row", oid)
		}
	case types.NodeTable:
	default:
		return nil, fmt.Errorf("%s is a %s, not a table", oid, node.Kind)
	}

	table := node.AsTable()
	row := node.GetRow()

	info := &TableInfo{
		Oid:     node.Oid.String(),
		Name:    node.Name,
		RowOid:  row.Oid.String(),
		RowName: row.Name,
		Implied: table.Implied,
	}
	if aug := node.GetAugment(); aug.Name != "" {
		info.Augments = aug.Name
	}

	indexNames := make(map[string]bool, len(table.Index))
	for i, idx := range table.Index {
		part := IndexPart{Name: idx.Name, Oid: idx.Oid.String()}
		if idx.Type != nil {
			part.Syntax = idx.Type.Name
		}
		// IMPLIED applies to the last index object and only there does it
		// change how many sub-identifiers to read.
		part.Implied = table.Implied && i == len(table.Index)-1
		info.Index = append(info.Index, part)
		indexNames[idx.Name] = true
	}

	for _, name := range table.ColumnOrder {
		col := table.Columns[name]
		c := TableColumn{
			Name:     col.Name,
			Oid:      col.Oid.String(),
			Access:   col.Access.String(),
			IsIndex:  indexNames[col.Name],
			Writable: isWritable(col.Access),
		}
		if col.Type != nil {
			c.Syntax = col.Type.Name
			if col.Type.Name == "RowStatus" {
				info.RowStatusOid = c.Oid
			}
			if col.Type.Enum != nil && len(col.Type.Enum.Values) > 0 {
				c.EnumValues = make(map[string]int64, len(col.Type.Enum.Values))
				for _, v := range col.Type.Enum.Values {
					c.EnumValues[v.Name] = v.Value
				}
			}
		}
		info.Columns = append(info.Columns, c)
	}

	if len(info.Columns) == 0 {
		return nil, fmt.Errorf("%s has no columns", oid)
	}
	return info, nil
}

// DecodeIndexes splits each instance sub-OID into the values its INDEX clause
// declares. An instance is the part of a walk result's OID after the column.
func (s *Service) DecodeIndexes(tableOid string, instances []string) []DecodedIndex {
	gosmiMu.RLock()
	defer gosmiMu.RUnlock()

	out := make([]DecodedIndex, len(instances))
	node, err := lookupTableNode(tableOid)
	if err != nil {
		// With no MIB there is nothing to decode into. Hand back the raw
		// instance rather than failing, so the table still renders.
		for i, raw := range instances {
			out[i] = DecodedIndex{Raw: raw, Error: err.Error()}
		}
		return out
	}
	table := node.AsTable()

	for i, raw := range instances {
		out[i] = decodeInstance(table, raw)
	}
	return out
}

func decodeInstance(table gosmi.Table, raw string) DecodedIndex {
	res := DecodedIndex{Raw: raw}
	subs, err := parseSubIDs(raw)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	if len(table.Index) == 0 {
		res.Error = "the row declares no INDEX"
		return res
	}

	pos := 0
	for i, idx := range table.Index {
		implied := table.Implied && i == len(table.Index)-1
		value, used, err := decodeOne(idx, subs[pos:], implied)
		if err != nil {
			res.Error = fmt.Sprintf("%s: %v", idx.Name, err)
			return res
		}
		value.Name = idx.Name
		res.Parts = append(res.Parts, value)
		pos += used
	}
	if pos != len(subs) {
		res.Error = fmt.Sprintf("%d sub-identifier(s) left over", len(subs)-pos)
	}
	return res
}

// decodeOne reads one INDEX object off the front of subs and reports how many
// sub-identifiers it consumed.
func decodeOne(node gosmi.SmiNode, subs []uint32, implied bool) (IndexValue, int, error) {
	if len(subs) == 0 {
		return IndexValue{}, 0, fmt.Errorf("ran out of sub-identifiers")
	}
	if node.Type == nil {
		v := uint64(subs[0])
		return IndexValue{Display: strconv.FormatUint(v, 10), Sort: float64(v), Numeric: true}, 1, nil
	}

	switch node.Type.BaseType {
	case types.BaseTypeEnum:
		v := int64(subs[0])
		return IndexValue{
			Display: fmt.Sprintf("%s(%d)", node.Type.Enum.Name(v), v),
			Sort:    float64(v), Numeric: true,
		}, 1, nil

	case types.BaseTypeInteger32, types.BaseTypeUnsigned32,
		types.BaseTypeInteger64, types.BaseTypeUnsigned64:
		v := uint64(subs[0])
		return IndexValue{Display: strconv.FormatUint(v, 10), Sort: float64(v), Numeric: true}, 1, nil

	case types.BaseTypeObjectIdentifier:
		n, start, err := lengthOf(subs, implied, -1)
		if err != nil {
			return IndexValue{}, 0, err
		}
		parts := make([]string, n)
		for i := 0; i < n; i++ {
			parts[i] = strconv.FormatUint(uint64(subs[start+i]), 10)
		}
		return IndexValue{Display: strings.Join(parts, ".")}, start + n, nil

	case types.BaseTypeOctetString, types.BaseTypeBits:
		// IpAddress is an OCTET STRING of four octets that everyone reads as
		// an address, and it is the commonest index in the standard MIBs.
		fixed := fixedSize(node.Type)
		if node.Type.Name == "IpAddress" {
			fixed = 4
		}
		n, start, err := lengthOf(subs, implied, fixed)
		if err != nil {
			return IndexValue{}, 0, err
		}
		octets := make([]byte, n)
		for i := 0; i < n; i++ {
			if subs[start+i] > 255 {
				return IndexValue{}, 0, fmt.Errorf("sub-identifier %d is not an octet", subs[start+i])
			}
			octets[i] = byte(subs[start+i])
		}
		return IndexValue{Display: renderOctets(node.Type, octets)}, start + n, nil
	}
	return IndexValue{}, 0, fmt.Errorf("cannot index on %s", node.Type.BaseType)
}

// lengthOf resolves how many sub-identifiers a variable-length index object
// takes, and where its data starts.
//
// Three cases, and getting one wrong shifts every value after it: a fixed SIZE
// carries no length at all, an IMPLIED last index takes whatever remains, and
// anything else is preceded by its length.
func lengthOf(subs []uint32, implied bool, fixed int) (n, start int, err error) {
	switch {
	case fixed >= 0:
		if len(subs) < fixed {
			return 0, 0, fmt.Errorf("need %d sub-identifiers, have %d", fixed, len(subs))
		}
		return fixed, 0, nil
	case implied:
		return len(subs), 0, nil
	default:
		n = int(subs[0])
		if len(subs) < n+1 {
			return 0, 0, fmt.Errorf("length %d exceeds the %d sub-identifiers left", n, len(subs)-1)
		}
		return n, 1, nil
	}
}

// fixedSize returns the single exact size a type allows, or -1.
func fixedSize(t *models.Type) int {
	if len(t.Ranges) != 1 {
		return -1
	}
	r := t.Ranges[0]
	if r.MinValue != r.MaxValue || r.MinValue < 0 {
		return -1
	}
	return int(r.MinValue)
}

// renderOctets shows a string as text when it is text and as hex when it is
// not: a MAC address read as characters is unreadable, and so is a device name
// read as decimal bytes.
func renderOctets(t *models.Type, b []byte) string {
	if t.Name == "IpAddress" && len(b) == 4 {
		return fmt.Sprintf("%d.%d.%d.%d", b[0], b[1], b[2], b[3])
	}
	printable := len(b) > 0
	for _, c := range b {
		if c < 0x20 || c > 0x7e {
			printable = false
			break
		}
	}
	if printable {
		return string(b)
	}
	parts := make([]string, len(b))
	for i, c := range b {
		parts[i] = fmt.Sprintf("%02x", c)
	}
	return strings.Join(parts, ":")
}

func parseSubIDs(raw string) ([]uint32, error) {
	raw = strings.TrimPrefix(strings.TrimSpace(raw), ".")
	if raw == "" {
		return nil, fmt.Errorf("empty instance")
	}
	fields := strings.Split(raw, ".")
	out := make([]uint32, len(fields))
	for i, f := range fields {
		v, err := strconv.ParseUint(f, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("%q is not a sub-identifier", f)
		}
		out[i] = uint32(v)
	}
	return out, nil
}

// isWritable reports whether a column can be written.
//
// read-create needs no case of its own: gosmi folds it into ReadWrite while
// parsing (parser.Access.ToSmi maps AccessReadWrite and AccessReadCreate to
// the same value), and types.Access has no ReadCreate member at all. An
// earlier version of this function tested for one by name, which could never
// match — TestGosmiFoldsReadCreateIntoReadWrite pins the behaviour we rely on
// instead.
func isWritable(a types.Access) bool {
	return a == types.AccessReadWrite
}

func lookupNode(oid string) (gosmi.SmiNode, error) {
	smiOid, err := types.OidFromString(strings.TrimSpace(oid))
	if err != nil {
		return gosmi.SmiNode{}, fmt.Errorf("%q is not an OID", oid)
	}
	node, err := gosmi.GetNodeByOID(smiOid)
	if err != nil {
		return gosmi.SmiNode{}, fmt.Errorf("%s is not in any loaded MIB", oid)
	}
	return node, nil
}

// parentNode resolves the node one level up by OID, which is how a column
// reaches its row and a row its table.
func parentNode(node gosmi.SmiNode) (gosmi.SmiNode, error) {
	if len(node.Oid) < 2 {
		return gosmi.SmiNode{}, fmt.Errorf("%s has no parent", node.Name)
	}
	parent := node.Oid[:len(node.Oid)-1]
	return lookupNode(parent.String())
}

// lookupTableNode resolves an OID to the table node itself.
func lookupTableNode(oid string) (gosmi.SmiNode, error) {
	info, err := tableInfo(oid)
	if err != nil {
		return gosmi.SmiNode{}, err
	}
	return lookupNode(info.Oid)
}

// EncodeIndex builds the instance sub-OID for a row from one value per INDEX
// object — what creating a row needs, since the row's identity IS its index.
//
// Deliberately not gosmi's Type.IndexValue: that one always writes a length
// prefix for an OCTET STRING unless IMPLIED, and a fixed-SIZE index carries no
// length at all. An IpAddress index encoded its way comes out five
// sub-identifiers long and addresses a row that does not exist. This mirrors
// decodeOne rule for rule, and a round-trip test holds the two together.
func (s *Service) EncodeIndex(tableOid string, values []string) (string, error) {
	gosmiMu.RLock()
	defer gosmiMu.RUnlock()

	node, err := lookupTableNode(tableOid)
	if err != nil {
		return "", err
	}
	table := node.AsTable()
	if len(table.Index) == 0 {
		return "", fmt.Errorf("%s declares no INDEX", tableOid)
	}
	if len(values) != len(table.Index) {
		return "", fmt.Errorf("%s needs %d index value(s), got %d", node.Name, len(table.Index), len(values))
	}

	var subs []uint32
	for i, idx := range table.Index {
		implied := table.Implied && i == len(table.Index)-1
		enc, err := encodeOne(idx, strings.TrimSpace(values[i]), implied)
		if err != nil {
			return "", fmt.Errorf("%s: %w", idx.Name, err)
		}
		subs = append(subs, enc...)
	}

	parts := make([]string, len(subs))
	for i, v := range subs {
		parts[i] = strconv.FormatUint(uint64(v), 10)
	}
	return strings.Join(parts, "."), nil
}

func encodeOne(node gosmi.SmiNode, value string, implied bool) ([]uint32, error) {
	if value == "" {
		return nil, fmt.Errorf("no value given")
	}
	if node.Type == nil {
		v, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("%q is not a number", value)
		}
		return []uint32{uint32(v)}, nil
	}

	switch node.Type.BaseType {
	case types.BaseTypeEnum:
		// A label is what the UI shows, so it has to be accepted back.
		if v, err := node.Type.Enum.Value(value); err == nil {
			return []uint32{uint32(v)}, nil
		}
		fallthrough

	case types.BaseTypeInteger32, types.BaseTypeUnsigned32,
		types.BaseTypeInteger64, types.BaseTypeUnsigned64:
		v, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("%q is not a number", value)
		}
		return []uint32{uint32(v)}, nil

	case types.BaseTypeObjectIdentifier:
		subs, err := parseSubIDs(value)
		if err != nil {
			return nil, err
		}
		return withLength(subs, implied, -1)

	case types.BaseTypeOctetString, types.BaseTypeBits:
		fixed := fixedSize(node.Type)
		var octets []uint32
		if node.Type.Name == "IpAddress" {
			fixed = 4
			ip, err := parseDottedQuad(value)
			if err != nil {
				return nil, err
			}
			octets = ip
		} else {
			octets = make([]uint32, 0, len(value))
			for _, b := range []byte(value) {
				octets = append(octets, uint32(b))
			}
		}
		return withLength(octets, implied, fixed)
	}
	return nil, fmt.Errorf("cannot index on %s", node.Type.BaseType)
}

// withLength prefixes a variable-length index value with its length, following
// the same three cases lengthOf reads back.
func withLength(subs []uint32, implied bool, fixed int) ([]uint32, error) {
	switch {
	case fixed >= 0:
		if len(subs) != fixed {
			return nil, fmt.Errorf("needs exactly %d octet(s), got %d", fixed, len(subs))
		}
		return subs, nil
	case implied:
		return subs, nil
	default:
		if len(subs) > 0xffffffff {
			return nil, fmt.Errorf("value too long")
		}
		return append([]uint32{uint32(len(subs))}, subs...), nil
	}
}

func parseDottedQuad(s string) ([]uint32, error) {
	fields := strings.Split(s, ".")
	if len(fields) != 4 {
		return nil, fmt.Errorf("%q is not an IPv4 address", s)
	}
	out := make([]uint32, 4)
	for i, f := range fields {
		v, err := strconv.ParseUint(f, 10, 8)
		if err != nil {
			return nil, fmt.Errorf("%q is not an IPv4 address", s)
		}
		out[i] = uint32(v)
	}
	return out, nil
}
