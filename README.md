<p align="center">
  <img src="SnmpLens.png" alt="SnmpLens" width="128" />
</p>

<h1 align="center">SnmpLens</h1>

<p align="center">
  A modern, cross-platform SNMP MIB browser, MIB editor and monitoring desktop application —
  dashboards drawn from shareable presets, a trap receiver, and alerts routed to syslog, a webhook or email.
  <br />
  One native binary, built with <a href="https://wails.io/">Wails</a> (Go + Svelte).
</p>

<p align="center">
  <a href="https://snmplens.com/"><strong>Website</strong></a>
  &nbsp;·&nbsp;
  <a href="https://snmplens.com/demo.html"><strong>Try it in your browser</strong></a>
  &nbsp;·&nbsp;
  <a href="https://snmplens.com/documentation.html">Documentation</a>
  &nbsp;·&nbsp;
  <a href="https://snmplens.com/download.html">Download</a>
</p>

<p align="center">
  <a href="https://github.com/Wasabules/SnmpLens/actions/workflows/ci.yml"><img src="https://github.com/Wasabules/SnmpLens/actions/workflows/ci.yml/badge.svg" alt="CI" /></a>
  <a href="https://github.com/Wasabules/SnmpLens/releases"><img src="https://img.shields.io/github/v/release/Wasabules/SnmpLens?include_prereleases" alt="Release" /></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="License" /></a>
  <img src="https://img.shields.io/badge/platform-Windows%20%7C%20macOS%20%7C%20Linux-brightgreen" alt="Platform" />
  <img src="https://img.shields.io/badge/SNMPv1%20%7C%20v2c%20%7C%20v3-supported-orange" alt="SNMP Versions" />
</p>

---

## Screenshots

<p align="center">
  <img src="docs/assets/img/operations-dark-1200.webp" alt="SNMP Operations" width="90%" />
  <br /><em>SNMP Operations — a walk of <code>ifTable</code> pivoted into columns and split by INDEX</em>
</p>

<table>
  <tr>
    <td width="50%"><img src="docs/assets/img/dashboard-preset-dark-1200.webp" alt="Dashboard drawn from a preset" width="100%" /><br /><em>A dashboard drawn from a preset: tiles, a chart, a wall of port states and a map of the rack</em></td>
    <td width="50%"><img src="docs/assets/img/dashboard-group-dark-1200.webp" alt="One preset across two switches" width="100%" /><br /><em>One preset drawn across two switches of different sizes</em></td>
  </tr>
  <tr>
    <td width="50%"><img src="docs/assets/img/mib-browser-dark-1200.webp" alt="MIB Browser" width="100%" /><br /><em>The MIB tree, searchable across name, OID, description and syntax</em></td>
    <td width="50%"><img src="docs/assets/img/mib-editor-dark-1200.webp" alt="MIB Editor" width="100%" /><br /><em>The MIB editor, reporting an unknown type and a duplicated OID by line and column</em></td>
  </tr>
  <tr>
    <td width="50%"><img src="docs/assets/img/monitor-charts-dark-1200.webp" alt="Monitoring" width="100%" /><br /><em>Polling sessions charted as values, deltas, rates or latency</em></td>
    <td width="50%"><img src="docs/assets/img/events-journal-dark-1200.webp" alt="Event journal" width="100%" /><br /><em>The event journal — traps, thresholds, reachability, and deliveries that failed</em></td>
  </tr>
  <tr>
    <td width="50%"><img src="docs/assets/img/settings-notifications-dark-1200.webp" alt="Alert routing" width="100%" /><br /><em>Alert routing: destinations, rules and the delivery log</em></td>
    <td width="50%"><img src="docs/assets/img/trap-listener-dark-1200.webp" alt="Trap Listener" width="100%" /><br /><em>Received traps, with the trap OID resolved through your MIBs</em></td>
  </tr>
  <tr>
    <td width="50%"><img src="docs/assets/img/network-discovery-dark-1200.webp" alt="Network Discovery" width="100%" /><br /><em>CIDR discovery, then ping and traceroute without elevated privileges</em></td>
    <td width="50%"><img src="docs/assets/img/history-diff-dark-1200.webp" alt="History and Diff" width="100%" /><br /><em>Two walks of the same device, diffed</em></td>
  </tr>
  <tr>
    <td width="50%"><img src="docs/assets/img/anonymous-mode-dark-1200.webp" alt="Anonymous Mode" width="100%" /><br /><em>Anonymous Mode — addresses and credentials masked for safe screenshots</em></td>
    <td width="50%"><img src="docs/assets/img/operations-light-1200.webp" alt="Light Theme" width="100%" /><br /><em>Light theme</em></td>
  </tr>
</table>

---

## Features

### SNMP Operations

- **GET / SET / GETNEXT / GETBULK / WALK** with concurrent multi-target execution
- **Conceptual tables** — a walk is pivoted into columns and split by INDEX following RFC 2578 §7.7, so `tcpConnTable` reads as four index fields instead of one opaque sub-OID, name-keyed tables show names, and rows sort numerically. Sortable, filterable, exportable to CSV
- **Row creation and deletion** on tables that define a `RowStatus` column, written as one atomic SET so the agent never sees a half-built row
- **Smart Value Formatting** — TimeTicks as a readable duration (e.g. `127d 3h 46m`), enumerations as `ethernetCsmacd(6)`, large numbers with thousand separators
- **Result Filtering** — regex-capable filter bar on WALK/GETBULK results to search across OIDs, names, types, and values
- **One-click Copy** — copy any OID, value, or target to clipboard with a single click
- **Device Comparison** — side-by-side multi-target comparison with delta and percentage differences
- **Double-click GET** — double-click any MIB tree node to instantly perform a GET operation
- **SNMPv3 Full Support** — authentication (MD5, SHA, SHA-256, SHA-512) and privacy (DES, AES, AES-256)
- **IPv6** — targets, the trap listener and traceroute, including link-local addresses with a zone
- **SNMP Debug** — live packet inspection, with community strings and passphrases scrubbed from the log

### Dashboards & Presets (`Ctrl + 8`)

- **Presets** — small JSON files describing what to poll on one kind of equipment and how to draw it. Meant to be shared: a preset picks from a fixed vocabulary of widgets and cannot carry anything that executes
- **Preset library** — *Settings → Presets*: add a preset by file or by dropping it on the window. A preset with problems is kept and listed with them, and cannot be bound until they are fixed
- **Cost stated before binding** — OIDs, cadence, requests and values a day, how many readings carry a threshold and how many of those can raise an incident
- **Device detection** — asks an equipment for its `sysObjectID` and puts the presets written for it first
- **Discovery at bind time** — a widget can name a column to walk instead of listing its instances, so one preset fits a 24-port switch and a 48-port chassis, with every port named the way the equipment names it
- **Layout** — widgets placed on a twelve-column grid, collapsing to one column on a narrow window
- **Thresholds carried by the preset** — materialised into the monitoring session when it is bound, and editable afterwards like any other
- **Six widget kinds** — latest value, per-second rate, chart, state tile, grid of states (a switch's port panel), and a **map**
- **Maps** — boxes, lines, labels and dots bound to OIDs and coloured by what each reading is doing, drawn over a picture of your rack or site. Backgrounds are yours: PNG, JPEG or GIF, checked by decoding them; SVG is deliberately refused
- **Several equipments on one dashboard** — one monitoring session per equipment, drawn together as a group
- **A snapshot, not a link** — editing or deleting a preset never changes a dashboard already bound; binding again adopts the new version
- **Six example presets** — IF-MIB interfaces, switch ports, Cisco, a HOST-RESOURCES server, a UPS (RFC 1628), and a 24-port switch drawn as its front panel

### MIB Browser

- **Hierarchical Tree** navigation with collapsible nodes
- **Global Search** across name, OID, description, and syntax
- **Filter Chips** for dynamic filtering
- **Favorites** — bookmark frequently used OIDs
- **Node Details** panel with full OID metadata (syntax, access, status, units, parent, description)
- **Custom MIBs** — load your own MIB files from a persistent directory, and switch any module off
- **Drag & Drop Import** — drop MIB files or folders anywhere in the app. Recursive folder scanning and duplicate detection
- **Why a MIB did not load** — the stage it reached, the line and column with an excerpt, the imports that could not be satisfied and the symbols each was needed for, and a plain answer for the files people download by mistake (an HTML page, a PDF, a zip, UTF-16)
- **Dependency tree** — *Settings → MIBs* shows what imports what, which modules are missing, and which symbols they were needed for

### MIB Editor (`Ctrl + 7`)

- **Syntax highlighting**, find and replace (`Ctrl + F`), undo and redo grouped by edit rather than by keystroke
- **Three kinds of checking** — syntax errors with line and column as you type, whether the module loads, and a semantic pass on a pause that catches what loading does not: unknown types, duplicate OIDs, unresolved parents, missing modules in `FROM`, undefined `INDEX` objects
- **Fix imports** — works out which symbol comes from which module and edits the IMPORTS clause as text, so comments and alignment survive
- **Outline** — every definition in the file with its kind, syntax, access and status, filterable, and still there above a syntax error
- **Safe to use on standard MIBs** — bundled MIBs are backed up before they are overwritten, can be restored to the shipped version, and the tree is health-checked after a reload. Unsaved work survives closing the window

### Monitoring

- **Real-time OID Polling** with interactive Chart.js graphs
- **Multiple View Modes** — raw values, delta, rate (per-second), and latency. Counter wraps are corrected, and rates derived from the time that actually elapsed
- **Threshold Alerts** — a minimum, a maximum and how long it must be breached for. A value oscillating around its threshold is one incident, not forty; a device that stops answering is its own kind of incident
- **Runs in the background** — the poll clock lives in the Go backend, so sessions, thresholds and alerts keep running with the window closed. Tray icon, start at login (per-user, never asks for elevation), and sessions resumed at startup
- **Overload guardrail** — when a round no longer fits inside its cadence, the session widens its own period and says so, instead of polling the device flat out. *Keep my cadence* overrides it
- **Session Management** — create, pause, resume, and delete monitoring sessions
- **Historical Data** — SQLite storage for long-term trending with time-range queries

### Events & Alert Routing (`Ctrl + 6`)

- **Event journal** — traps, threshold episodes opened and resolved, devices that stopped answering, deliveries that failed. Filterable and exportable
- **Routing rules** — match on category, severity, source and OID prefix, with priorities, a "stop here" flag and quiet hours in your own timezone
- **Destinations** — syslog (UDP, TCP, or TLS per RFC 5425, with mutual TLS), webhook (bearer token, custom headers), and email (implicit TLS or STARTTLS). Each accepts a CA certificate, so an internal collector can be trusted without turning verification off
- **Message templates** — per destination, over a fixed list of variables. A webhook can send its own JSON — which is how you talk to Slack, Teams or Alertmanager — with a preview rendered by the same code that sends it
- **Durable outbox** — deliveries survive a closed window or an unreachable relay, with retries and backoff. A delivery log shows what was sent, what is waiting, and what was given up on, with a retry button

### Trap Management

- **Trap Listener** — receive SNMPv1/v2c/v3 traps and acknowledged INFORMs on a configurable port
- **Built for storms** — an 8 MiB receive buffer; measured with no loss at 2000 traps per second sustained
- **Trap Sender** — send traps, or INFORMs to check a receiver acknowledges them (refused on v1, which has no such PDU)
- **Native OS Notifications** — Windows toast / macOS / Linux notifications with MIB-resolved trap names
- **Filtering & Export** — filter received traps and export to CSV

### Network Tools

- **CIDR Discovery** — scan IP ranges for SNMP-responsive devices
- **Ping** — cross-platform, no elevated privileges required (pure Go)
- **Traceroute** — hop-by-hop route tracing, over IPv4 or IPv6

### Query History

- Full operation history with timestamps and results
- **Diff Comparison** — side-by-side diff between any two query results
- **Saved Queries** — bookmark and re-run frequent queries

### Target Management

- Multiple targets with comma or newline separation
- **Target Groups** for organizing devices
- **Labels** — a device's label appears in operations, traps, history and events, with the address on hover
- **Per-target Overrides** — custom SNMP version, community, port per device
- **Connection Testing** — verify reachability before operations
- **Bind a preset** — choose a dashboard preset when you add an equipment, or bind one later

### Anonymous Mode

- **One-click privacy** — hide all sensitive data for safe screenshots and demos
- **Stable aliases** — IPs become `Device-1`, `Device-2`, etc. (consistent within a session)
- **Comprehensive masking** — covers IP addresses, community strings, SNMPv3 credentials, hostnames, device descriptions, trap sources, debug logs
- **Quick toggle** — `Ctrl+Shift+A` or the checkbox in *Settings → General*
- **Visual indicator** — pulsing `ANON` badge in the tab bar when active
- **Non-persistent** — automatically disabled on restart to prevent accidental data hiding

### Security

- **Credentials held by the operating system** — the community string and SNMPv3 passphrases are sealed with a key kept by DPAPI on Windows, the Keychain on macOS, or a protected file on Linux, rather than stored beside the ciphertext
- **Signed updates** — an Ed25519-signed checksum manifest, bound to its release tag, verified before anything is applied; build provenance is attested
- **Content from the network is escaped** for every protocol it is written into — SMTP, mail headers, the syslog header — and exported CSV neutralises cells that a spreadsheet would run as a formula
- **Presets cannot execute anything** — a fixed widget and shape vocabulary, numeric OIDs only, no SVG, and map backgrounds named rather than carried

See the [security policy](SECURITY.md) for how to report a vulnerability.

### UI / UX

- **Dark / Light Theme** with system detection or manual toggle
- **Native Desktop Notifications** — configurable per feature (traps, monitoring alerts)
- **5 Languages** — English, French, German, Spanish, Chinese (auto-detected)
- **Resizable Panels** — adjustable MIB browser width (persisted)
- **Keyboard Shortcuts** — see table below
- **Browser demo** — the real interface on fixed data, at [snmplens.com/demo.html](https://snmplens.com/demo.html)

---

## Keyboard Shortcuts

| Shortcut           | Action                                  |
| ------------------ | --------------------------------------- |
| `Ctrl + 1`         | Operations tab                          |
| `Ctrl + 2`         | Traps tab                               |
| `Ctrl + 3`         | History tab                             |
| `Ctrl + 4`         | Monitor tab                             |
| `Ctrl + 5`         | Network / Discovery tab                 |
| `Ctrl + 6`         | Events tab                              |
| `Ctrl + 7`         | MIB editor tab                          |
| `Ctrl + 8`         | Dashboard tab                           |
| `Ctrl + ,`         | Open Settings                           |
| `Ctrl + B`         | Show or hide the MIB panel              |
| `Ctrl + Shift + A` | Toggle Anonymous Mode                   |
| `Ctrl + F`         | Find and replace, in the MIB editor     |
| `F5`               | Reload MIB files                        |
| `Esc`              | Close the open dialog                   |

On macOS these use the Control key, not Command — except find and replace in the MIB editor, which accepts either.

---

## Tech Stack

| Layer    | Technology                                                                 |
| -------- | -------------------------------------------------------------------------- |
| Framework | [Wails v2](https://wails.io/) — Go backend + Web frontend in one binary  |
| Backend  | Go 1.26 — [gosnmp](https://github.com/gosnmp/gosnmp), [gosmi](https://github.com/sleepinggenius2/gosmi), [SQLite](https://pkg.go.dev/modernc.org/sqlite), [pro-bing](https://github.com/prometheus-community/pro-bing) |
| Frontend | [Svelte 5](https://svelte.dev/) + [Vite 8](https://vitejs.dev/)          |
| Charts   | [Chart.js](https://www.chartjs.org/) with date-fns adapter                |
| i18n     | [svelte-i18n](https://github.com/kaisermann/svelte-i18n)                  |
| Database | SQLite (embedded, WAL mode)                                                |
| Credentials | Key held by DPAPI (Windows), the Keychain (macOS) or a protected file (Linux) |

---

## Build

### Prerequisites

| Requirement | Version |
| ----------- | ------- |
| [Go](https://go.dev/) | 1.26+ |
| [Node.js](https://nodejs.org/) | 20.19+ or 22.12+ (what Vite 8 requires) |
| [Wails CLI](https://wails.io/docs/gettingstarted/installation) | v2 |

**Linux only** — install GTK and WebKit development libraries:

```bash
sudo apt-get install -y libgtk-3-dev libwebkit2gtk-4.1-dev
```

### Development

```bash
wails dev
```

Starts a hot-reload development server with live frontend updates.

### Production

```bash
wails build
```

The compiled binary is output to `build/bin/`.

### Platform-Specific Builds

```bash
wails build -platform windows/amd64          # Windows
wails build -platform linux/amd64 -tags webkit2_41   # Linux
wails build -platform darwin/universal       # macOS (Intel + Apple Silicon)
```

---

## Auto-Update

SnmpLens updates itself from GitHub Releases. On startup (and via **Settings → General → Updates → Check now**) it queries the latest release and, if a newer version exists, shows a banner with the release notes.

**Applying an update** depends on how the app was installed:

| Platform / install | Action |
| --- | --- |
| Windows (installer) | downloads and runs `SnmpLens-windows-amd64-setup.exe`, then relaunches |
| Windows / Linux (portable binary) | replaces the running executable in place, then relaunches |
| macOS, Linux `.deb` | opens the download in the browser (manual install) |

The running version is embedded at build time via ldflags; local (`wails build`) builds report `dev`, which disables the update check.

### Release integrity & authenticity

Every release includes `SnmpLens-checksums.txt` (SHA-256 of all assets) and its Ed25519 signature `SnmpLens-checksums.txt.sig`. Before applying an update the app verifies the signature against the public key embedded in `pkg/updater/verify.go`, then verifies the asset's SHA-256 against the now-authenticated manifest — the "signed manifest" model Linux package repositories use.

- **Signing is mandatory.** The release workflow fails when the key is missing, rather than publishing a release every installed copy would refuse.
- **The manifest names its tag** on its first line (`version v1.2.3`), so an older signed manifest cannot be replayed to install a previous, possibly vulnerable, release.
- **Build provenance is attested**, and can be checked without trusting this repository:

  ```bash
  gh attestation verify SnmpLens-windows-amd64.exe --repo Wasabules/SnmpLens
  ```

**Rotating the signing key:**

```bash
go run ./tools/updatersign keygen
```

1. Paste the printed **public key** into `pkg/updater/verify.go` (`updaterPublicKey`).
2. Store the printed **private key** as the `UPDATER_PRIVATE_KEY` secret of the `release` environment (*Settings → Environments → release*). That environment should carry a protection rule — a required reviewer or a tag policy — so a run has to be admitted before the key is handed to it.

Rotation is not seamless: copies already installed trust only the public key they were built with, so they refuse updates signed with a new key and have to be updated by hand once.

---

## Architecture

```
SnmpLens
├── main.go, app*.go           # Wails entry point, and the methods the frontend calls
├── pkg/
│   ├── snmp/                   # SNMP client: operations, walks, traps and informs, discovery
│   ├── mib/                    # MIB loading (gosmi), diagnostics, editor analysis, dependency graph
│   ├── monitor/                # The poll clock, threshold episodes, counter wraps, overload guardrail
│   ├── preset/                 # Dashboard presets: validation, cost, discovery, layout, thresholds, maps
│   ├── imagegate/              # The decode check a map background has to pass
│   ├── events/                 # The event journal vocabulary
│   ├── notify/                 # Alert routing: syslog, webhook, email, templates, durable outbox
│   ├── storage/                # SQLite (WAL): history, sessions, events, rules, outbox
│   ├── secrets/                # Credential store held by the operating system
│   ├── updater/                # Signed-manifest auto-update
│   ├── service/ tray/ autostart/  # Background operation: early preferences, tray icon, login entry
│   ├── network/                # Pure-Go ping and traceroute
│   └── netaddr/                # Address handling shared by snmp and network (IPv6, zones)
├── mibs/                       # Bundled standard MIBs, extracted on first run
├── presets/                    # Example dashboard presets, extracted on first run
└── frontend/src/
    ├── App.svelte              # Tabbed shell, shortcuts, resizable MIB panel, file drop
    ├── MibPanel.svelte         # MIB tree browser
    ├── OperationsPanel.svelte  # SNMP operations and conceptual tables
    ├── TrapPanel.svelte        # Trap and inform listener and sender
    ├── HistoryPanel.svelte     # Query history and diff
    ├── MonitorPanel.svelte     # Polling sessions and charts
    ├── DashboardPanel.svelte   # Dashboards drawn from presets
    ├── DiscoveryPanel.svelte   # CIDR discovery, ping, traceroute
    ├── EventsPanel.svelte      # Event journal
    ├── MibEditorPanel.svelte   # MIB editor
    ├── SettingsModal.svelte    # Settings dialog; each section lives in settings/
    ├── stores/                 # Svelte stores (state management)
    ├── utils/                  # Helpers (CSV, formatting, layout, dashboard grouping…)
    └── i18n/                   # Translation files (en, fr, de, es, zh)
```

The Go backend exposes methods to the frontend via [Wails bindings](https://wails.io/docs/howdoesitwork). SNMP operations run concurrently using goroutines for multi-target execution. Monitoring runs in Go, one goroutine per session, so it keeps going with the window closed. Data is persisted in an embedded SQLite database with WAL mode. Credentials are sealed with a key held by the operating system, which the frontend never sees.

The reasoning behind the less obvious decisions — and the measurements that settled them — is written down in [CLAUDE.md](CLAUDE.md).

---

## Configuration

Everything is set in the in-app **Settings** dialog (`Ctrl + ,`), in six sections:

- **General** — theme, language, SNMP timeout and retries, confirmation before a SET, data retention, notifications, update checks, Anonymous Mode
- **MIBs** — the MIB directory, which modules are loaded, missing dependencies and the dependency tree
- **Presets** — the dashboard preset library, what each one costs, and map backgrounds
- **SNMP** — default community, SNMPv3 credentials, and a connection test
- **Notifications** — alert destinations, routing rules, message templates and the delivery log
- **Service** — background operation, start at login, what to resume at startup, and the SET audit

Everything SnmpLens writes lives in the user config directory:

| OS      | Path                              |
| ------- | --------------------------------- |
| Windows | `%APPDATA%\SnmpLens\`             |
| macOS   | `~/.config/SnmpLens/`             |
| Linux   | `~/.config/SnmpLens/`             |

| Item            | Contents                                                         |
| --------------- | ---------------------------------------------------------------- |
| `mibs/`         | The bundled MIBs, extracted on first run, plus everything you add |
| `presets/`      | Dashboard presets                                                |
| `assets/`       | Map backgrounds                                                  |
| `mib-backups/`  | Backups taken before a bundled MIB is overwritten                |
| `mib-drafts/`   | Unsaved MIB editor buffers                                       |
| `monitoring.db` | SQLite: history, events, sessions, rules, destinations, outbox   |
| `service.json`  | The few preferences read before the window exists                |
| `simulator.json` | Simulated devices (their passwords are in the system keychain)  |
| `simulator-models/` | Custom simulator models, and their icons                     |

---

## Simulated Devices

The **Simulator** indicator in the header opens the simulated devices: SNMP agents running on this machine, at loopback addresses only — `127.x.x.x` or `::1` — answering v1, v2c and v3 (the whole USM) from a catalogue of models, or from your own (below). A device can be started and stopped, **restarted** (uptime and counters from zero, and `coldStart` if it sends one on starting), **duplicated**, made to send any of its notifications on demand, and **added as a target** in one click, with its port and identifiers.

A bench moves as a file. **Export all…**, or a device's export button, writes JSON, and SnmpLens asks every time whether to put the passwords in it — the communities and passphrases, which otherwise stay in the system keychain. The file says which (`"secrets": "included"` or `"omitted"`). **Import devices…** makes each device new on this machine: its own ID and engine ID, and another address when its own is taken. A device that cannot be made here — of a custom model that is not installed, say — is refused on its own; devices imported without their passwords start once they are given them in the editor.

**Faults** make a device misbehave while it runs, without restarting it — its uptime and counters are what a fault is tested against: a latency (with jitter), a share of requests lost, a device that answers nothing at all, a share answered with `genErr` or `tooBig`, and counters running 10, 100 or 1000 times faster, so that a Counter32 wraps in minutes. They are what SnmpLens's reachability alerts, overload guardrail and counter-wrap arithmetic are tested against, and they are kept with the device. A device can also be given its own sysLocation and sysContact, and set to start with SnmpLens.

---

## Custom Simulator Models

The simulator's catalogue can be extended with models of your own: a JSON file saying what a device answers, imported from the model picker (**Import models…**) on its own, or in a ZIP archive that also carries the model's icon — PNG, JPEG or GIF, up to 512 × 512 px. An archive may hold several models. A model imported again replaces the one kept, and the devices made from it restart.

```json
{
  "kind": "snmplens-simulator-model",
  "formatVersion": 1,
  "id": "acme-crac",
  "name": "Acme CRAC-40 cooling unit",
  "vendor": "Acme",
  "category": "environment",
  "icon": "acme-crac.png",
  "system": { "descr": "Acme CRAC-40 controller, firmware 3.2.1", "objectId": "1.3.6.1.4.1.32473.1.40" },
  "interfaces": [{ "descr": "eth0", "speedMbps": 100, "up": true, "inOctetsPerSec": 400, "outOctetsPerSec": 900 }],
  "objects": [
    { "oid": "1.3.6.1.4.1.32473.2.2.1.1.{#}", "instances": [1, 2, 3], "type": "Integer32", "value": "{#}" },
    { "oid": "1.3.6.1.4.1.32473.2.2.1.2.{#}", "instances": [1, 2, 3], "type": "OctetString", "value": "Sensor {#}" },
    { "oid": "1.3.6.1.4.1.32473.2.2.1.3.{#}", "instances": [1, 2, 3], "type": "Integer32", "gauge": { "min": 180, "max": 260 } },
    { "oid": "1.3.6.1.4.1.32473.2.4.0", "type": "Counter64", "counter": { "perSecond": 1, "start": 31536000 } },
    { "oid": "1.3.6.1.4.1.32473.3.1.0", "type": "OctetString", "value": "Return air above 27 °C", "notifyOnly": true }
  ],
  "notifications": [
    { "name": "acmeHighTemperature", "oid": "1.3.6.1.4.1.32473.0.1",
      "objects": ["1.3.6.1.4.1.32473.2.2.1.3.2", "1.3.6.1.4.1.32473.3.1.0"] }
  ]
}
```

| Field | What it says |
| ----- | ------------ |
| `id` | Lower-case letters, digits and hyphens; the model is listed as `custom:<id>` |
| `category` | `server`, `network`, `security`, `wireless`, `storage`, `power`, `printing`, `environment` or `other` |
| `icon` | A file in the same ZIP; a model imported alone keeps the icon it already had |
| `system` | `descr` and `objectId` (sysDescr, sysObjectID) are required; `contact`, `location`, `services` are not. sysName is the device's name |
| `interfaces` | IF-MIB's tables: `descr`, `name`, `alias`, `type` (IANAifType), `mtu`, `speedMbps`, `up`, `adminDown`, and the rates `inOctetsPerSec`, `outOctetsPerSec`, `errorsPerSec` the counters move at |
| `objects[].type` | `Integer32`, `OctetString`, `ObjectIdentifier`, `IpAddress`, `Counter32`, `Gauge32`, `Unsigned32`, `TimeTicks`, `Counter64`, `Opaque` |
| `objects[]` behaviour | Exactly one of `value`, `hex` (octets), `uptime`, `secondsUp`, `gauge` (`min`, `max`, `periodSec`) and `counter` (`perSecond`, `start`, `swing`, `periodSec`) |
| `instances` | A table's rows: `{#}` in the OID, and in a string value, becomes each; `"value": "{#}"` is the row's own index |
| `notifyOnly` | An accessible-for-notify object: carried by notifications, never answered to a request |
| `notifications` | The model's own, beside `coldStart`, `warmStart` and `authenticationFailure`, which every device sends |

Some OIDs are the agent's own and no model may answer them: SNMPv2-MIB's snmp group on every device, and for SNMPv3 snmpEngine, the MPD and USM statistics and snmpUnknownContexts. They are read live from what the agent counts as it answers — requests, varbinds, a refused community, notifications sent.

A file is checked in full when it is imported — unknown fields, types, ranges, an OID given twice, an OID the agent keeps — and a refusal names the field or the OID. The example is `pkg/simulator/testdata/custom-model.json`; 32473 is the enterprise number RFC 5612 reserves for documentation.

### Model packages

A model can also be spread over the files of one folder — a **package** — so that each file says one thing, and so that a device can answer what a real one answered, recorded:

```
acme-gateway/
├── model.json         what the device is: the format above, "system" optional once a walk records one
├── oids.json          objects, as { "objects": [ … ] } — or several files in oids/
├── traps.json         notifications, as { "notifications": [ … ] }
├── walks/             what a real device answered: snmpsim .snmprec, or snmpwalk -On output
│   └── gateway.snmprec
└── acme-gateway.png   the icon model.json names
```

Zip the folder and import it; one archive may hold several packages and lone models. Record a walk with `snmpwalk -v2c -c public -On -Ox <host> .1.3.6.1 > walks/device.walk` — plus a walk of `.1.0.8802` if the device answers LLDP — or with snmpsim's recorder. `-On` keeps the OIDs numeric, which is how a walk is read; `-Ox` keeps an OCTET STRING's octets rather than the text a MIB's DISPLAY-HINT makes of them, so a MAC address stays six octets.

Three rules put the files together:

- **What is written wins over what was recorded**, OID by OID: an object in an oids file replaces the value a walk recorded at the same OID — which is how a recorded constant is made to move, or a recorded value corrected. An OID written twice is still an error. When `model.json` gives `interfaces`, IF-MIB is made from them and the walks' own is left out.
- **The agent's own objects stay behind.** The snmp group and everything under `1.3.6.1.6.3` — the recorded device's engine, its USM users, VACM groups and community strings — are never served, and the import says how many were left out.
- **The system group is made, never replayed.** sysName is the simulated device's name and sysUpTime its own uptime; sysDescr, sysObjectID, contact, location and services are what the walks recorded, except where `model.json`'s `system` says otherwise.

**Recorded counters move.** Each starts at the value recorded and grows at the rate it had averaged since the device booted — its value over the sysUpTime recorded beside it — swinging around that rate as traffic does. hrSystemUptime and hrSystemDate are the device's own; everything else answers what was recorded. A line of a walk that cannot be read is left out and counted, and the import says where the first one was. A walk is at most 16 MB, and a package's walks record at most 65 536 objects between them. The example is `pkg/simulator/testdata/package`.

### Recording a device

**Record a device…** in the model picker walks a real device — `.1.3.6.1`, then `.1.0.8802` for LLDP — with the identifiers its target uses, and keeps what it answered as a package: a model like any other, from which a device answers what the real one did, its counters moving. The recorded device's own agent — its snmp group, engine, USM users, VACM groups and community table — is never written. A recording can be stopped, and then keeps nothing; a device answering more than 65 536 objects is refused rather than cut short.

**Export** (beside a custom model's delete button) writes any custom model to a ZIP as a package folder, with its icon. It is how a recording is edited — add an `oids.json` beside its walk to make a recorded constant move, or a `traps.json` to give it notifications — and imported again, or passed on.

---

## Test Agent

A built-in Python SNMP agent simulator is provided in `tools/snmp_test_agent.py` for testing without real network equipment. It requires no external dependencies beyond `pycryptodome` (for SNMPv3 AES).

```bash
pip install pycryptodome
python tools/snmp_test_agent.py --trap-port 1162 --trap-interval 10
```

### Features

- Pure Python — no pysnmp dependency, works on Python 3.10+
- SNMPv1, v2c, and full v3 support (discovery, authentication, encryption)
- 5 simulated interfaces with dynamic traffic counters
- Realistic OIDs: system, ifTable, ifXTable, IP, SNMP stats, hrSystem, hrStorage
- Periodic trap sending (v2c and v3) with linkDown, linkUp, coldStart, customAlert

### Credentials

| Version | User | Auth | Priv |
| ------- | ---- | ---- | ---- |
| v1/v2c | — | community `public` | — |
| v3 | `snmplens` | SHA / `authpass123` | AES-128 / `privpass123` |
| v3 | `sha256user` | SHA-256 / `authpass123` | AES-128 / `privpass123` |
| v3 | `sha512user` | SHA-512 / `authpass123` | AES-256 / `privpass123` |
| v3 | `authonly` | SHA / `authpass123` | — |
| v3 | `noauthuser` | — | — |

---

## License

[MIT](LICENSE) — Geoffrey Lecoq
