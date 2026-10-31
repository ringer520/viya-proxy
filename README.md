# Viya

<p align="center">
  <img src="assets/logo.png" alt="Viya logo" width="140">
</p>

**A modern, single-binary reverse proxy manager built on Caddy.**

Viya is a self-hosted control panel for managing [Caddy](https://caddyserver.com/) reverse-proxy sites and Layer 4 (TCP/UDP) port forwards. It pairs a Go + SQLite control plane with a polished Vue 3 interface and runs Caddy as a supervised subprocess — deploy one binary, open the browser, and manage everything from a clean dashboard.

---

## Why Viya?

Most reverse proxies are configured by hand-editing text files. Viya replaces that with a **safe, self-hosted web console**:

- **One binary, zero runtime dependencies** — the Vue frontend is embedded, the database is a single SQLite file, and Caddy ships alongside. No Node, no Docker, no external database.
- **Native Chinese UI with full English support** — the interface is designed Chinese-first and switches to English in one click, with per-panel bilingual copy.
- **Web sites and port forwards unified** — HTTPS sites and raw TCP/UDP forwards live in the same console and share the same traffic statistics.
- **Secure by default** — the admin API and Caddy's admin endpoint bind to loopback only; sessions, CSRF, rate limiting, read-only accounts and Fail2Ban-style auto-bans are built in.

## Screenshots

The dashboard combines live traffic visualization with everyday management tasks in a single overview.

<div align="center">

**Run overview** — engine health, CPU/memory/disk gauges, a 24-hour traffic trend and a "needs attention" roll-up.

<img src="docs/screenshots/02-overview.png" alt="Viya run overview" width="820">

**Web services** — sites grouped by HTTP/HTTPS listener, each with per-site access control and 24-hour traffic.

<img src="docs/screenshots/03-proxy-sites.png" alt="Viya web services" width="820">

**Port forwarding** — TCP/UDP rules with the same traffic block as web sites, unified in one list.

<img src="docs/screenshots/04-layer4.png" alt="Viya port forwarding" width="820">

**Dynamic DNS** — scheduled public-IP detection with per-record update history.

<img src="docs/screenshots/05-ddns.png" alt="Viya dynamic DNS" width="820">

**SSL certificates** — automatic and custom certificates, issuance progress and expiry tracking.

<img src="docs/screenshots/06-certificates.png" alt="Viya SSL certificates" width="820">

**Logs** — live access logs, application logs, login audit and rotation settings.

<img src="docs/screenshots/09-logs.png" alt="Viya logs" width="820">

**Configuration history** — every applied change is snapshotted and can be diffed (sensitive values are redacted).

<img src="docs/screenshots/10-config-history.png" alt="Viya configuration history" width="820">

**Panel settings** — five themes, light/dark, compact layout, accounts and alerting in one drawer.

<img src="docs/screenshots/11-panel-settings.png" alt="Viya panel settings" width="820">

</div>

## Highlights

### Reverse proxy, done right

- **Proxy sites** — full CRUD with automatic HTTPS (HTTP-01, DNS-01, internal CA, HTTP-only, custom certificates) and upstream request headers.
- **Multiple listeners** — independent HTTP/HTTPS listeners, each managing its own set of sites.
- **Layer 4 forwarding** — TCP, UDP and TCP+UDP port forwards with conflict detection.
- **Per-site security** — Basic Auth, CIDR allow/deny lists and per-IP rate limiting.

### DNS, integrated

- **DNS providers** — Cloudflare, Tencent Cloud DNSPod and Alibaba Cloud credentials, encrypted at rest with AES-256-GCM.
- **Dynamic DNS** — A/AAAA/CNAME records with public-IP detection (or a pinned interface address) on a configurable schedule, plus full run history.

### Traffic you can see

- **Unified statistics** — web sites and port forwards are aggregated into one overview with 24h / 7d / 30d windows and a per-period ranking.
- **Live charts** — per-site/per-rule sparklines and a whole-instance trend line.

### Observability

- **Upstream health** — periodic TCP probes with latency shown per site.
- **Certificate expiry** — aggregated "needs attention" roll-up with a configurable warning window.
- **Alerting webhook** — JSON push on upstream failures, repeated DDNS failures, certificate order failures, a nearly-full data directory, or a Caddy crash (de-duplicated per event for 10 minutes).
- **System metrics** — CPU, memory and disk gauges on the overview.

### Safe by design

- **Session auth** — bcrypt, server-side sessions, CSRF tokens, and SQLite-persisted login throttling (survives restarts).
- **Read-only accounts** — a `viewer` role that can read every page but is rejected on any write.
- **Fail2Ban Lite** — auto-bans an IP that triggers repeated 400/401/403 for the same site, without touching manual rules.
- **Configuration history** — every apply is snapshotted and diffable, and startup reconciliation restores the last-known-good config. If Caddy dies, Viya restarts it.

## Tech stack

| Layer | Choice |
| --- | --- |
| Control plane | Go 1.27 |
| Storage | SQLite (WAL, single connection, no external DB) |
| Frontend | Vue 3 + TypeScript + Vite |
| Engine | Caddy (custom build with `caddy-l4`, DNS provider and rate-limit modules) |
| Delivery | Single static binary, frontend embedded via `go:embed` |

## Quick start

```sh
# 1. Fetch dependencies and build the frontend
pnpm --dir web install
pnpm --dir web build

# 2. Build the Viya binary (embeds the frontend)
go build -trimpath -o bin/viya ./cmd/viya

# 3. Build the custom Caddy binary (from build/caddy)
cd build/caddy && go build -trimpath -o ../../bin/caddy .

# 4. Run against a local data directory
VIYA_CADDY_BIN="$(pwd)/bin/caddy" ./bin/viya
```

Open `http://127.0.0.1:8181`, create your first admin account (password ≥ 8 characters), and start adding sites.

The `scripts/build-linux.ps1` script produces `bin/viya-linux-amd64` and the matching `bin/caddy-linux-amd64`, and `scripts/package-linux.ps1` bundles them into a `release/` tarball.

## Installation

### One-line install (Debian/Ubuntu)

```sh
curl -fsSL https://raw.githubusercontent.com/ringer520/viya-proxy/main/install.sh | sudo sh
```

This downloads the latest prebuilt release and provisions the systemd service end to end. To pin a specific version:

```sh
curl -fsSL https://raw.githubusercontent.com/ringer520/viya-proxy/main/install.sh | VERSION=v1.0.0 sudo -E sh
```

### Prebuilt release

Releases are built automatically by GitHub Actions whenever a `v*` tag is pushed (see [`.github/workflows/release.yml`](.github/workflows/release.yml)). Each release ships a `viya-linux-amd64.tar.gz` containing the Viya and Caddy binaries plus the Debian installer.

1. Download the latest `viya-linux-amd64.tar.gz` from [Releases](https://github.com/ringer520/viya-proxy/releases).
2. Extract it and run the installer:

```sh
tar -xzf viya-linux-amd64.tar.gz
cd viya-linux-amd64/
sudo sh ./deploy/install-debian.sh
```

3. Confirm it is running:

```sh
systemctl status viya
journalctl -u viya -f
```

### Build from source

Requirements: Go 1.27+, Node.js and pnpm. The Caddy plugin versions are pinned in `build/caddy/go.mod`.

```powershell
pnpm --dir web install
pnpm --dir web typecheck
pnpm --dir web build
go build ./...
./scripts/build-linux.ps1
./scripts/package-linux.ps1
```

## Deployment

The admin UI/API binds to `127.0.0.1:8181` by default. From a trusted workstation, use an SSH tunnel:

```sh
ssh -L 8181:127.0.0.1:8181 root@YOUR_HOST
```

Caddy's admin API listens on loopback only. The service serves sites on `:8080` by default and `:80` for plain HTTP and ACME HTTP-01; TLS sites use the configured listener ports. For a standard production setup, set `VIYA_HTTP_LISTEN=:80` via a systemd drop-in so Caddy uses ports 80/443, then configure NAT/firewall rules as needed.

> **Never expose the admin port or Caddy's admin API to an untrusted network.** If you reverse-proxy the panel itself, set `VIYA_COOKIE_SECURE=true` and `VIYA_TRUSTED_PROXIES` to the proxy's IP.

### Configuration

| Environment variable | Default | Purpose |
| --- | --- | --- |
| `VIYA_LISTEN` | `127.0.0.1:8181` | Admin UI/API address |
| `VIYA_HTTP_LISTEN` | `:8080` | Caddy site listener; HTTP-only sites also serve on `:80` |
| `VIYA_DATA_DIR` | `./data` | SQLite, certificates and Caddy runtime data |
| `VIYA_MASTER_KEY_FILE` | `$VIYA_DATA_DIR/master.key` | Master key for DNS credential encryption (`/etc/viya/secrets/master.key` on installed setups) |
| `VIYA_CADDY_BIN` | `./bin/caddy` | Custom Caddy executable |
| `VIYA_CADDY_ADMIN` | `http://127.0.0.1:2019` | Caddy admin API (must be loopback) |
| `VIYA_COOKIE_SECURE` | `false` | Set `true` when the panel is served over HTTPS |
| `VIYA_TRUSTED_PROXIES` | `127.0.0.1,::1` | Comma-separated trusted proxy IPs/CIDRs that may set `X-Forwarded-Proto` |

## Backup & restore

```sh
sudo sh ./scripts/backup-linux.sh
sudo sh ./scripts/restore-linux.sh /var/backups/viya/viya-YYYYMMDDTHHMMSSZ.tar.gz
```

Backups use the SQLite online backup API and include Caddy certificate state and custom certificates; the archive is `0600` and treated as sensitive (it contains private keys). The master key lives at `/etc/viya/secrets/master.key` and is **not** included in backups — keep it separately, or encrypted DNS credentials cannot be decrypted after a restore.

## Documentation

See [`docs/`](docs/) for the implementation plan, operation flow, and hardening notes. The console itself carries per-page guidance in both languages.

## License

[MIT](LICENSE) © 2026 ringer520
