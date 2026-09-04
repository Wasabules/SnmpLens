package mib

import (
	"log"
	"sync"

	"github.com/sleepinggenius2/gosmi"
)

// gosmi keeps its whole world in package-level state: one search path, one
// module table, one node index. Every exported call here reaches into it.
//
// Wails dispatches each bound method on its own goroutine, so a batch
// ResolveOids from the operations panel can already overlap a LoadAll from the
// MIB settings tab — a read racing a mutation of that shared table. The editor
// makes it far more likely, because saving a MIB tears the world down and
// rebuilds it while the rest of the app carries on resolving OIDs.
//
// One RWMutex for the package: writers are the loaders and the rebuild,
// readers are the lookups.
var gosmiMu sync.RWMutex

// Reloaded reports what a rebuild produced.
type Reloaded struct {
	Tree        []*Node         `json:"tree"`
	Diagnostics []MibLoadResult `json:"diagnostics"`
	// Health is the answer to the only question that matters after editing a
	// MIB the rest of the app depends on: does the tree still resolve.
	Health Health `json:"health"`
}

// Health is a smoke test of the rebuilt tree.
type Health struct {
	Ok bool `json:"ok"`
	// Failures name the probes that did not resolve, in a form a person can
	// act on.
	Failures []string `json:"failures"`
	// Modules is how many are loaded, so "0" is visible rather than implied.
	Modules int `json:"modules"`
}

// healthProbes are OIDs that must resolve in any working installation. They
// come from the core modules every other MIB imports from, so if these fail
// the tree is broken regardless of which file was edited.
var healthProbes = []struct{ oid, want string }{
	{"1.3.6.1.2.1.1.1", "sysDescr"},
	{"1.3.6.1.2.1.1.3", "sysUpTime"},
	{"1.3.6.1.2.1.2.2.1.10", "ifInOctets"},
}

// Rebuild tears gosmi down and loads the given files again.
//
// A full Exit/Init is the only way to forget a module: gosmi has no unload, so
// after editing a MIB the previously parsed version would otherwise stay in
// the table and the editor would appear to do nothing. The search path is
// reapplied inside the lock because Init resets it.
func (s *Service) Rebuild(fileNames []string) Reloaded {
	gosmiMu.Lock()
	gosmi.Exit()
	gosmi.Init()
	gosmi.AppendPath(s.path)

	// The core modules first: everything else imports from them, and loading
	// them explicitly makes a broken one show up as itself rather than as a
	// hundred confusing failures in unrelated files.
	var diagnostics []MibLoadResult
	for _, core := range []string{"SNMPv2-SMI", "SNMPv2-TC"} {
		if _, err := gosmi.LoadModule(core); err != nil {
			diagnostics = append(diagnostics, MibLoadResult{
				FileName: core, Success: false,
				Error: "core module failed to load: " + err.Error(),
			})
			log.Printf("mib: core module %s failed to reload: %v", core, err)
		}
	}
	gosmiMu.Unlock()

	resp := s.LoadWithDiagnostics(fileNames)
	resp.Diagnostics = append(diagnostics, resp.Diagnostics...)

	return Reloaded{
		Tree:        resp.Tree,
		Diagnostics: resp.Diagnostics,
		Health:      checkHealth(),
	}
}

// checkHealth resolves a few OIDs that must work.
func checkHealth() Health {
	gosmiMu.RLock()
	defer gosmiMu.RUnlock()

	h := Health{Ok: true, Failures: []string{}, Modules: len(gosmi.GetLoadedModules())}
	for _, probe := range healthProbes {
		node, err := gosmi.GetNode(probe.want)
		switch {
		case err != nil || node.Name != probe.want:
			h.Ok = false
			h.Failures = append(h.Failures, probe.want+" ("+probe.oid+") no longer resolves")

		case node.Oid.String() != probe.oid:
			// The OID, which the probe has always carried and never compared.
			//
			// gosmi resolves imports lazily and returns nil rather than
			// failing, so gutting SNMPv2-SMI leaves every dependent module
			// "loaded" with its objects still present BY NAME — and an empty
			// OID, because the parent chain resolves to nothing. Measured: the
			// tree came back with 0 roots, Translate(1.3.6.1.2.1.1.1.0)
			// answered "iso", and this function reported ok=true with 15
			// modules. The editor then showed a green "reloaded" toast over a
			// destroyed tree.
			h.Ok = false
			got := node.Oid.String()
			if got == "" {
				got = "nothing"
			}
			h.Failures = append(h.Failures,
				probe.want+" resolves to "+got+" instead of "+probe.oid+
					" — a module it depends on is missing or empty")
		}
	}
	return h
}
