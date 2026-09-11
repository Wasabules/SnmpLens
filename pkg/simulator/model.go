package simulator

import "github.com/gosnmp/gosnmp"

// ModelInfo is a device model as the interface lists it. It carries no prose:
// the interface names and describes a model from its ID, in five languages
// (simulator.model.<id>), the way preset.WidgetKinds serves i18n key suffixes
// rather than sentences.
type ModelInfo struct {
	ID       string `json:"id"`
	Category string `json:"category"`
}

// Identity is what makes one device differ from another of the same model.
type Identity struct {
	// Name is sysName, and the host name wherever the device repeats it.
	Name string
	// Seed decides how the device's values move, so two devices of one model
	// do not move in step.
	Seed uint64
}

type model struct {
	ModelInfo
	// enterprise is the vendor's number, which the engine ID carries.
	enterprise uint32
	build      func(Identity) []Object
}

var models = []model{linuxServer}

// Models lists the models a device can be made from.
func Models() []ModelInfo {
	out := make([]ModelInfo, len(models))
	for i, m := range models {
		out[i] = m.ModelInfo
	}
	return out
}

func findModel(id string) (model, bool) {
	for _, m := range models {
		if m.ID == id {
			return m, true
		}
	}
	return model{}, false
}

// objects accumulates a model's objects without restating Object{…} each time.
type objects []Object

func (o *objects) add(oid string, t gosnmp.Asn1BER, r Reading) {
	*o = append(*o, Object{OID: oid, Type: t, Value: r})
}
