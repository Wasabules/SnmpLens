package mib

import (
	"strings"

	"github.com/sleepinggenius2/gosmi"
	"github.com/sleepinggenius2/gosmi/types"
)

// RowStatusColumn reports whether instance lies in a column whose SYNTAX is
// RowStatus in a loaded MIB, and that column's OID: what a simulated agent
// needs to create and destroy rows as RFC 2579 has an agent do, without a MIB
// of its own. gosmi answers an OID with the closest node at or above it, which
// for an instance of a column is the column.
func (s *Service) RowStatusColumn(instance string) (string, bool) {
	gosmiMu.Lock()
	defer gosmiMu.Unlock()
	id, err := types.OidFromString(strings.TrimPrefix(instance, "."))
	if err != nil {
		return "", false
	}
	node, err := gosmi.GetNodeByOID(id)
	if err != nil || node.Kind != types.NodeColumn || node.Type == nil || node.Type.Name != "RowStatus" ||
		len(node.Oid) >= len(id) {
		return "", false
	}
	return node.Oid.String(), true
}
