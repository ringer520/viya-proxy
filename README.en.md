# Viya

<p align="center"><img src="assets/logo.png" alt="Viya" width="112"></p>

<p align="center"><strong>A self-hosted visual control panel for Caddy</strong><br>Manage reverse proxy sites, static websites, TLS certificates, dynamic DNS and TCP/UDP forwarding from one console.</p>

<p align="center"><a href="README.md">简体中文</a> · English</p>

## Quick navigation

- [Screenshots](#screenshots) · [Features](#features)
- [Linux installation](#linux-installation) · [Docker installation and updates](#docker-installation-and-updates)
- [Backups and restore](#backups-and-restore) · [One-line OpenWrt installation](#one-line-openwrt-installation)
- [Build from source](#build-from-source) · [Uninstall](#uninstall)

### Operational status and access protection

- Port forwards distinguish enabled configuration from verified engine listeners and offer a pre-save port check. A listener does not prove upstream application health.
- DDNS exposes address, comparison, update and provider-readback stages, with separate check/change timestamps. Interface mode supports automatic IPv6 selection or an explicit pin; unavailable pins are never silently replaced.
- SSL checks inspect the certificate served by the local TLS listener. Issuance records and the currently served certificate are separate states.
- DNS credentials support read-only checks, credential replacement and dependency checks. Successful reads do not verify DNS write permission.
- Access rules are grouped into manual, region and automatic bans. Panel-domain restrictions require confirmation through the affected domain within 120 seconds or the original rule is restored; LAN access can cancel.
- Logs support server-side filters over at most the last 2 MiB of the current file. Configuration history keeps the latest 10 operation summaries and actors.

Region rules use the source connecting to Caddy, which may be a CDN or proxy address. Upgrades do not automatically remove existing restrictions.


## Screenshots

The overview brings together host resource gauges, a 24-hour traffic trend and service health. Expand the section below to view the management pages.

![Viya runtime overview](docs/screenshots/02-overview.png)

<details>
<summary>Show other pages</summary>

| Web services | TCP / UDP forwarding |
| --- | --- |
| ![Web services](docs/screenshots/03-proxy-sites.png) | ![Port forwarding](docs/screenshots/04-layer4.png) |

| Dynamic DNS | TLS certificates |
| --- | --- |
| ![Dynamic DNS](docs/screenshots/05-ddns.png) | ![Certificates](docs/screenshots/06-certificates.png) |

| DNS credentials | IP bans |
| --- | --- |
| ![DNS credentials](docs/screenshots/07-dns-providers.png) | ![IP bans](docs/screenshots/08-ip-bans.png) |

| Logs | Configuration history |
| --- | --- |
| ![Logs](docs/screenshots/09-logs.png) | ![Configuration history](docs/screenshots/10-config-history.png) |

</details>

## Features

Web services and port forwards have separate rolling 30-day statistics. Click the overview Web error rate to locate sites and HTTP status codes in retained recent logs. Settings → Storage provides a confirmed reset of all traffic statistics, preserving business configuration and original log files.


- **Web services:** reverse proxy, static hosting and URL redirects, grouped by HTTP/HTTPS listener; custom ports and paths, clone, enable/disable and security policies.
- **Static files:** upload, browse, download, rename and delete website files in the panel. Choose whether to remove hosted files when deleting a site.
- **TLS certificates:** issue and renew certificates, see issuance progress, upload custom certificates and reuse matching wildcard certificates.
- **Dynamic DNS:** A, AAAA and CNAME records; choose an interface address or public-IP lookup; scheduled and manual updates.
- **DNS credentials:** Cloudflare, DNSPod, Alibaba Cloud and other supported providers. Credentials are encrypted with a separately stored master key.
- **TCP / UDP forwarding:** manage TCP, UDP or dual-protocol rules and inspect connection and traffic statistics.
- **Access security:** IP/CIDR bans, access controls, login throttling, automatic bans, default security settings and response headers.
- **Operations:** CPU, memory and disk monitoring; access and application logs; traffic trends; configuration history, backup and restore.
- **Accounts and UI:** administrator and read-only roles, Chinese and English, five base palettes, three original illustrated anime themes, system-aware motion, and a compact display mode.

## Linux installation

The one-line installer supports Linux systems running **systemd** on `amd64`, `arm64` and `armv7`. It verifies the release archive with SHA-256, installs a system service and enables it at boot. The installer needs `curl` or `wget`, `tar`, `python3` and system CA certificates; it attempts to install missing prerequisites with the system package manager.

Run as root:

```sh
curl -fsSL https://raw.githubusercontent.com/ringer520/viya-proxy/main/install.sh | sh
```

The same command works for a regular user: the installer detects that it is not root and re-fetches itself through `sudo`. A regular account must have `sudo` installed and be allowed to use it. By default the panel listens on `0.0.0.0:8181`, so open `http://SERVER_IP:8181` from your LAN. Create the administrator account on first access.

> Initial administrator setup is allowed from any IP that can reach the panel. Before setup, restrict the panel port to a trusted network; do not expose an uninitialized admin panel to the public Internet.

### Panel port conflicts

Interactive installation prompts for a different port when the default is occupied. You can also set the listen address and port explicitly:

```sh
curl -fsSL https://raw.githubusercontent.com/ringer520/viya-proxy/main/install.sh | VIYA_INSTALL_LISTEN=0.0.0.0:8182 sh
```

If a host firewall is enabled, allow LAN access to the panel TCP port and the web listener ports you use, commonly 80 and 443.

### Service management and updates

```sh
sudo systemctl status viya.service
sudo journalctl -u viya.service -f
sudo systemctl restart viya.service
```

The panel can check for and install updates on systemd Linux. Update Docker installations by pulling the new image.

## Docker installation and updates

The database, certificates, static website files, Caddy data, logs and master key live under `/var/lib/viya` in the container. Bind **one host directory** there:

```sh
sudo mkdir -p /opt/viya/data
cd /opt/viya
```

Create `compose.yaml`:

```yaml
services:
  viya:
    image: ringer520/viya-proxy:latest
    container_name: viya
    restart: unless-stopped
    ports:
      - "8181:8181"       # Panel
      - "80:80"           # HTTP sites and HTTP-01 challenges
      - "443:443"         # HTTPS sites
      - "443:443/udp"     # HTTP/3 (optional)
    volumes:
      - /opt/viya/data:/var/lib/viya
```

```sh
docker compose up -d
docker compose logs -f viya
```

Open `http://SERVER_IP:8181`. If the panel port is occupied, change the host side of the mapping, for example `18181:8181`. Resolve conflicts on host ports 80/443 or adjust the mappings. HTTP-01 validation requires public access to port 80; use DNS-01 when that is unavailable. Host mappings must match the listener ports used by your sites.

Pull and recreate the container to update:

```sh
docker compose pull
docker compose up -d
```

Back up `/opt/viya/data` before upgrading and keep its `master.key`; saved DNS credentials cannot be decrypted without the original key. Do not use `docker compose down -v` for an update, because it removes Docker volumes.

## Backups and restore

Installed helpers live in `/opt/viya/scripts/` and require Python 3.11+. Ordinary backups include an online SQLite copy, managed websites, certificates and runtime files; the master key and access logs are excluded.

Run as root (prepend `sudo` for a regular user):

```sh
sh /opt/viya/scripts/backup-linux.sh
# Full migration backup: stop the service briefly, include the key, then restart.
VIYA_BACKUP_INCLUDE_KEY=1 VIYA_BACKUP_STOP_SERVICE=1 sh /opt/viya/scripts/backup-linux.sh
```

Archives in `/var/backups/viya/` have a file manifest, SHA-256 checksums and a key fingerprint. Full archives contain accounts, credentials and private keys, are not additionally encrypted, and have mode `0600`. Store a protected off-host copy. Website roots outside the data directory need separate backups.

```sh
sh /opt/viya/scripts/restore-linux.sh --verify /path/to/backup.tar.gz
VIYA_RESTORE_KEY=1 sh /opt/viya/scripts/restore-linux.sh --dry-run /path/to/backup.tar.gz
VIYA_RESTORE_KEY=1 sh /opt/viya/scripts/restore-linux.sh /path/to/backup.tar.gz
```

Verification and dry runs leave the installation unchanged. Restoring first backs up current data, validates files and SQLite, then checks service readiness; failure restores the previous data and key. These restore scripts target systemd installations. Install Viya on the destination first and keep the same data-directory path. Omit `VIYA_RESTORE_KEY=1` for ordinary archives and supply the original master key separately. Paths can be configured with `VIYA_DATA_DIR`, `VIYA_MASTER_KEY_FILE` and `VIYA_BACKUP_DIR`.

Encrypted panel configuration exports exclude panel accounts, account passwords, sessions and website files, but include business credentials and certificates. Docker users should stop the container and back up its whole mounted data directory.

Settings → Storage shows data usage and retention. Log rotation remains under Logs. Cleanup removes expired statistics, recent access records beyond the cap and expired sessions; it preserves websites, certificates and recovery points. SQLite reuses freed pages rather than immediately shrinking its file.


## One-line OpenWrt installation

For procd-based OpenWrt on amd64, arm64 or armv7 (MIPS is not supported), run as root:

```sh
wget -qO- https://raw.githubusercontent.com/ringer520/viya-proxy/main/install.sh | sh
```

The entry script detects OpenWrt. The dedicated installer is `deploy/install-openwrt.sh`; download it to `/tmp/viya-install.sh` and run `VERSION=v0.3.1 VIYA_INSTALL_PORT=8181 sh /tmp/viya-install.sh` to pin a release and initial port. It verifies SHA-256, checks free space and port conflicts, enables procd startup, and checks readiness.

New installations listen on `0.0.0.0:8181`: open `http://router-LAN-IP:8181`. Persistent data: `/opt/viya/data`; master key: `/etc/viya/secrets/master.key`. Use persistent storage for `/opt`; allow space for binaries, data and upgrade backups, and roughly 128 MiB free in `/tmp` for download/extraction. Requirements include curl/wget, tar, sha256sum, netstat, UCI and HTTPS CA certificates. The installer does not open WAN firewall rules.

New services use UCI: `uci set viya.main.listen='0.0.0.0:8182'; uci commit viya; /etc/init.d/viya restart`. Existing custom init scripts and data paths are preserved. Upgrade copies are stored under `/opt/viya/backups/install-*`. Linux/systemd installation control flow is checked in isolation. OpenWrt scripts pass POSIX shell syntax checks; real OpenWrt installation and device compatibility have not yet been validated on hardware.

## Build from source

Development requires Go 1.27+, Node.js 22 and pnpm 9. Vite output is embedded in the Viya binary, so Node.js and an external database are not required at runtime. The control plane and customized Caddy engine are built as separate binaries.

```sh
pnpm --dir web install --frozen-lockfile
pnpm --dir web build
go build ./cmd/viya
```

Build Linux packages from Windows PowerShell:

```powershell
./scripts/build-linux.ps1
./scripts/package-linux.ps1
```

Pushing a `v*` tag starts GitHub Actions. It builds Linux release archives and SHA-256 files for `amd64`, `arm64` and `armv7`, then publishes a multi-architecture Docker image. Release notes live in `docs/releases/` and are used as the GitHub Release description.

## Uninstall

These systemd commands remove the program and service but preserve the database, certificates, website files and master key:

```sh
sudo systemctl disable --now viya.service
sudo rm -f /etc/systemd/system/viya.service
sudo rm -rf /etc/systemd/system/viya.service.d /opt/viya
sudo systemctl daemon-reload
```

Only when you no longer need the data, separately remove `/var/lib/viya`, `/etc/viya`, `/var/log/viya` and `/var/backups/viya`. These locations may contain the database, certificates, website files, the key used to decrypt DNS credentials, and backups.

For Docker, run `docker compose down`; persistent data remains in `/opt/viya/data`. Remove that directory separately only when you intend to permanently delete it.

## License

Viya is released under the [MIT License](LICENSE).
