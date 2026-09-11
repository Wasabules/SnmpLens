package mib

import (
	"strconv"
	"strings"

	"github.com/sleepinggenius2/gosmi"
	"github.com/sleepinggenius2/gosmi/types"
)

// OidName is an OID as a MIB names it: the module, the object, and what the OID
// carries past the object — the instance. "IF-MIB::ifDescr.3" is what someone
// looks a MIB up for; ".1.3.6.1.2.1.2.2.1.2.3" is not.
type OidName struct {
	Module   string `json:"module"`
	Object   string `json:"object"`
	Instance string `json:"instance"`
}

// String is the name as net-snmp writes one, or "" when no loaded MIB names
// the OID.
func (n OidName) String() string {
	if n.Object == "" {
		return ""
	}
	out := n.Object
	if n.Module != "" {
		out = n.Module + "::" + out
	}
	if n.Instance != "" {
		out += "." + n.Instance
	}
	return out
}

// NameOIDs names each OID from the loaded MIBs, holding the lock once for the
// batch. Only an object that holds a value — a scalar or a column — names an
// OID: gosmi answers any OID with the closest node it knows, and
// "SNMPv2-SMI::enterprises" followed by nine arcs says nothing a search could
// use. An OID no loaded MIB names is left out of the map.
func (s *Service) NameOIDs(oids []string) map[string]OidName {
	gosmiMu.Lock()
	defer gosmiMu.Unlock()
	out := make(map[string]OidName, len(oids))
	for _, o := range oids {
		id, err := types.OidFromString(strings.TrimPrefix(o, "."))
		if err != nil {
			continue
		}
		node, err := gosmi.GetNodeByOID(id)
		if err != nil || (node.Kind != types.NodeScalar && node.Kind != types.NodeColumn) || len(node.Oid) > len(id) {
			continue
		}
		arcs := make([]string, 0, len(id)-len(node.Oid))
		for _, a := range id[len(node.Oid):] {
			arcs = append(arcs, strconv.FormatUint(uint64(a), 10))
		}
		out[o] = OidName{Module: node.GetModule().Name, Object: node.Name, Instance: strings.Join(arcs, ".")}
	}
	return out
}
