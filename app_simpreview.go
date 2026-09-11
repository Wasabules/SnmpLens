package main

import (
	"strings"

	"SnmpLens/pkg/mib"
	"SnmpLens/pkg/simulator"
)

// SimulatorPreviewRow is one object a simulated device would answer, named
// from the loaded MIBs when they know it.
type SimulatorPreviewRow struct {
	OID   string `json:"oid"`
	Name  string `json:"name"`
	Type  string `json:"type"`
	Value string `json:"value"`
}

// SimulatorPreviewPage is what a device would answer under a subtree: the
// first rows of it, and how many there are in all.
type SimulatorPreviewPage struct {
	Rows  []SimulatorPreviewRow `json:"rows"`
	Total int                   `json:"total"`
}

// SimulatorPreview is what a device, as the editor holds it, would answer under
// subtree the moment it starts — saved or not, running or not. It needs no
// secret and keeps none: the device need only have a model, and parameters and
// values of its own the model can take.
func (a *App) SimulatorPreview(d simulator.Device, subtree string) (SimulatorPreviewPage, error) {
	out := SimulatorPreviewPage{Rows: []SimulatorPreviewRow{}}
	p, err := simulator.PreviewDevice(d.WithoutSecrets(), subtree, simulator.MaxPreviewRows)
	if err != nil {
		return out, err
	}
	out.Total = p.Total
	// One lock for the page, not one per row: pkg/mib takes gosmi's for the batch.
	var names map[string]mib.OidInfo
	if a.mibService != nil && len(p.Rows) > 0 {
		oids := make([]string, len(p.Rows))
		for i, r := range p.Rows {
			oids[i] = strings.TrimPrefix(r.OID, ".")
		}
		names = a.mibService.ResolveOids(oids)
	}
	for _, r := range p.Rows {
		out.Rows = append(out.Rows, SimulatorPreviewRow{OID: r.OID, Name: names[strings.TrimPrefix(r.OID, ".")].Name,
			Type: r.Type, Value: r.Value})
	}
	return out, nil
}
