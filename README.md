<p align="center">
  <img src="SnmpLens.png" alt="SnmpLens" width="128" />
</p>

<h1 align="center">SnmpLens</h1>

<p align="center">
  A modern SNMP MIB browser and network management desktop application built with <a href="https://wails.io/">Wails</a> (Go + Svelte).
  <br />
  <strong>Windows / macOS / Linux</strong>
</p>

## Features

- **SNMP Operations** — GET, SET, GETNEXT, GETBULK, WALK with concurrent multi-target execution
- **Smart Table View** — Auto-detect SNMP tables and render as structured columns with sorting and export
- **MIB Browser** — Hierarchical tree navigation, global search (name, OID, description, syntax), filter chips, favorites
- **Trap Listener** — Receive and send SNMPv1/v2c/v3 traps with filtering and export
- **Network Tools** — CIDR discovery, ping (pure Go, cross-platform), traceroute
- **Real-time Monitoring** — OID polling with Chart.js graphs, delta/rate/latency views, threshold alerts with desktop notifications and sound
- **Historical Data** — SQLite persistent storage for long-term trending, session stats, time-range queries
- **Device Comparison** — Side-by-side multi-target comparison with delta and percentage difference
- **SNMP Debug** — Live packet inspection for troubleshooting
- **Query History** — Full operation history with diff comparison between results
- **Target Management** — Target groups, per-target SNMP parameter overrides, connection testing
- **SNMPv3 Support** — Full authentication (MD5, SHA, SHA-256, SHA-512) and privacy (DES, AES) support
- **Dark / Light Theme** — System detection or manual toggle
- **Internationalization** — English, French, German, Spanish, Chinese
- **Security** — AES-256-GCM credential encryption in local storage

## Build

### Prerequisites

- [Go](https://go.dev/) 1.25+
- [Node.js](https://nodejs.org/) 18+
- [Wails CLI](https://wails.io/docs/gettingstarted/installation) v2

### Development

```bash
wails dev
```

### Production

```bash
wails build
```

## License

[MIT](LICENSE)
