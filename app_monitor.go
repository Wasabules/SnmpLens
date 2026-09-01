package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"SnmpLens/pkg/events"
	"SnmpLens/pkg/monitor"
	"SnmpLens/pkg/secrets"
	"SnmpLens/pkg/snmp"
	"SnmpLens/pkg/storage"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// MonitorConnection is the connection half of a session, as the renderer sends
// it. The credential fields are write-only: they are split off into pkg/secrets
// and never travel back out.
type MonitorConnection struct {
	Port       int `json:"port"`
	TimeoutSec int `json:"timeoutSec"`
	Retries    int `json:"retries"`
	// Community and the v3 passphrases inside V3 are moved to the secret store
	// on the way in. See sessionCreds.
	Community string        `json:"community"`
	V3        snmp.V3Params `json:"v3"`
}

// sessionCreds is the blob held in the secret store for one session.
type sessionCreds struct {
	Community string `json:"community,omitempty"`
	AuthPass  string `json:"authPass,omitempty"`
	PrivPass  string `json:"privPass,omitempty"`
}

// split separates what may be stored in the clear from what may not.
func (c MonitorConnection) split() (*storage.SessionConn, sessionCreds) {
	return &storage.SessionConn{
			Port:          c.Port,
			TimeoutSec:    c.TimeoutSec,
			Retries:       c.Retries,
			V3User:        c.V3.User,
			V3AuthProto:   c.V3.AuthProto,
			V3PrivProto:   c.V3.PrivProto,
			V3SecLevel:    c.V3.SecLevel,
			V3ContextName: c.V3.ContextName,
		}, sessionCreds{
			Community: c.Community,
			AuthPass:  c.V3.AuthPass,
			PrivPass:  c.V3.PrivPass,
		}
}

// MonitorCreateSession creates a persistent monitoring session and returns its
// UUID.
//
// The connection is persisted alongside it because the poll clock now runs in
// Go: a session has to be resumable with no window open, and after a restart
// there is no renderer left to ask for the settings.
func (a *App) MonitorCreateSession(oid string, targets []string, intervalMs int, snmpVersion string, thresholds map[string]*storage.Thresholds, name string, conn MonitorConnection) (string, error) {
	if a.storage == nil {
		return "", fmt.Errorf("storage not initialized")
	}
	sessConn, creds := conn.split()
	id, err := a.storage.CreateSession(name, oid, targets, intervalMs, snmpVersion, time.Now().UTC().Format(time.RFC3339), thresholds, sessConn)
	if err != nil {
		return "", err
	}
	a.saveSessionCreds(id, creds)
	return id, nil
}

// saveSessionCreds stores the credentials outside the database.
func (a *App) saveSessionCreds(sessionID string, creds sessionCreds) {
	if a.secrets == nil {
		return
	}
	if creds.Community == "" && creds.AuthPass == "" && creds.PrivPass == "" {
		_ = a.secrets.Delete(secrets.SessionRef(sessionID))
		return
	}
	raw, err := json.Marshal(creds)
	if err != nil {
		return
	}
	if err := a.secrets.Set(secrets.SessionRef(sessionID), string(raw)); err != nil {
		log.Printf("WARNING: could not store the credentials of session %s: %v", sessionID, err)
	}
}

// loadSessionCreds reads them back. A session with none simply polls with what
// the profile alone provides, which is correct for a v2c agent that accepts an
// empty community.
func (a *App) loadSessionCreds(sessionID string) sessionCreds {
	var creds sessionCreds
	if a.secrets == nil {
		return creds
	}
	raw, err := a.secrets.Get(secrets.SessionRef(sessionID))
	if err != nil {
		return creds
	}
	_ = json.Unmarshal([]byte(raw), &creds)
	return creds
}

// initScheduler wires the poll clock to storage, the threshold engine and the
// renderer.
func (a *App) initScheduler() {
	s := monitor.NewScheduler()

	s.Persist = func(points []monitor.Point) {
		if a.storage == nil {
			return
		}
		dps := make([]storage.DataPoint, 0, len(points))
		for _, p := range points {
			dps = append(dps, storage.DataPoint{
				SessionID: p.SessionID, Target: p.Target, Timestamp: p.Timestamp,
				Value: p.Value, Delta: p.Delta, Rate: p.Rate,
				ResponseTimeMs: p.ResponseTimeMs, Error: p.Error,
				SnmpType: p.SnmpType, OID: p.OID,
			})
		}
		a.storage.QueueDataPoints(dps)
	}

	s.Evaluate = func(sessionID, name string, samples []monitor.Sample, th map[string]*monitor.Threshold) error {
		if a.evaluator == nil {
			return nil
		}
		return a.evaluator.Ingest(sessionID, name, samples, th)
	}

	// With no window open this does nothing, and nothing else changes: by the
	// time it runs the samples are already stored and already evaluated.
	s.Emit = func(sessionID string, points []monitor.Point) {
		if a.ctx == nil {
			return
		}
		runtime.EventsEmit(a.ctx, "monitor:samples", map[string]interface{}{
			"sessionId": sessionID,
			"points":    points,
		})
	}

	s.OnStateChange = a.refreshTrayStatus
	a.scheduler = s
}

// buildFetch closes over the connection so the scheduler never sees a
// credential.
func (a *App) buildFetch(sess storage.Session) monitor.FetchFunc {
	conn := sess.Conn
	if conn == nil {
		conn = &storage.SessionConn{}
	}
	creds := a.loadSessionCreds(sess.ID)

	v3 := snmp.V3Params{
		User:        conn.V3User,
		AuthProto:   conn.V3AuthProto,
		PrivProto:   conn.V3PrivProto,
		SecLevel:    conn.V3SecLevel,
		ContextName: conn.V3ContextName,
		AuthPass:    creds.AuthPass,
		PrivPass:    creds.PrivPass,
	}
	version := sess.SnmpVersion
	community := creds.Community
	port, timeout, retries := conn.Port, conn.TimeoutSec, conn.Retries

	return func(_ context.Context, oid string, targets []string) []monitor.Reading {
		results := a.snmpClient.Get(targets, oid, community, version, port, timeout, retries, v3)
		readings := make([]monitor.Reading, 0, len(results))
		for _, r := range results {
			readings = append(readings, toReading(r))
		}
		return readings
	}
}

// nonNumericTypes are SNMP types that cannot be plotted. Such a value is stored
// with its type but no number, so the chart shows a gap rather than a
// meaningless zero.
var nonNumericTypes = []string{"octetstring", "objectidentifier", "ipaddress", "opaque", "nsapaddress", "bitstring"}

// errorSentinels are the "no such thing" answers an agent returns in place of a
// value. Treated as readings they would look like real data.
var errorSentinels = map[string]bool{"noSuchObject": true, "noSuchInstance": true, "endOfMibView": true}

// toReading converts one SNMP answer into a scheduler reading.
func toReading(r *snmp.BulkResult) monitor.Reading {
	out := monitor.Reading{
		Target:         r.Target,
		Error:          r.Error,
		ResponseTimeMs: int(r.ResponseTimeMs),
	}
	if r.Result == nil {
		return out
	}
	out.SnmpType = r.Result.Type

	if s, ok := r.Result.Value.(string); ok && errorSentinels[s] {
		out.Error = s
		return out
	}
	lower := strings.ToLower(out.SnmpType)
	for _, t := range nonNumericTypes {
		if strings.Contains(lower, t) {
			return out
		}
	}
	if v, ok := numericValue(r.Result.Value); ok {
		out.Value = &v
	}
	return out
}

// numericValue coerces the interface{} that crosses from gosnmp into a float64.
// formatSnmpValue has already narrowed *big.Int to int64/uint64, so the integer
// cases below are the whole surface.
func numericValue(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	default:
		return 0, false
	}
}

// toMonitorThresholds converts the stored bands into the evaluator's shape.
func toMonitorThresholds(in map[string]*storage.Thresholds) map[string]*monitor.Threshold {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]*monitor.Threshold, len(in))
	for oid, t := range in {
		if t == nil {
			continue
		}
		out[oid] = &monitor.Threshold{
			Min: t.Min, Max: t.Max,
			ForSeconds:   t.ForSeconds,
			AlertEnabled: t.AlertEnabled,
		}
	}
	return out
}

// specFor turns a stored session into something the scheduler can run.
func (a *App) specFor(sess storage.Session) monitor.SessionSpec {
	oids := []string{}
	for _, o := range strings.Split(sess.OID, ",") {
		if o = strings.TrimSpace(o); o != "" {
			oids = append(oids, o)
		}
	}
	return monitor.SessionSpec{
		ID:         sess.ID,
		Name:       sess.Name,
		OIDs:       oids,
		Targets:    sess.Targets,
		Interval:   time.Duration(sess.IntervalMs) * time.Millisecond,
		Thresholds: toMonitorThresholds(sess.Thresholds),
		Fetch:      a.buildFetch(sess),
	}
}

// MonitorStart begins polling a stored session in Go.
func (a *App) MonitorStart(sessionID string) error {
	if a.storage == nil || a.scheduler == nil {
		return fmt.Errorf("storage not initialized")
	}
	sess, err := a.findSession(sessionID)
	if err != nil {
		return err
	}
	a.scheduler.Start(a.specFor(sess))
	return a.storage.UpdateSession(sessionID, true, "")
}

// MonitorStop ends the Go-side poll loop for a session.
func (a *App) MonitorStop(sessionID string) error {
	if a.scheduler == nil {
		return nil
	}
	a.scheduler.Stop(sessionID)
	if a.storage == nil {
		return nil
	}
	return a.storage.UpdateSession(sessionID, false, time.Now().UTC().Format(time.RFC3339))
}

// MonitorRunning lists the sessions the Go scheduler is polling, so a window
// that has just opened can reconcile with what happened without it.
func (a *App) MonitorRunning() []string {
	if a.scheduler == nil {
		return []string{}
	}
	return a.scheduler.Running()
}

// MonitorUpdateConnection replaces a session's connection settings, for when a
// community or passphrase changes after the session was created.
func (a *App) MonitorUpdateConnection(sessionID string, conn MonitorConnection) error {
	if a.storage == nil {
		return fmt.Errorf("storage not initialized")
	}
	sessConn, creds := conn.split()
	if err := a.storage.UpdateSessionConn(sessionID, sessConn); err != nil {
		return err
	}
	a.saveSessionCreds(sessionID, creds)
	// A running session holds its old closure, so restart it here rather than
	// letting the change appear to have been ignored.
	if a.scheduler != nil && a.scheduler.IsRunning(sessionID) {
		a.scheduler.Stop(sessionID)
		if sess, err := a.findSession(sessionID); err == nil {
			a.scheduler.Start(a.specFor(sess))
		}
	}
	return nil
}

func (a *App) findSession(sessionID string) (storage.Session, error) {
	sessions, err := a.storage.ListSessions()
	if err != nil {
		return storage.Session{}, err
	}
	for _, s := range sessions {
		if s.ID == sessionID {
			return s, nil
		}
	}
	return storage.Session{}, fmt.Errorf("unknown monitoring session %q", sessionID)
}

// resumeActiveSessions restarts what was running when the app last exited.
//
// This is the other half of service mode: without it a machine that reboots
// overnight comes back with every monitoring silently stopped, and the first
// anyone hears of it is the alert that never arrived.
func (a *App) resumeActiveSessions() {
	if a.storage == nil || a.scheduler == nil || !a.serviceCfg.AutoResumeMonitors {
		return
	}
	sessions, err := a.storage.ListSessions()
	if err != nil {
		log.Printf("WARNING: could not list sessions to resume: %v", err)
		return
	}
	resumed := 0
	for _, sess := range sessions {
		// A session stored before the connection was persisted cannot be
		// resumed headlessly: there is nothing to authenticate with.
		if !sess.Active || sess.Conn == nil {
			continue
		}
		a.scheduler.Start(a.specFor(sess))
		resumed++
	}
	if resumed > 0 {
		log.Printf("resumed %d monitoring session(s)", resumed)
		a.recordSystemEvent(events.KindSystemInfo, "info",
			fmt.Sprintf("Resumed %d monitoring session(s) after startup.", resumed))
	}
}
