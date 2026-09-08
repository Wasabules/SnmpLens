package preset

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Reading presets off disk.
//
// Everything here is about a file SOMEBODY ELSE WROTE, which is the whole
// premise of the format: it arrives from a community repository, an email or a
// vendor's site, and nothing about it may be assumed. So this file parses,
// bounds and reports; it never repairs, and it never decides.

// MaxFileBytes bounds a preset file before it is read.
//
// The bound is checked with os.Stat rather than after os.ReadFile, the way
// importSingleFile does for MIBs: a size check that happens after the read has
// already spent the memory it exists to refuse. A preset at every one of this
// package's sanity bounds — 40 widgets, 500 OIDs, 120-character strings — is
// well under 100 KB, so a megabyte is generous by an order of magnitude and
// still refuses the file that is a video with the wrong extension.
const MaxFileBytes = 1 << 20

// Info is one preset as a LIST shows it: enough to choose between files,
// without opening the widgets.
//
// Problems is a count rather than the errors themselves. A directory listing
// that carried every validation error of every file would be most of the data
// for a screen that shows none of it; the errors are what ReadPreset is for,
// and this says which file to ask about.
type Info struct {
	File        string `json:"file"`
	Name        string `json:"name"`
	Author      string `json:"author,omitempty"`
	Vendor      string `json:"vendor,omitempty"`
	Description string `json:"description,omitempty"`
	// SysObjectIDPrefix is carried so a caller can rank a list against a device
	// without re-reading every file.
	SysObjectIDPrefix []string `json:"sysObjectIdPrefix,omitempty"`
	IntervalSec       int      `json:"intervalSec"`
	Widgets           int      `json:"widgets"`
	OIDs              int      `json:"oids"`
	Problems          int      `json:"problems"`
}

// Parse reads a preset from bytes and validates it.
//
// A file that is not JSON at all is reported as one error with an empty Field,
// not as a bare encoding/json message: the caller renders a field path beside
// each problem, and "invalid character 'P' looking for beginning of value" in
// that column reads as a problem with a field called nothing.
//
// DisallowUnknownFields is deliberately NOT used. FormatVersion is how this
// format handles a file from a version that has moved on, and refusing an
// unknown key would refuse exactly the files that mechanism exists to accept.
func Parse(data []byte) (Preset, []Error) {
	var p Preset
	if err := json.Unmarshal(data, &p); err != nil {
		return p, []Error{errf("", "notJson", map[string]string{"detail": clip(err.Error())})}
	}
	return p, Validate(p)
}

// LoadFile reads one preset from disk.
//
// Three answers, and they are not the same thing: err is "this file could not
// be read", the error slice is "this file is not a valid preset", and both
// empty is a preset. Collapsing the first two would report a permission
// problem as a formatting problem.
func LoadFile(path string) (Preset, []Error, error) {
	st, err := os.Stat(path)
	if err != nil {
		return Preset{}, nil, err
	}
	if st.IsDir() {
		return Preset{}, nil, fmt.Errorf("%s is a directory", filepath.Base(path))
	}
	if st.Size() > MaxFileBytes {
		return Preset{}, []Error{errf("", "tooBig", map[string]string{
			"found": fmt.Sprint(st.Size()), "max": fmt.Sprint(MaxFileBytes),
		})}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Preset{}, nil, err
	}
	p, errs := Parse(data)
	return p, errs, nil
}

// List reads a directory of presets.
//
// One unreadable or malformed file must never hide the others: a broken preset
// is listed with its problem count, and a file that cannot be read at all is
// skipped rather than aborting the sweep. That is the rule ListMibFiles
// follows for the same reason — a folder of files from strangers has one bad
// one in it, and a listing that fails entirely is a listing nobody can use to
// find which.
func List(dir string) ([]Info, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			// A directory nobody has put a preset in yet is empty, not broken.
			return []Info{}, nil
		}
		return nil, err
	}

	out := make([]Info, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		full := filepath.Join(dir, e.Name())
		p, errs, err := LoadFile(full)
		if err != nil {
			continue
		}
		out = append(out, Info{
			File:              e.Name(),
			Name:              p.Name,
			Author:            p.Author,
			Vendor:            p.Match.Vendor,
			Description:       p.Description,
			SysObjectIDPrefix: p.Match.SysObjectIDPrefix,
			IntervalSec:       p.IntervalSec,
			Widgets:           len(p.Widgets),
			OIDs:              Estimate(p).OIDs,
			Problems:          len(errs),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].File < out[j].File
	})
	return out, nil
}

// prefixDepth is how many sub-identifiers of prefix an OID matches, or zero.
//
// Arc by arc, never as text. strings.HasPrefix is the obvious implementation
// and it is wrong in the way that matters here: "1.3.6.1.4.1.9" is Cisco and
// "1.3.6.1.4.1.911" is somebody else entirely, and a text prefix says they are
// the same vendor. Leading dots are stripped on both sides, because
// formatSnmpValue renders an ObjectIdentifier WITH one and a preset file is
// written by hand either way.
func prefixDepth(prefix, oid string) int {
	pa := strings.Split(strings.TrimPrefix(strings.TrimSpace(prefix), "."), ".")
	oa := strings.Split(strings.TrimPrefix(strings.TrimSpace(oid), "."), ".")
	if len(pa) == 0 || pa[0] == "" || len(pa) > len(oa) {
		return 0
	}
	for i, arc := range pa {
		if arc != oa[i] {
			return 0
		}
	}
	return len(pa)
}

// MatchDepth says how specifically a preset claims this device, in
// sub-identifiers. Zero is "it does not".
//
// Depth rather than a boolean because two presets can both match — a vendor's
// generic one and a model's — and the specific one is the better advice.
func MatchDepth(prefixes []string, sysObjectID string) int {
	if strings.TrimSpace(sysObjectID) == "" {
		return 0
	}
	best := 0
	for _, prefix := range prefixes {
		if d := prefixDepth(prefix, sysObjectID); d > best {
			best = d
		}
	}
	return best
}

// Matches reports whether this preset claims this device.
func Matches(p Preset, sysObjectID string) bool {
	return MatchDepth(p.Match.SysObjectIDPrefix, sysObjectID) > 0
}

// Rank orders a listing for one device: what claims it first, most specific
// first, then everything else by name.
//
// It ORDERS, it never filters. A preset that says nothing about which device it
// is for is still a preset the operator may have downloaded for this one, and
// Match is documented as advice to the person binding — never an automatic
// action. Hiding the rest would turn that advice into a decision.
func Rank(infos []Info, sysObjectID string) []Info {
	out := make([]Info, len(infos))
	copy(out, infos)
	depth := make(map[string]int, len(out))
	for _, in := range out {
		depth[in.File] = MatchDepth(in.SysObjectIDPrefix, sysObjectID)
	}
	sort.SliceStable(out, func(i, j int) bool {
		di, dj := depth[out[i].File], depth[out[j].File]
		if di != dj {
			return di > dj
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].File < out[j].File
	})
	return out
}
