package main

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"SnmpLens/pkg/mib"
	"SnmpLens/pkg/network"
	"SnmpLens/pkg/snmp"
	"SnmpLens/pkg/storage"
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
	updater          *updater.Service
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
	return a.snmpClient.Set(req.Targets, req.OID, req.Community, req.Value, req.ValueType, req.Version, req.Port, req.Timeout, req.Retries, req.V3)
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
	if a.storage != nil {
		if err := a.storage.Close(); err != nil {
			log.Printf("Error closing monitoring storage: %v", err)
		}
	}
}

// --- Monitoring Storage Methods ---

// MonitorCreateSession creates a persistent monitoring session and returns its UUID.
func (a *App) MonitorCreateSession(oid string, targets []string, intervalMs int, snmpVersion string, thresholds *storage.Thresholds) (string, error) {
	if a.storage == nil {
		return "", fmt.Errorf("storage not initialized")
	}
	return a.storage.CreateSession(oid, targets, intervalMs, snmpVersion, time.Now().UTC().Format(time.RFC3339), thresholds)
}

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
