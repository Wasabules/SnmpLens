package simulator

import (
	"fmt"
	"maps"
	"slices"
)

// A model can be given numbers: how many ports a switch has, disks a NAS,
// outlets a PDU, processors a server. Each is bounded by what the model could
// be — a Catalyst 2960 of 24 ports has at most 24 — and the interface names it
// from its name (simulator.param.<name>), in five languages, as it names the
// models. A device given none is its model as the catalogue describes it.

// ModelParam is a number a device of a model may be given.
type ModelParam struct {
	Name    string `json:"name"`
	Min     int    `json:"min"`
	Max     int    `json:"max"`
	Default int    `json:"default"`
}

// count is the number a device was given for name, or def — what its model
// has — when it was given none.
func (id Identity) count(name string, def int) int {
	if n, ok := id.Params[name]; ok {
		return n
	}
	return def
}

// checkParams reports the first of params the model does not take, or takes
// within other bounds.
func (m model) checkParams(params map[string]int) error {
	for _, name := range slices.Sorted(maps.Keys(params)) {
		i := slices.IndexFunc(m.Params, func(p ModelParam) bool { return p.Name == name })
		if i < 0 {
			return fmt.Errorf("%q is not a parameter of this model", name)
		}
		if p, n := m.Params[i], params[name]; n < p.Min || n > p.Max {
			return fmt.Errorf("%s is %d to %d, not %d", name, p.Min, p.Max, n)
		}
	}
	return nil
}
