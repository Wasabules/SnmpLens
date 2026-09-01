package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"embed"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"SnmpLens/pkg/events"
	"SnmpLens/pkg/mib"
	"SnmpLens/pkg/monitor"
	"SnmpLens/pkg/network"
	"SnmpLens/pkg/notify"
	"SnmpLens/pkg/secrets"
	"SnmpLens/pkg/service"
	"SnmpLens/pkg/snmp"
	"SnmpLens/pkg/storage"
	"SnmpLens/pkg/tray"
	"SnmpLens/pkg/updater"

	"time"

	"github.com/sleepinggenius2/gosmi"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct holds our services and application context.
type App struct {
	ctx              context.Context
	mibs             embed.FS
	persistentMibDir string
	mibService       *mib.Service
	snmpClient       *snmp.Client
	storage          *storage.Storage
	evaluator        *monitor.Evaluator
	dispatcher       *notify.Dispatcher
	secrets          secrets.Store
	updater          *updater.Service

	// Background-mode state. configDir is the SnmpLens directory, kept here
	// because several subsystems need it after startup.
	configDir  string
	serviceCfg service.Config
	trayIcons  trayIcons
	tray       *tray.Controller
	trayLive   bool
	trapsOn    bool
	scheduler  *monitor.Scheduler
}

// NewApp creates a new App application struct.
func NewApp(mibs embed.FS) *App {
	return &App{
		mibs:    mibs,
		updater: updater.NewService("Wasabules", "SnmpLens"),
	}
}

// startup is called when the app starts.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.snmpClient = snmp.NewClient(ctx)
	a.updater.SetContext(ctx)

	// 1. Determine/Create the persistent MIB directory
	configDir, err := os.UserConfigDir()
	if err != nil {
		log.Fatalf("Failed to get user config dir: %v", err)
	}
	a.persistentMibDir = filepath.Join(configDir, "SnmpLens", "mibs")

	if err := os.MkdirAll(a.persistentMibDir, 0755); err != nil {
		log.Fatalf("Failed to create persistent MIB directory: %v", err)
	}

	// Always make sure the bundled standard MIBs are present. They define the
	// base types (SNMPv2-SMI, SNMPv2-TC, ...) that nearly every other MIB
	// IMPORTS from, so without them user-supplied MIBs fail to load. Running
	// this on every startup (not just first run) self-heals an empty or
	// partially-populated MIB directory.
	a.ensureStandardMibs()

	// 2. Initialize gosmi and our MIB service
	gosmi.Init()
	log.Printf("Setting MIB search path to: %s", a.persistentMibDir)
	gosmi.AppendPath(a.persistentMibDir)
	a.mibService = mib.NewService(a.persistentMibDir)

	// 3. Initialize SQLite storage for monitoring data
	dbPath := filepath.Join(configDir, "SnmpLens", "monitoring.db")
	store, err := storage.Init(dbPath)
	if err != nil {
		log.Printf("WARNING: Failed to initialize monitoring storage: %v", err)
	} else {
		a.storage = store
		// Traps are journalled by the SNMP client itself, in its listener
		// goroutine, before anything is emitted to the webview.
		a.snmpClient.SetRecorder(events.RecorderFunc(a.recordEvent))
		a.initEvaluator()
		if store, err := secrets.Open(filepath.Join(configDir, "SnmpLens")); err != nil {
			log.Printf("WARNING: secret storage unavailable, sinks needing a credential will fail: %v", err)
		} else {
			a.secrets = store
			log.Printf("Secret storage backend: %s", store.Backend())
		}
		a.initDispatcher()
	}

	// 4. Load core MIBs
	coreMibs := []string{"SNMPv2-SMI", "SNMPv2-TC"}
	for _, mibName := range coreMibs {
		if _, err := gosmi.LoadModule(mibName); err != nil {
			log.Printf("ERROR: Failed to load core MIB '%s': %v.", mibName, err)
		} else {
			log.Printf("Successfully loaded core MIB: %s", mibName)
		}
	}

	// 5. The poll clock. It lives in Go so that monitoring, thresholds and the
	// notification routes keep working with the window closed — which is the
	// entire point of background mode.
	a.initScheduler()

	// 6. Tray, hide-on-close and the trap listener, in that order: the tray
	// decides whether closing the window may merely hide it.
	a.initBackgroundMode()

	// 7. Pick up what was running when we last exited.
	a.resumeActiveSessions()
}

// ensureStandardMibs copies any bundled standard MIB that is missing from the
// persistent directory. Files that already exist are left untouched so user
// edits and additions are preserved.
func (a *App) ensureStandardMibs() {
	mibFiles, err := a.mibs.ReadDir("mibs")
	if err != nil {
		log.Printf("ERROR: Failed to read embedded mibs directory: %v", err)
		return
	}

	var extracted []string
	for _, mibFile := range mibFiles {
		if mibFile.IsDir() {
			continue
		}
		fileName := mibFile.Name()
		destPath := filepath.Join(a.persistentMibDir, fileName)
		if _, err := os.Stat(destPath); err == nil {
			continue // already present
		}

		// embed.FS always uses forward slashes, even on Windows — never filepath.Join here.
		content, err := a.mibs.ReadFile("mibs/" + fileName)
		if err != nil {
			log.Printf("Warning: Failed to read embedded MIB file %s: %v", fileName, err)
			continue
		}
		if err := os.WriteFile(destPath, content, 0644); err != nil {
			log.Printf("Warning: Failed to write standard MIB file %s: %v", fileName, err)
			continue
		}
		extracted = append(extracted, fileName)
	}

	if len(extracted) > 0 {
		log.Printf("Extracted %d standard MIB(s) to %s: %v", len(extracted), a.persistentMibDir, extracted)
	}
}

// --- Frontend Exposed Methods ---

// GetPersistentMibDirectory returns the path where users can store their MIBs.
func (a *App) GetPersistentMibDirectory() string {
	return a.persistentMibDir
}

// LoadAllMibs loads all MIBs from the persistent directory.
func (a *App) LoadAllMibs() ([]*mib.Node, error) {
	return a.mibService.LoadAll()
}

// LoadEnabledMibs loads only the specified (enabled) MIBs from the persistent directory.
func (a *App) LoadEnabledMibs(enabledFiles []string) ([]*mib.Node, error) {
	if len(enabledFiles) == 0 {
		log.Println("No enabled MIBs specified, loading all MIBs")
		return a.mibService.LoadAll()
	}
	return a.mibService.LoadSpecific(enabledFiles)
}

// LoadMibsWithDiagnostics loads MIBs and returns both tree and per-file load diagnostics.
func (a *App) LoadMibsWithDiagnostics(enabledFiles []string) mib.MibLoadResponse {
	if len(enabledFiles) == 0 {
		log.Println("No enabled MIBs specified, loading all MIBs with diagnostics")
	}
	return a.mibService.LoadWithDiagnostics(enabledFiles)
}

// GetOidDetails translates a numeric OID into its MIB details.
func (a *App) GetOidDetails(oid string) mib.OidDetails {
	return a.mibService.Translate(oid)
}

// ResolveOid returns detailed MIB info for a single OID, including enum values.
func (a *App) ResolveOid(oid string) mib.OidInfo {
	return a.mibService.ResolveOid(oid)
}

// ResolveOids returns detailed MIB info for a batch of OIDs.
func (a *App) ResolveOids(oids []string) map[string]mib.OidInfo {
	return a.mibService.ResolveOids(oids)
}

// SnmpGet performs a concurrent SNMP GET operation.
func (a *App) SnmpGet(req snmp.SnmpRequest) []*snmp.BulkResult {
	return a.snmpClient.Get(req.Targets, req.OID, req.Community, req.Version, req.Port, req.Timeout, req.Retries, req.V3)
}

// SnmpSet performs a concurrent SNMP SET operation.
func (a *App) SnmpSet(req snmp.SetRequest) []*snmp.BulkResult {
	results := a.snmpClient.Set(req.Targets, req.OID, req.Community, req.Value, req.ValueType, req.Version, req.Port, req.Timeout, req.Retries, req.V3)
	a.auditFailedSets(req, results)
	return results
}

// auditFailedSets journals SETs that were refused.
//
// A SET is the one operation that CHANGES a device, so a refused one is worth
// keeping: it is either a permissions problem or someone attempting a change
// they should not. Only failures are recorded, and never the value that was
// being written — an audit trail must not become somewhere passwords or
// community strings accumulate in the clear.
func (a *App) auditFailedSets(req snmp.SetRequest, results []*snmp.BulkResult) {
	if !a.serviceCfg.AuditFailedSets || a.storage == nil {
		return
	}
	for _, r := range results {
		if r == nil || r.Error == "" {
			continue
		}
		a.journalEvent(events.Event{
			Category: events.CategoryOperation,
			Kind:     events.KindOperationFailed,
			Severity: "warning",
			Source:   r.Target,
			OID:      req.OID,
			TitleKey: "events.kind." + events.KindOperationFailed,
			Summary:  fmt.Sprintf("SET %s on %s was refused: %s", req.OID, r.Target, r.Error),
		})
	}
}

// journalEvent records an event, logging rather than propagating a failure:
// every caller is on a path whose real work already succeeded or already
// reported its own error.
func (a *App) journalEvent(e events.Event) {
	if err := a.recordEvent(e, ""); err != nil {
		log.Printf("WARNING: could not journal a %s event: %v", e.Kind, err)
	}
}

// SnmpGetNext performs a concurrent SNMP GETNEXT operation.
func (a *App) SnmpGetNext(req snmp.SnmpRequest) []*snmp.BulkResult {
	return a.snmpClient.GetNext(req.Targets, req.OID, req.Community, req.Version, req.Port, req.Timeout, req.Retries, req.V3)
}

// SnmpGetBulk performs a concurrent SNMP GETBULK operation.
func (a *App) SnmpGetBulk(req snmp.GetBulkRequest) []*snmp.BulkResult {
	return a.snmpClient.GetBulk(req.Targets, req.OID, req.Community, req.Version, req.Port, req.Timeout, req.Retries, req.NonRepeaters, req.MaxRepetitions, req.V3)
}

// SnmpWalk performs a concurrent SNMP WALK operation.
func (a *App) SnmpWalk(req snmp.SnmpRequest) []*snmp.BulkResult {
	return a.snmpClient.Walk(req.Targets, req.OID, req.Community, req.Version, req.Port, req.Timeout, req.Retries, req.V3)
}

// TestConnection tests the SNMP connection to a target by fetching sysDescr.0
func (a *App) TestConnection(req snmp.TestRequest) *snmp.BulkResult {
	results := a.snmpClient.Get([]string{req.Target}, "1.3.6.1.2.1.1.1.0", req.Community, req.Version, req.Port, req.Timeout, 1, req.V3)
	if len(results) > 0 {
		return results[0]
	}
	return &snmp.BulkResult{Target: req.Target, Error: "No response"}
}

// SnmpDiscover scans a CIDR range for SNMP-responsive devices.
func (a *App) SnmpDiscover(req snmp.DiscoverRequest) []snmp.DiscoveryResult {
	return a.snmpClient.Discover(req.CIDR, req.Community, req.Version, req.Port, req.Timeout, req.V3)
}

// SendTrap sends an SNMP trap to a target.
func (a *App) SendTrap(target string, port int, community, version, trapOid string, variables []snmp.TrapVariable) error {
	return a.snmpClient.SendTrap(target, port, community, version, trapOid, variables)
}

// StartTrapListener starts listening for SNMP traps.
func (a *App) StartTrapListener(req snmp.TrapListenerRequest) error {
	return a.snmpClient.StartTrapListener(req.Port, req.V3)
}

// StopTrapListener stops the active trap listener.
func (a *App) StopTrapListener() {
	a.snmpClient.StopTrapListener()
}

// ListMibFiles returns a list of MIB file names in the specified directory.
func (a *App) ListMibFiles(dirPath string) ([]string, error) {
	return mib.ListMibFiles(dirPath)
}

// MibImportResult holds per-file results for a MIB import operation.
type MibImportResult struct {
	FileName string `json:"fileName"`
	Success  bool   `json:"success"`
	Skipped  bool   `json:"skipped,omitempty"`
	Error    string `json:"error,omitempty"`
}

// ImportMibFiles copies the given files (or directories, recursively) into the
// persistent MIB directory. It returns per-file results so the frontend can
// report failures.
func (a *App) ImportMibFiles(filePaths []string) []MibImportResult {
	var results []MibImportResult

	for _, src := range filePaths {
		info, err := os.Stat(src)
		if err != nil {
			results = append(results, MibImportResult{
				FileName: filepath.Base(src),
				Success:  false,
				Error:    fmt.Sprintf("stat error: %v", err),
			})
			continue
		}

		if info.IsDir() {
			// Walk the directory recursively, importing regular files only
			filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return nil // skip dirs and errors silently
				}
				results = append(results, a.importSingleFile(path))
				return nil
			})
		} else {
			results = append(results, a.importSingleFile(src))
		}
	}
	return results
}

// importSingleFile copies one file into the persistent MIB directory.
// If an identical file already exists, it is skipped.
func (a *App) importSingleFile(src string) MibImportResult {
	name := filepath.Base(src)
	dst := filepath.Join(a.persistentMibDir, name)

	srcData, err := os.ReadFile(src)
	if err != nil {
		log.Printf("ImportMibFiles: failed to read %s: %v", src, err)
		return MibImportResult{FileName: name, Success: false, Error: fmt.Sprintf("read error: %v", err)}
	}

	// Check if the destination already has an identical file
	if dstData, err := os.ReadFile(dst); err == nil {
		if bytes.Equal(srcData, dstData) {
			log.Printf("ImportMibFiles: skipped %s (already exists)", name)
			return MibImportResult{FileName: name, Success: true, Skipped: true}
		}
	}

	if err := os.WriteFile(dst, srcData, 0644); err != nil {
		log.Printf("ImportMibFiles: failed to write %s: %v", dst, err)
		return MibImportResult{FileName: name, Success: false, Error: fmt.Sprintf("write error: %v", err)}
	}
	log.Printf("ImportMibFiles: imported %s", name)
	return MibImportResult{FileName: name, Success: true}
}

// BrowseDialog opens a directory picker dialog and returns the selected path.
func (a *App) BrowseDialog() (string, error) {
	selection, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select MIB Directory",
	})
	if err != nil {
		return "", err
	}
	return selection, nil
}

// --- Network Tools ---

// NetworkPing executes a ping command against a target.
func (a *App) NetworkPing(target string, count int) (network.PingResult, error) {
	return network.Ping(target, count)
}

// NetworkTraceroute executes a traceroute command, emitting progress events per hop.
func (a *App) NetworkTraceroute(target string) ([]network.TracerouteHop, error) {
	return network.Traceroute(a.ctx, target)
}

// --- SNMP Debug Methods ---

// SnmpSetDebug enables or disables SNMP packet debug logging.
func (a *App) SnmpSetDebug(enabled bool) {
	a.snmpClient.SetDebugMode(enabled)
}

// SnmpGetDebugLog returns the current SNMP debug log buffer.
func (a *App) SnmpGetDebugLog() []snmp.DebugEntry {
	return a.snmpClient.GetDebugLog()
}

// SnmpClearDebugLog clears the SNMP debug log buffer.
func (a *App) SnmpClearDebugLog() {
	a.snmpClient.ClearDebugLog()
}

// --- Auto-update Methods ---

// GetAppVersion returns the running application version (or "dev" for local builds).
func (a *App) GetAppVersion() string {
	return updater.Version
}

// CheckForUpdate queries GitHub Releases and reports whether a newer version exists.
func (a *App) CheckForUpdate() updater.UpdateInfo {
	info, err := a.updater.CheckForUpdate()
	if err != nil {
		log.Printf("Update check failed: %v", err)
	}
	return info
}

// DownloadAndApplyUpdate downloads, verifies and applies the pending update. On a
// successful self-apply the app relaunches and exits, so this may not return.
func (a *App) DownloadAndApplyUpdate() error {
	return a.updater.DownloadAndApply()
}

// OpenURL opens a URL in the user's default browser.
func (a *App) OpenURL(url string) {
	runtime.BrowserOpenURL(a.ctx, url)
}

// shutdown is called when the app is closing.
func (a *App) shutdown(ctx context.Context) {
	// Stop the poll clock first: its goroutines write samples through storage,
	// so letting them run into a closed database would log failures for work
	// that was actually fine.
	if a.scheduler != nil {
		a.scheduler.StopAll()
	}
	a.tray.Stop()
	// Stop the dispatcher BEFORE closing storage: it queries the outbox on its
	// own goroutine, and a drain racing a closed database would log spurious
	// failures against deliveries that are actually fine.
	if a.dispatcher != nil {
		a.dispatcher.Stop()
	}
	if a.storage != nil {
		if err := a.storage.Close(); err != nil {
			log.Printf("Error closing monitoring storage: %v", err)
		}
	}
}

// --- Monitoring Storage Methods ---

// MonitorSaveDataPoints queues data points for batch insertion.
func (a *App) MonitorSaveDataPoints(points []storage.DataPoint) {
	if a.storage == nil {
		return
	}
	a.storage.QueueDataPoints(points)
}

// MonitorLoadSessions returns all persisted monitoring sessions.
func (a *App) MonitorLoadSessions() ([]storage.Session, error) {
	if a.storage == nil {
		return []storage.Session{}, nil
	}
	return a.storage.ListSessions()
}

// MonitorLoadSessionData loads recent data points for a session.
func (a *App) MonitorLoadSessionData(sessionID string, limit int) ([]storage.DataPoint, error) {
	if a.storage == nil {
		return []storage.DataPoint{}, nil
	}
	return a.storage.QueryDataPoints(sessionID, "", "", limit)
}

// MonitorLoadHistoricalData loads data points for a specific time range.
func (a *App) MonitorLoadHistoricalData(sessionID, from, to string) ([]storage.DataPoint, error) {
	if a.storage == nil {
		return []storage.DataPoint{}, nil
	}
	return a.storage.QueryDataPoints(sessionID, from, to, 0)
}

// MonitorDeleteSession removes a session and all its data.
func (a *App) MonitorDeleteSession(sessionID string) error {
	if a.storage == nil {
		return fmt.Errorf("storage not initialized")
	}
	return a.storage.DeleteSession(sessionID)
}

// MonitorGetStats returns aggregate statistics for a monitoring session.
func (a *App) MonitorGetStats(sessionID string) (storage.SessionStats, error) {
	if a.storage == nil {
		return storage.SessionStats{}, nil
	}
	return a.storage.GetSessionStats(sessionID)
}

// MonitorCleanup deletes data older than the specified number of days.
func (a *App) MonitorCleanup(daysToKeep int) (int64, error) {
	if a.storage == nil {
		return 0, fmt.Errorf("storage not initialized")
	}
	return a.storage.Cleanup(time.Duration(daysToKeep) * 24 * time.Hour)
}

// MonitorLoadBuckets returns a session's data aggregated into fixed-width time
// buckets (bucketSec seconds), so long ranges render from a bounded number of
// points instead of every raw sample.
func (a *App) MonitorLoadBuckets(sessionID, from, to string, bucketSec int) ([]storage.Bucket, error) {
	if a.storage == nil {
		return []storage.Bucket{}, nil
	}
	return a.storage.QueryBuckets(sessionID, from, to, bucketSec)
}

// MonitorImportLegacyData imports data from localStorage migration.
func (a *App) MonitorImportLegacyData(sessions []storage.Session, points map[string][]storage.DataPoint) error {
	if a.storage == nil {
		return fmt.Errorf("storage not initialized")
	}
	return a.storage.ImportLocalStorageData(sessions, points)
}

// MonitorUpdateSession updates a session's active status.
func (a *App) MonitorUpdateSession(sessionID string, active bool, stoppedAt string) error {
	if a.storage == nil {
		return fmt.Errorf("storage not initialized")
	}
	return a.storage.UpdateSession(sessionID, active, stoppedAt)
}

// --- Query History Persistence ---

// SaveHistoryEntry persists a single query-history entry to SQLite.
func (a *App) SaveHistoryEntry(entry map[string]interface{}) error {
	if a.storage == nil {
		return fmt.Errorf("storage not initialized")
	}
	return a.storage.SaveHistory(entry)
}

// LoadHistory returns all persisted query-history entries, newest first.
func (a *App) LoadHistory() ([]map[string]interface{}, error) {
	if a.storage == nil {
		return []map[string]interface{}{}, nil
	}
	return a.storage.LoadHistory()
}

// DeleteHistoryEntry removes a single query-history entry by id.
func (a *App) DeleteHistoryEntry(id string) error {
	if a.storage == nil {
		return fmt.Errorf("storage not initialized")
	}
	return a.storage.DeleteHistoryEntry(id)
}

// DeleteHistoryEntries removes several query-history entries at once.
func (a *App) DeleteHistoryEntries(ids []string) error {
	if a.storage == nil {
		return fmt.Errorf("storage not initialized")
	}
	return a.storage.DeleteHistoryEntries(ids)
}

// ClearHistory removes every query-history entry.
func (a *App) ClearHistory() error {
	if a.storage == nil {
		return fmt.Errorf("storage not initialized")
	}
	return a.storage.ClearHistory()
}

// CountHistory returns the number of persisted query-history entries.
func (a *App) CountHistory() (int, error) {
	if a.storage == nil {
		return 0, nil
	}
	return a.storage.CountHistory()
}

// ImportHistoryEntries bulk-inserts query-history entries (localStorage
// migration and JSON import from the UI).
func (a *App) ImportHistoryEntries(entries []map[string]interface{}) error {
	if a.storage == nil {
		return fmt.Errorf("storage not initialized")
	}
	return a.storage.ImportHistoryEntries(entries)
}

// --- Event journal ---

// recordEvent persists an event and tells any open window about it. It is the
// single Recorder implementation handed to every producer.
//
// Persist first, notify second: the runtime event is a UI hint and is dropped
// when no window is listening, whereas the row is the record of what happened.
func (a *App) recordEvent(e events.Event, payload string) error {
	if a.storage == nil {
		return fmt.Errorf("storage not initialized")
	}
	saved, err := a.storage.InsertEvent(e, payload)
	if err != nil {
		return err
	}

	// Route in the same breath as the insert. Queuing is what makes delivery
	// durable; actually sending happens on the dispatcher goroutine, so a slow
	// SMTP relay can never stall a trap listener.
	a.routeEvent(saved)

	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "event:new", saved)
	}
	return nil
}

// routeEvent matches an event against the rules and queues one delivery per
// selected sink.
func (a *App) routeEvent(e events.Event) {
	routes, err := a.storage.ListRoutes()
	if err != nil || len(routes) == 0 {
		return
	}
	sinkIDs := notify.Select(routes, e, time.Now())
	if len(sinkIDs) == 0 {
		return
	}

	// Redaction is decided per sink, so group by the rendering it needs rather
	// than rendering once for everyone.
	sinks, err := a.storage.ListSinks()
	if err != nil {
		return
	}
	redactBySink := map[string]bool{}
	for _, s := range sinks {
		redactBySink[s.ID] = s.Redact
	}

	plainSubject, plainBody := notify.Render(e, false)
	redactedSubject, redactedBody := notify.Render(e, true)

	var plain, redacted []string
	for _, id := range sinkIDs {
		if redactBySink[id] {
			redacted = append(redacted, id)
		} else {
			plain = append(plain, id)
		}
	}

	if len(plain) > 0 {
		if err := a.storage.EnqueueDeliveries(e, plain, plainSubject, plainBody); err != nil {
			log.Printf("notify: could not queue deliveries: %v", err)
		}
	}
	if len(redacted) > 0 {
		if err := a.storage.EnqueueDeliveries(e, redacted, redactedSubject, redactedBody); err != nil {
			log.Printf("notify: could not queue redacted deliveries: %v", err)
		}
	}

	if a.dispatcher != nil {
		a.dispatcher.Wake()
	}
}

// EventsQuery returns one page of the journal, newest first.
func (a *App) EventsQuery(filter events.Filter) (events.Page, error) {
	if a.storage == nil {
		return events.Page{Items: []events.Event{}}, nil
	}
	return a.storage.QueryEvents(filter)
}

// EventsPayload returns an event's bulk detail, loaded only when opened.
func (a *App) EventsPayload(id string) (string, error) {
	if a.storage == nil {
		return "", nil
	}
	return a.storage.EventPayload(id)
}

// EventsCounts feeds the unacknowledged badge without fetching rows.
func (a *App) EventsCounts() (events.Counts, error) {
	if a.storage == nil {
		return events.Counts{UnackedBySev: map[string]int{}, UnackedByCatego: map[string]int{}}, nil
	}
	return a.storage.EventCounts()
}

// EventsAck acknowledges specific events.
func (a *App) EventsAck(ids []string) error {
	if a.storage == nil {
		return fmt.Errorf("storage not initialized")
	}
	return a.storage.AckEvents(ids)
}

// EventsAckAll acknowledges everything matching a filter.
func (a *App) EventsAckAll(filter events.Filter) error {
	if a.storage == nil {
		return fmt.Errorf("storage not initialized")
	}
	return a.storage.AckAllEvents(filter)
}

// EventsDelete removes events and their payloads.
func (a *App) EventsDelete(ids []string) error {
	if a.storage == nil {
		return fmt.Errorf("storage not initialized")
	}
	return a.storage.DeleteEvents(ids)
}

// EventsClear empties the journal.
func (a *App) EventsClear() error {
	if a.storage == nil {
		return fmt.Errorf("storage not initialized")
	}
	return a.storage.ClearEvents()
}

// --- Threshold and reachability evaluation ---

// generateEventCorrID returns the id that ties a resolution event back to the
// opening event of the same incident.
func generateEventCorrID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("corr-%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("corr-%x", b)
}

// initEvaluator wires the breach evaluator to the journal and restores any
// episode that was still open when the app last closed. Without the restore, a
// restart would re-announce an incident that had already been reported.
func (a *App) initEvaluator() {
	ev := monitor.NewEvaluator()
	ev.Record = a.recordEvent
	ev.NewCorrID = func() string { return generateEventCorrID() }
	ev.SaveEpi = func(r monitor.EpisodeRecord) error {
		return a.storage.SaveEpisode(storage.Episode{
			DedupKey:  r.DedupKey,
			Kind:      r.Kind,
			SessionID: r.SessionID,
			Target:    r.Target,
			OID:       r.OID,
			FirstSeen: r.FirstSeen,
			FiredAt:   r.FiredAt,
			CorrID:    r.CorrID,
		})
	}
	ev.DeleteEpi = a.storage.DeleteEpisode

	if saved, err := a.storage.LoadEpisodes(); err == nil {
		records := make([]monitor.EpisodeRecord, 0, len(saved))
		for _, e := range saved {
			records = append(records, monitor.EpisodeRecord{
				DedupKey:  e.DedupKey,
				Kind:      e.Kind,
				SessionID: e.SessionID,
				Target:    e.Target,
				OID:       e.OID,
				FirstSeen: e.FirstSeen,
				FiredAt:   e.FiredAt,
				CorrID:    e.CorrID,
			})
		}
		ev.Restore(records)
	} else {
		log.Printf("WARNING: failed to restore open incidents: %v", err)
	}

	a.evaluator = ev
}

// --- Notification routing ---

// initDispatcher starts the outbox drain loop.
func (a *App) initDispatcher() {
	resolve := func(sinkID string) (notify.Sink, bool) {
		sinks, err := a.storage.ListSinks()
		if err != nil {
			return nil, false
		}
		for _, cfg := range sinks {
			if cfg.ID != sinkID {
				continue
			}
			if !cfg.Enabled {
				return nil, false
			}
			return notify.Build(cfg, a.sinkSecret(cfg.ID))
		}
		return nil, false
	}

	d := notify.NewDispatcher(a.storage, resolve, 15*time.Second)
	d.OnResult = func(q notify.Queued, err error, dead bool) {
		if err == nil || !dead {
			return
		}
		// A dead letter is the only signal an operator gets that a
		// notification never arrived, so it becomes an event of its own.
		a.recordEvent(events.Event{
			Category: events.CategorySystem,
			Kind:     events.KindSystemSinkDeadLetter,
			Severity: events.SevMajor.String(),
			TitleKey: "events.kind." + events.KindSystemSinkDeadLetter,
			Params: map[string]any{
				"sink":     q.SinkID,
				"attempts": q.Attempts + 1,
				"error":    err.Error(),
			},
			Summary: "Delivery to " + q.SinkID + " given up: " + err.Error(),
		}, "")
	}
	d.Start()
	a.dispatcher = d
}

// sinkSecret returns a sink's credential from secure storage. Secrets are kept
// out of the sink config so it can be exported, logged or read from the
// database without leaking one.
func (a *App) sinkSecret(sinkID string) string {
	if a.secrets == nil || sinkID == "" {
		return ""
	}
	v, err := a.secrets.Get(secrets.SinkRef(sinkID))
	if err != nil {
		return ""
	}
	return v
}

// SecretsBackend names the protection actually in use on this machine, so the
// settings UI can state what is true here rather than what was hoped for.
func (a *App) SecretsBackend() string {
	if a.secrets == nil {
		return "unavailable"
	}
	return a.secrets.Backend()
}

// NotifyListSinks returns the configured destinations. Credentials are never
// included — only whether one is on file.
func (a *App) NotifyListSinks() ([]notify.SinkConfig, error) {
	if a.storage == nil {
		return []notify.SinkConfig{}, nil
	}
	sinks, err := a.storage.ListSinks()
	if err != nil {
		return sinks, err
	}
	for i := range sinks {
		sinks[i].Secret = ""
		sinks[i].HasSecret = a.sinkSecret(sinks[i].ID) != ""
	}
	return sinks, nil
}

// NotifySaveSink creates or updates a destination.
//
// The credential is split off and stored separately: SinkConfig.Secret is
// write-only transport, and the config that reaches the database must never
// carry it.
func (a *App) NotifySaveSink(cfg notify.SinkConfig) (notify.SinkConfig, error) {
	if a.storage == nil {
		return cfg, fmt.Errorf("storage not initialized")
	}

	incoming := cfg.Secret
	cfg.Secret = ""

	saved, err := a.storage.SaveSink(cfg)
	if err != nil {
		return saved, err
	}

	// An empty field means "leave the stored credential alone", not "clear it":
	// the UI never receives the current value, so it cannot echo it back.
	if incoming != "" && a.secrets != nil {
		if err := a.secrets.Set(secrets.SinkRef(saved.ID), incoming); err != nil {
			return saved, fmt.Errorf("saved the destination but could not store its credential: %w", err)
		}
	}
	saved.Secret = ""
	saved.HasSecret = a.sinkSecret(saved.ID) != ""
	return saved, nil
}

// NotifyClearSinkSecret removes a destination's stored credential.
func (a *App) NotifyClearSinkSecret(sinkID string) error {
	if a.secrets == nil {
		return fmt.Errorf("secret storage unavailable")
	}
	return a.secrets.Delete(secrets.SinkRef(sinkID))
}

// NotifyDeleteSink removes a destination.
func (a *App) NotifyDeleteSink(id string) error {
	if a.storage == nil {
		return fmt.Errorf("storage not initialized")
	}
	return a.storage.DeleteSink(id)
}

// NotifyTestSink sends a synthetic event so the operator can verify a
// destination without waiting for a real incident.
func (a *App) NotifyTestSink(cfg notify.SinkConfig) error {
	sink, ok := notify.Build(cfg, cfg.Secret)
	if !ok {
		return fmt.Errorf("unknown sink kind %q", cfg.Kind)
	}
	if cfg.Secret == "" {
		cfg.Secret = a.sinkSecret(cfg.ID) // testing a saved sink must use its saved credential
	}
	sample := events.Event{
		ID:       "test-" + generateEventCorrID(),
		Ts:       time.Now().UTC().Format(time.RFC3339),
		Category: events.CategorySystem,
		Kind:     events.KindSystemListenerStarted,
		Severity: events.SevInfo.String(),
		State:    events.StateOneshot,
		Source:   "SnmpLens",
		TitleKey: "events.kind." + events.KindSystemListenerStarted,
		Summary:  "SnmpLens test notification",
	}
	subject, body := notify.Render(sample, cfg.Redact)
	return sink.Send(sample, subject, body)
}

// NotifyListRoutes returns the routing rules.
func (a *App) NotifyListRoutes() ([]notify.Route, error) {
	if a.storage == nil {
		return []notify.Route{}, nil
	}
	return a.storage.ListRoutes()
}

// NotifySaveRoute creates or updates a routing rule.
func (a *App) NotifySaveRoute(r notify.Route) (notify.Route, error) {
	if a.storage == nil {
		return r, fmt.Errorf("storage not initialized")
	}
	return a.storage.SaveRoute(r)
}

// NotifyDeleteRoute removes a routing rule.
func (a *App) NotifyDeleteRoute(id string) error {
	if a.storage == nil {
		return fmt.Errorf("storage not initialized")
	}
	return a.storage.DeleteRoute(id)
}

// NotifyListDeliveries returns the delivery log. state may be "", "pending",
// "sent" or "dead".
func (a *App) NotifyListDeliveries(state string, limit int) ([]storage.Delivery, error) {
	if a.storage == nil {
		return []storage.Delivery{}, nil
	}
	return a.storage.ListDeliveries(state, limit)
}

// NotifyRetryDelivery puts a dead letter back in the queue.
func (a *App) NotifyRetryDelivery(id int64) error {
	if a.storage == nil {
		return fmt.Errorf("storage not initialized")
	}
	if err := a.storage.RetryDelivery(id); err != nil {
		return err
	}
	if a.dispatcher != nil {
		a.dispatcher.Wake()
	}
	return nil
}
