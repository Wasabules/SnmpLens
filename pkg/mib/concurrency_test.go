package mib

import (
	"os"
	"sync"
	"testing"

	"github.com/sleepinggenius2/gosmi"
)

// Two READERS at once.
//
// gosmi has no read-only operations: internal.(*Object).GetSmiNode is a getter
// that memoises, writing x.Oid and x.OidLen on first call. Two goroutines
// holding a READ lock and resolving the same OID therefore write the same
// fields at once — which is why gosmiMu is an exclusive Mutex and not an
// RWMutex. Run this with -race or it proves nothing.
func TestConcurrentReaders(t *testing.T) {
	if _, err := os.Stat("../../mibs"); err != nil {
		t.Skip("no corpus")
	}
	gosmi.Exit()
	gosmi.Init()
	gosmi.SetPath("../../mibs")
	s := NewService("../../mibs")
	if _, err := s.LoadAll(); err != nil {
		t.Skip(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for k := 0; k < 40; k++ {
				s.Translate("1.3.6.1.2.1.1.1.0")
				s.ResolveOids([]string{"1.3.6.1.2.1.2.2.1.10", "1.3.6.1.2.1.1.3.0"})
				if info, err := s.Table("1.3.6.1.2.1.2.2"); err == nil {
					s.DecodeIndexes(info.Oid, []string{"1", "2", "3"})
					s.EncodeIndex(info.Oid, []string{"7"})
				}
				Symbols()
			}
		}(i)
	}
	wg.Wait()
}

// A reader running WHILE a rebuild tears the world down. This is the editor's
// ordinary case: saving a MIB rebuilds while the other tabs resolve OIDs.
func TestReadDuringRebuild(t *testing.T) {
	if _, err := os.Stat("../../mibs"); err != nil {
		t.Skip("no corpus")
	}
	gosmi.Exit()
	gosmi.Init()
	gosmi.SetPath("../../mibs")
	s := NewService("../../mibs")
	files, err := ListMibFiles("../../mibs")
	if err != nil {
		t.Skip(err)
	}
	if _, err := s.LoadAll(); err != nil {
		t.Skip(err)
	}

	stop := make(chan struct{})
	var wrong int
	var mu sync.Mutex

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				// sysDescr must resolve to sysDescr or fail — never to
				// something else.
				if got := s.Translate("1.3.6.1.2.1.1.1.0").Name; got != "sysDescr" {
					mu.Lock()
					wrong++
					mu.Unlock()
				}
			}
		}()
	}

	for k := 0; k < 3; k++ {
		s.Rebuild(files)
	}
	close(stop)
	wg.Wait()

	if wrong > 0 {
		t.Errorf("%d reads answered with something other than sysDescr during a rebuild", wrong)
	}
}

// Two rebuilds at once. Each one's Exit/Init destroys what the other just
// loaded, and the health probe then reports a failure that is not real — which
// app_mibeditor.go records as a "major" system event and routes to every
// configured syslog, webhook and mail sink.
func TestConcurrentRebuilds(t *testing.T) {
	if _, err := os.Stat("../../mibs"); err != nil {
		t.Skip("no corpus")
	}
	gosmi.Exit()
	gosmi.Init()
	gosmi.SetPath("../../mibs")
	s := NewService("../../mibs")
	files, err := ListMibFiles("../../mibs")
	if err != nil {
		t.Skip(err)
	}

	var mu sync.Mutex
	var failures []string

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := s.Rebuild(files)
			if !r.Health.Ok {
				mu.Lock()
				failures = append(failures, r.Health.Failures...)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if len(failures) > 0 {
		t.Errorf("concurrent rebuilds reported %d health failures on an unmodified corpus: %v",
			len(failures), failures)
	}
}
