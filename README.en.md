# Viya

<p align="center"><img src="assets/logo.png" alt="Viya" width="112"></p>

<p align="center"><strong>A self-hosted visual control panel for Caddy</strong><br>Reverse proxies, static sites, TLS certificates, dynamic DNS and TCP/UDP forwarding in one console.</p>

<p align="center"><a href="README.md">简体中文</a> · English</p>

<p align="center">
  <a href="https://github.com/ringer520/viya-proxy/releases/latest"><img alt="Release" src="https://img.shields.io/github/v/release/ringer520/viya-proxy?color=blue"></a>
  <a href="https://hub.docker.com/r/ringer520/viya-proxy"><img alt="Docker" src="https://img.shields.io/badge/docker-ringer520%2Fviya--proxy-2496ed?logo=docker&logoColor=white"></a>
  <a href="LICENSE"><img alt="License" src="https://img.shields.io/badge/license-MIT-green"></a>
</p>

<p align="center"><a href="https://viya.861010.xyz">Website · viya.861010.xyz</a></p>

**Contents:** [Quick start](#quick-start) · [Security](#security) · [Features](#features) · [Screenshots](#screenshots) · [Installation](#installation) · [Backups](#backups-and-restore) · [Build from source](#build-from-source) · [Uninstall](#uninstall) · [File service reach](docs/文件服务权限.md)

## Quick start

```sh
curl -fsSL https://raw.githubusercontent.com/ringer520/viya-proxy/main/install.sh | sh
```

Open `http://SERVER_IP:8181` afterwards and create the administrator account. systemd Linux and OpenWrt are supported, with amd64, arm64 and armv7 builds; see [Installation](#installation) for Docker.

> [!IMPORTANT]
> The panel listens on `0.0.0.0:8181` and the first administrator can be created from any IP that can reach it. **On a VPS or any machine reachable from the internet, read [Security](#security) before initialising.**

## Security

Viya holds your domains, certificates and all forwarded traffic. The defaults are conservative, but three things can only be decided by you.

### Before initialising

- **Never expose an uninitialised panel to the internet.** Creating the first administrator does not check the source address, so whoever opens it first becomes the administrator.
- **On a VPS, bind the panel to loopback at install time** and initialise through an SSH tunnel:

```sh
# On the server: the panel only listens locally
VIYA_INSTALL_LISTEN=127.0.0.1:8181 sh install.sh

# Then tunnel and open http://127.0.0.1:8181 locally
ssh -L 8181:127.0.0.1:8181 root@YOUR_SERVER
```

### Once HTTPS works

Turn on **Panel settings → Access & security → Block IP access from the internet**. Afterwards:

| How you connect | Result |
| --- | --- |
| A hostname that reverse-proxies to the panel | Allowed (discovered from the site table, no list to maintain) |
| A LAN address | Allowed |
| A public IP or an unknown hostname | 403, refused before the login form |
| The host itself / an SSH tunnel | Allowed, so a wrong rule can be undone |

For tighter control, expand "Advanced" in the same card: explicit hostnames, source networks and the HTTPS redirect.

<details>
<summary>Lockout protection and recovery</summary>

Saving replays the new rules against the request you are making. A rule that would cut you off is rejected with a message instead of being saved.

If you do get locked out, remove the row over SSH:

```sh
python3 -c "
import sqlite3
c = sqlite3.connect('/var/lib/viya/viya.db')
c.execute(\"DELETE FROM panel_settings WHERE key='panel_access'\")
c.commit()"
sudo systemctl restart viya
```

The local bypass requires both the peer and the resolved client to be local. Trusting the peer alone would treat every proxied request as coming from `127.0.0.1` and disable the rules entirely; `X-Forwarded-For` is only read when the peer is in `VIYA_TRUSTED_PROXIES`.

</details>

### Always

- **Keep the master key separate.** `/etc/viya/secrets/master.key` decides whether stored DNS credentials can be decrypted. Storing it next to the backup means the credentials are not encrypted at all.
- **Exported configuration contains live credentials and certificates.** Do not share it.
- **Any reverse proxy in front of the panel must be in `VIYA_TRUSTED_PROXIES`.** Otherwise every visitor looks like one address, which breaks login throttling, region bans and the audit log, and turns "force HTTPS" into a redirect loop.
- **The server file service is read-only by default.** Its reach is decided by the run user and systemd restrictions; see the [file service reach notes](docs/文件服务权限.md) (Chinese).

## Features

| Area | What it does |
| --- | --- |
| Web services | Reverse proxy, static site and URL redirect; managed per HTTP/HTTPS listener with path matching, upstream tuning, cloning and enable/disable |
| TLS certificates | Issuance and auto-renewal, progress, custom upload; sites can reuse a matching wildcard certificate |
| Dynamic DNS | A / AAAA / CNAME; follow the interface or pin an address, scheduled and manual updates |
| DNS credentials | Cloudflare, DNSPod, Alibaba Cloud and others; encrypted at rest with a separate master key |
| TCP / UDP forwarding | Single- or dual-protocol rules with 30-day connection and traffic stats, counted separately from web traffic |
| Access control | IP/CIDR bans, access rules, login throttling, automatic banning, security headers |
| Operations | Resource monitoring, access and program logs, traffic trends, configuration history, backup and restore |
| Accounts and UI | Administrator and read-only accounts, Chinese/English, five base themes plus three anime themes |
| Files | Per-site file management (upload, edit, rename, delete); a read-only server file service, hidden by default |

<details>
<summary>What the status indicators mean</summary>

The UI separates "configuration saved" from "actually serving", so a successful save is never mistaken for a working service.

- Port forwarding shows the configured state and the verified listener separately; a listening port does not mean the upstream works.
- DDNS reports each stage (address lookup, comparison, submission, provider read-back) and records inspection time apart from actual change time. IPv6 can follow the interface or pin an address; a pinned address that disappears is reported, not silently replaced.
- Certificate usage is listed on the last line of each entry; site diagnostics check the whole request path.
- A passing read-only credential check does not prove write permission.
- Bans are manual, region-based or automatic, and you can query which rule matches an address. Changing the panel's own restriction must be confirmed through the matching hostname within 120 seconds, otherwise the previous rule is restored; the LAN entry can be cancelled.
- Log filtering covers site, keyword, time and status over at most the last 2 MiB of the current file; configuration history keeps the latest 10 operation summaries and actors.
- Region rules match the address that connects to Caddy. Behind a CDN or another proxy, the proxy egress may be blocked by your own region rules.

</details>

## Screenshots

The overview shows server resources, traffic trends over a selectable range and the services that need attention; clicking the web error rate breaks recent errors down by site and status code.

![Viya overview](docs/screenshots/02-overview.webp)

<details>
<summary>More screens</summary>

| Web services | TCP / UDP forwarding |
| --- | --- |
| ![Web services](docs/screenshots/03-proxy-sites.webp) | ![Port forwarding](docs/screenshots/04-layer4.webp) |

| Dynamic DNS | TLS certificates |
| --- | --- |
| ![Dynamic DNS](docs/screenshots/05-ddns.webp) | ![Certificates](docs/screenshots/06-certificates.webp) |

| DNS credentials | IP bans |
| --- | --- |
| ![DNS credentials](docs/screenshots/07-dns-providers.webp) | ![IP bans](docs/screenshots/08-ip-bans.webp) |

| Logs | Configuration history |
| --- | --- |
| ![Logs](docs/screenshots/09-logs.webp) | ![Configuration history](docs/screenshots/10-config-history.webp) |

</details>

## Installation

### Linux (systemd)

```sh
curl -fsSL https://raw.githubusercontent.com/ringer520/viya-proxy/main/install.sh | sh
```

The installer verifies the release SHA-256, creates a systemd service and enables it at boot. It needs `curl` or `wget`, `tar`, `python3` and system CA certificates, and will try to install missing dependencies with the system package manager. Running it as a normal user works too: it re-runs itself through `sudo`.

<details>
<summary>Port conflicts, firewall and pinning a version</summary>

If the default port is taken, the installer prompts for another one, or you can set it directly:

```sh
curl -fsSL https://raw.githubusercontent.com/ringer520/viya-proxy/main/install.sh | VIYA_INSTALL_LISTEN=0.0.0.0:8182 sh
```

With a host firewall enabled, allow the panel TCP port from your LAN as well as the web service ports (usually 80/443).

Pin a release:

```sh
curl -fsSL https://raw.githubusercontent.com/ringer520/viya-proxy/main/install.sh | VERSION=v0.4.0 sh
```

Environment variables must stay on the right side of the pipe.

</details>

<details>
<summary>Service management and self-update</summary>

```sh
sudo systemctl status viya.service
sudo journalctl -u viya.service -f
sudo systemctl restart viya.service
```

systemd installs can check for and apply updates from panel settings; Docker deployments update by pulling a new image.

</details>

### Docker

The database, certificates, site files, Caddy data, logs and master key all live in `/var/lib/viya` inside the container, so map a single host directory.

```sh
sudo mkdir -p /opt/viya/data && cd /opt/viya
```

```yaml
services:
  viya:
    image: ringer520/viya-proxy:latest
    container_name: viya
    restart: unless-stopped
    ports:
      - "8181:8181"       # panel
      - "80:80"           # HTTP sites and HTTP-01 validation
      - "443:443"         # HTTPS sites
      - "443:443/udp"     # HTTP/3 (optional)
    volumes:
      - /opt/viya/data:/var/lib/viya
```

```sh
docker compose up -d
docker compose logs -f viya
```

Open `http://SERVER_IP:8181`. Update with `docker compose pull && docker compose up -d`.

<details>
<summary>Port conflicts, certificate validation and update caveats</summary>

- To avoid a panel port clash, change the host side, for example `18181:8181`.
- Free host ports 80/443 first, and keep the site listening ports consistent with the mappings.
- HTTP-01 validation needs port 80 reachable from the internet; use DNS-01 otherwise.
- Back up `/opt/viya/data` before updating and keep its `master.key`, or stored DNS credentials can no longer be decrypted.
- **Never update with `docker compose down -v`** — it deletes the data volume.

</details>

### OpenWrt

Supports **amd64, arm64 and armv7** on procd-based OpenWrt; MIPS is not supported. Run as root over SSH:

```sh
wget -qO- https://raw.githubusercontent.com/ringer520/viya-proxy/main/install.sh | sh
```

The generic entry point detects OpenWrt and calls the dedicated installer. A fresh install serves `http://ROUTER_IP:8181`, keeps data in `/opt/viya/data` and the master key in `/etc/viya/secrets/master.key`.

<details>
<summary>Port 80 conflicts (LuCI) and advanced options</summary>

Port 80 on OpenWrt belongs to LuCI's uhttpd by default, while Viya's web listeners default to 80/443. The installer names the process holding the port and continues anyway, but **saved sites cannot serve traffic** until the port is freed or the listeners are moved — the symptom is a failed configuration apply. Two ways out:

```sh
# Move LuCI off 80 (it then listens on 8080)
uci set uhttpd.main.listen_http='0.0.0.0:8080' '[::]:8080'
uci commit uhttpd && /etc/init.d/uhttpd restart

# Or leave LuCI alone and move 80/443 in the panel under Web services → Listeners
```

Pin a version and port:

```sh
wget -O /tmp/viya-install.sh https://raw.githubusercontent.com/ringer520/viya-proxy/main/deploy/install-openwrt.sh
VERSION=v0.4.0 VIYA_INSTALL_PORT=8181 sh /tmp/viya-install.sh
```

Change the service listener:

```sh
uci set viya.main.listen='0.0.0.0:8182'
uci commit viya && /etc/init.d/viya restart
logread -e viya
```

Requires `curl` or `wget`, `tar`, `sha256sum`, `netstat`, UCI and HTTPS CA certificates (`ca-bundle` on opkg systems, `ca-certificates` on apk systems). `/opt` must be persistent or an attached external volume, and downloads need roughly 128 MiB free in `/tmp`. The installer never opens WAN ports on its own. On low-memory routers, prefer external storage or a separate Linux host.

The OpenWrt script passes POSIX shell syntax checks but has not been validated on every device; confirm firmware, CPU, storage and networking on the actual hardware. Before upgrading, copy data, keys and the binary to `/opt/viya/backups/install-*`.

</details>

## Backups and restore

The backup tools live in `/opt/viya/scripts/` and need Python 3.11 or newer.

```sh
sh /opt/viya/scripts/backup-linux.sh
```

A normal backup contains a consistent SQLite copy, hosted site files, certificates and runtime data, but **not** the master key or access logs. Archives land in `/var/backups/viya/` with a manifest, SHA-256 checksums and the key fingerprint.

<details>
<summary>Full migration, verification, restore and retention</summary>

For a full migration, stop the service briefly and include the master key; the script restarts whatever was running before (run as root, or prefix with `sudo`):

```sh
VIYA_BACKUP_INCLUDE_KEY=1 VIYA_BACKUP_STOP_SERVICE=1 sh /opt/viya/scripts/backup-linux.sh
```

A full backup holds accounts, credentials and private keys, is not additionally encrypted, is written with mode `0600`, and should be copied somewhere protected off the machine. Site roots outside the data directory need their own backup.

Verify and rehearse a restore without touching the current install:

```sh
sh /opt/viya/scripts/restore-linux.sh --verify /path/to/viya-backup.tar.gz
VIYA_RESTORE_KEY=1 sh /opt/viya/scripts/restore-linux.sh --dry-run /path/to/viya-backup.tar.gz
```

Restoring a full backup requires explicitly accepting the bundled key; for a normal backup omit `VIYA_RESTORE_KEY=1` and supply the original master key:

```sh
VIYA_RESTORE_KEY=1 sh /opt/viya/scripts/restore-linux.sh /path/to/viya-backup.tar.gz
```

Restore backs up the current data first, verifies, then swaps it in and checks that Viya and Caddy come up; a failed start rolls the data and key back. The target must already have Viya installed with the same data directory; `VIYA_DATA_DIR`, `VIYA_MASTER_KEY_FILE` and `VIYA_BACKUP_DIR` override the paths.

The encrypted configuration export in the panel excludes accounts, passwords, sessions and site files, but includes live credentials and certificates — do not publish it. Docker users should stop the container and back up the whole mapped directory.

Panel settings → Storage shows usage and the retention policy; log rotation is configured under Logs. Manual cleanup only removes expired statistics, recent access records and expired sessions — never sites, certificates or restore points. SQLite reuses free pages, so the file does not necessarily shrink immediately.

</details>

## Build from source

Go 1.27+, Node.js 22 and pnpm 9. The frontend is embedded in the binary, so no Node.js or external database is needed at runtime.

```sh
pnpm --dir web install --frozen-lockfile
pnpm --dir web build
go build ./cmd/viya
```

The frontend is written to `internal/web/dist/`, which is generated and not committed. Always run `pnpm --dir web build` first, otherwise `go build` fails because `go:embed` cannot find the files.

<details>
<summary>Windows packaging and the release pipeline</summary>

```powershell
./scripts/build-linux.ps1
./scripts/package-linux.ps1
```

Pushing a `v*` tag triggers GitHub Actions, which builds amd64, arm64 and armv7 packages with SHA-256 files and publishes multi-arch Docker images. Release notes live in `docs/releases/` and become the GitHub Release body.

</details>

## Uninstall

```sh
sudo systemctl disable --now viya.service
sudo rm -f /etc/systemd/system/viya.service
sudo rm -rf /etc/systemd/system/viya.service.d /opt/viya
sudo systemctl daemon-reload
```

This keeps the database, certificates, site files and master key. Once the data is definitely not needed, remove `/var/lib/viya`, `/etc/viya`, `/var/log/viya` and `/var/backups/viya` separately. For Docker, run `docker compose down`; persistent data remains in `/opt/viya/data`.

## License

Viya is released under the [MIT License](LICENSE).
