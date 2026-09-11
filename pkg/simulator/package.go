package simulator

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/gosnmp/gosnmp"
)

// A model package is a model spread over the files of one folder, each saying
// one thing: model.json what the device is, oids.json and oids/*.json the OIDs
// it answers, traps.json its notifications, and walks/ what a real device of
// the kind answered, recorded. model.json is a model file like any other, and a
// package that holds nothing else is the model it holds.
//
// Three rules put the files together, each the answer to a question a package
// raises and a lone file does not.
//
//   - What is written wins over what was recorded, OID by OID: an oids file is
//     how a recorded constant is made to move, or a recorded value corrected.
//     Between the written files an OID given twice is an error, as it is in
//     one file.
//   - The agent's own objects are the agent's. A walk of a real device records
//     its snmp group, its engine, its USM users and its VACM and community
//     tables; none of it is served, and the count left out is said.
//   - The system group is made, never replayed: sysName is the device's name
//     and sysUpTime its own uptime. What the walks recorded of the rest —
//     sysDescr, sysObjectID, contact, location, services — is the device's
//     identity, except where model.json's "system" says otherwise.
//
// A recorded counter moves: it starts at the value recorded and grows at the
// rate it had grown at on average since the device booted — the value over the
// sysUpTime recorded beside it — swinging around that rate as traffic does.
// Everything else a walk recorded answers what it answered.

// PackageModelFile is the file that makes the folder holding it a package.
const PackageModelFile = "model.json"

// PackageFile is one file of a package, by its name in the package: its path
// under the folder that holds model.json.
type PackageFile struct {
	Name string
	Data []byte
}

// PackageWarning is something said about a package that was imported anyway:
// an i18n key suffix (simulator.models.warning.<key>) and what it quotes.
type PackageWarning struct {
	Key    string
	Detail string
}

// The roles of the files a package reads.
const (
	roleModel = "model"
	roleTraps = "traps"
	roleOIDs  = "oids"
	roleWalk  = "walk"
)

// packageRole is what a file of a package is for, from its name in it. The
// fixed part of a name is read whatever its case: an archive made on Windows
// may well say Model.json.
func packageRole(name string) string {
	lower := strings.ToLower(name)
	switch lower {
	case PackageModelFile:
		return roleModel
	case "traps.json":
		return roleTraps
	case "oids.json":
		return roleOIDs
	}
	dir, base := path.Split(lower)
	if base == "" || strings.HasPrefix(base, ".") || len(base) > 128 {
		return ""
	}
	switch {
	case dir == "oids/" && path.Ext(base) == ".json":
		return roleOIDs
	case dir == "walks/":
		return roleWalk
	}
	return ""
}

// PackageReads reports whether a package reads the file of that name. Nothing
// else in its folder is: it is somebody's notes, or a file named wrong, which
// the import names.
func PackageReads(name string) bool { return packageRole(name) != "" }

var bom = []byte("\xef\xbb\xbf")

// agentSubtrees are what a recorded walk never serves, because the agent
// answers them itself or they were the recorded agent's configuration:
// SNMPv2-MIB's snmp group, and snmpModules — the engine, the MPD and USM
// statistics, and the USM, VACM, target, notification and community tables,
// which hold the recorded device's users and community strings.
var agentSubtrees = []oid{{1, 3, 6, 1, 2, 1, 11}, {1, 3, 6, 1, 6, 3}}

// ifMIBSubtrees are what model.json's "interfaces" makes. A walk's own are left
// out when it says any, rather than mixed in with the rows it makes.
var ifMIBSubtrees = []oid{{1, 3, 6, 1, 2, 1, 2}, {1, 3, 6, 1, 2, 1, 31, 1}}

var (
	oidSysGroup       = oid{1, 3, 6, 1, 2, 1, 1}
	oidHRSystemUptime = oid{1, 3, 6, 1, 2, 1, 25, 1, 1, 0}
	oidHRSystemDate   = oid{1, 3, 6, 1, 2, 1, 25, 1, 2, 0}
)

const (
	// recordedUptime is how long a recorded device is taken to have been up
	// when its walks do not say: a month, which a device in service shows.
	recordedUptime = 30 * 86400.0
	// recordedSwing is how far a recorded counter's rate swings.
	recordedSwing = 0.3
)

// packageParts is what a package adds to its model.json.
type packageParts struct {
	objects       []objectSource
	notifications []notificationSource
	// walks counts the walk files, and recorded is what they recorded between
	// them, the first file to record an OID giving its value.
	walks    int
	recorded []walkEntry
	// system is what they recorded of the system group, and uptime how long,
	// in seconds, the device had been up when they were recorded.
	system customSystem
	uptime float64
}

// ParseCustomPackage reads a package from its files and checks all of it, as
// ParseCustomModel does a lone file: a refusal names the file and, in it, the
// field, the OID or the line.
func ParseCustomPackage(files []PackageFile) (CustomModel, []PackageWarning, error) {
	files = slices.Clone(files)
	slices.SortFunc(files, func(a, b PackageFile) int { return strings.Compare(a.Name, b.Name) })
	var (
		model    *PackageFile
		parts    packageParts
		warnings []PackageWarning
		seen     = map[string]bool{}
	)
	for i := range files {
		f := &files[i]
		role := packageRole(f.Name)
		if role != "" && role != roleWalk && len(f.Data) > MaxCustomModelBytes {
			return CustomModel{}, nil, fmt.Errorf("%s: a JSON file of a package is at most %d KB", f.Name, MaxCustomModelBytes>>10)
		}
		switch role {
		case roleModel:
			if model != nil {
				return CustomModel{}, nil, fmt.Errorf("the package holds %s and %s, and has one model file", model.Name, f.Name)
			}
			model = f
		case roleTraps:
			var t struct {
				Notifications []customNotification `json:"notifications"`
			}
			if err := decodeStrict(f.Data, &t); err != nil {
				return CustomModel{}, nil, fmt.Errorf("%s: %w", f.Name, err)
			}
			parts.notifications = append(parts.notifications, notificationSource{file: f.Name, list: t.Notifications})
		case roleOIDs:
			var o struct {
				Objects []customObject `json:"objects"`
			}
			if err := decodeStrict(f.Data, &o); err != nil {
				return CustomModel{}, nil, fmt.Errorf("%s: %w", f.Name, err)
			}
			parts.objects = append(parts.objects, objectSource{file: f.Name, list: o.Objects})
		case roleWalk:
			w, err := readWalk(f.Data)
			if err != nil {
				return CustomModel{}, nil, fmt.Errorf("%s: %w", f.Name, err)
			}
			if len(w.entries) == 0 {
				if w.skipped > 0 {
					return CustomModel{}, nil, fmt.Errorf("%s: no value in it could be read — %s", f.Name, w.first)
				}
				return CustomModel{}, nil, fmt.Errorf("%s records nothing", f.Name)
			}
			if w.skipped > 0 {
				warnings = append(warnings, PackageWarning{Key: "walkSkipped",
					Detail: fmt.Sprintf("%s: %d, the first at %s", f.Name, w.skipped, w.first)})
			}
			for _, e := range w.entries {
				if !seen[e.name] {
					seen[e.name] = true
					parts.recorded = append(parts.recorded, e)
				}
			}
			if len(parts.recorded) > MaxRecordedObjects {
				return CustomModel{}, nil, fmt.Errorf("the walks record %d objects between them, and a package keeps at most %d",
					len(parts.recorded), MaxRecordedObjects)
			}
			parts.walks++
		default:
			return CustomModel{}, nil, fmt.Errorf("%s is not a file a package reads", f.Name)
		}
	}
	if model == nil {
		return CustomModel{}, nil, errors.New("the package has no model.json, which says what the device is")
	}
	f, err := decodeModel(bytes.TrimPrefix(model.Data, bom))
	if err != nil {
		return CustomModel{}, nil, fmt.Errorf("%s: %w", model.Name, err)
	}
	if left := parts.settle(); left > 0 {
		warnings = append(warnings, PackageWarning{Key: "walkAgentOwned", Detail: strconv.Itoa(left)})
	}
	m, err := f.compile(parts)
	if err != nil {
		return CustomModel{}, nil, err
	}
	return m, warnings, nil
}

// decodeStrict reads one JSON object into v, refusing a field the format does
// not define, and anything after the object.
func decodeStrict(raw []byte, v any) error {
	raw = bytes.TrimPrefix(raw, bom)
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return jsonProblem(raw, err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("there is more after the object: a file holds one")
	}
	return nil
}

// settle takes out of what the walks recorded what the model never serves
// from them: the agent's own objects, which it counts, and the system group's
// scalars, which it keeps as the identity the walks recorded and the uptime
// their counters are measured against.
func (p *packageParts) settle() (agentOwned int) {
	var sysUp, hrUp float64
	kept := p.recorded[:0]
	for _, e := range p.recorded {
		switch {
		case slices.ContainsFunc(agentSubtrees, e.oid.hasPrefix):
			agentOwned++
			continue
		case len(e.oid) == len(oidSysGroup)+2 && e.oid.hasPrefix(oidSysGroup) && e.oid[len(e.oid)-1] == 0 &&
			e.oid[len(oidSysGroup)] >= 1 && e.oid[len(oidSysGroup)] <= 7:
			if n := e.oid[len(oidSysGroup)]; n == 3 {
				if ticks, ok := e.val.(uint32); ok {
					sysUp = float64(ticks) / 100
				}
			} else {
				p.system.set(n, e)
			}
			continue
		case slices.Equal(e.oid, oidHRSystemUptime):
			if ticks, ok := e.val.(uint32); ok {
				hrUp = float64(ticks) / 100
			}
		}
		kept = append(kept, e)
	}
	p.recorded = kept
	p.uptime = recordedUptime
	switch {
	case sysUp > 0:
		p.uptime = sysUp
	case hrUp > 0:
		p.uptime = hrUp
	}
	// A device recorded seconds after it booted would count as if it had
	// carried its whole traffic in those seconds.
	p.uptime = max(p.uptime, 60)
	return agentOwned
}

// set keeps what a walk recorded of the system group's scalar n. sysName is
// not kept: it is the device's own name.
func (s *customSystem) set(n uint32, e walkEntry) {
	text, _ := e.val.(string)
	if b, ok := e.val.([]byte); ok {
		text = string(b)
	}
	switch n {
	case 1:
		s.Descr = text
	case 2:
		if e.typ == gosnmp.ObjectIdentifier {
			s.ObjectID = text
		}
	case 4:
		s.Contact = text
	case 6:
		s.Location = text
	case 7:
		if v, ok := e.val.(int); ok {
			s.Services = &v
		}
	}
}

// systemOf is the system group a model answers: model.json's "system" for a
// model without walks, and for one with them what they recorded, with what
// model.json's "system" gives in its place.
func (f customFile) systemOf(p packageParts) (systemGroup, error) {
	if p.walks == 0 {
		return f.System.check()
	}
	s := p.system
	if g := f.System; g != nil {
		s.Descr = cmp.Or(g.Descr, s.Descr)
		s.ObjectID = cmp.Or(g.ObjectID, s.ObjectID)
		s.Contact = cmp.Or(g.Contact, s.Contact)
		s.Location = cmp.Or(g.Location, s.Location)
		if g.Services != nil {
			s.Services = g.Services
		}
	}
	switch {
	case strings.TrimSpace(s.Descr) == "":
		return systemGroup{}, errors.New("no sysDescr: the walks record none, and model.json's \"system\" gives no \"descr\"")
	case s.ObjectID == "":
		return systemGroup{}, errors.New("no sysObjectID: the walks record none, and model.json's \"system\" gives no \"objectId\"")
	}
	return s.check()
}

// unwritten is what the walks recorded that nothing written replaces: every OID
// the model's own files make wins over the walks', and so does the whole of
// IF-MIB when model.json says "interfaces".
func (p packageParts) unwritten(written []Object, interfaces bool) []walkEntry {
	if len(p.recorded) == 0 {
		return nil
	}
	made := make(map[string]bool, len(written))
	for _, o := range written {
		if id, err := parseOID(o.OID); err == nil {
			made[id.String()] = true
		}
	}
	return slices.DeleteFunc(slices.Clone(p.recorded), func(e walkEntry) bool {
		return made[e.name] || (interfaces && slices.ContainsFunc(ifMIBSubtrees, e.oid.hasPrefix))
	})
}

// appendRecorded adds what the walks recorded to a device's objects.
func appendRecorded(o []Object, id Identity, entries []walkEntry, uptime float64) []Object {
	for i, e := range entries {
		r := Const(e.val)
		switch {
		case e.typ == gosnmp.Counter32 || e.typ == gosnmp.Counter64:
			var start uint64
			switch v := e.val.(type) {
			case uint32:
				start = uint64(v)
			case uint64:
				start = v
			}
			s := Swing{Depth: recordedSwing, Seed: id.Seed + 1<<22 + uint64(i)}
			r = Counter(float64(start)/uptime, start, s)
			if e.typ == gosnmp.Counter64 {
				r = Counter64(float64(start)/uptime, start, s)
			}
		case e.typ == gosnmp.TimeTicks && slices.Equal(e.oid, oidHRSystemUptime):
			r = Uptime()
		case e.typ == gosnmp.OctetString && slices.Equal(e.oid, oidHRSystemDate):
			r = DateAndTime()
		}
		o = append(o, Object{OID: e.name, Type: e.typ, Value: r})
	}
	return o
}
