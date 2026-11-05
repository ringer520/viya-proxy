# Viya

<p align="center"><img src="assets/logo.png" alt="Viya" width="112"></p>

<p align="center"><strong>自托管的 Caddy 可视化管理面板</strong><br>用一个控制台管理反向代理、静态网站、TLS 证书、动态 DNS 和 TCP/UDP 转发。</p>

<p align="center">简体中文 · <a href="README.en.md">English</a></p>

## 快速导航

- [界面预览](#界面预览) · [功能概览](#功能概览)
- [Linux 安装](#linux-安装) · [Docker 安装与更新](#docker-安装与更新)
- [备份与恢复](#备份与恢复) · [OpenWrt 一键安装](#openwrt-一键安装)
- [从源码构建](#从源码构建) · [卸载](#卸载)

### 操作状态与访问保护

- 端口转发分别展示配置启停与实际监听核实结果；保存前支持检查端口冲突。监听正常不等于上游业务正常。
- DDNS 展示获取地址、比较、提交和服务商回读阶段，分别记录检查与实际修改时间。IPv6 可选择跟随网卡或固定地址；固定地址失效不会被静默替换。
- SSL 证书支持申请、自动续期、克隆与上传，关联使用统一显示在每项信息末行；站点诊断可检查访问链路。
- DNS 凭据支持只读验证、替换凭据和关联资源检查。读取通过不代表具有 DNS 写入权限。
- IP 规则分为手动、地区与自动封禁，可查询某地址命中的规则。修改面板自身的限制需在 120 秒内通过对应域名确认，否则恢复原规则；局域网入口可以取消。
- 日志支持服务端站点、关键词、时间及状态筛选，文件查询范围为当前文件尾部最多 2 MiB；配置历史保留最近 10 条操作摘要与操作者。

地区限制按连接到 Caddy 的来源地址判断；使用 CDN 或其他代理时，代理出口可能被地区规则拦截。现有规则不会因升级被自动解除。


## 界面预览

运行概览展示服务器资源、可切换时间范围的流量趋势和需要关注的服务状态。点击 Web 错误率可按站点和状态码定位近期异常；其他管理页面可展开查看。

![Viya 运行概览](docs/screenshots/02-overview.webp)

<details>
<summary>展开查看其他界面</summary>

| Web 服务 | TCP / UDP 端口转发 |
| --- | --- |
| ![Web 服务](docs/screenshots/03-proxy-sites.webp) | ![端口转发](docs/screenshots/04-layer4.webp) |

| 动态 DNS | SSL 证书 |
| --- | --- |
| ![动态 DNS](docs/screenshots/05-ddns.webp) | ![SSL 证书](docs/screenshots/06-certificates.webp) |

| DNS 凭据 | IP 封禁 |
| --- | --- |
| ![DNS 凭据](docs/screenshots/07-dns-providers.webp) | ![IP 封禁](docs/screenshots/08-ip-bans.webp) |

| 日志 | 配置历史 |
| --- | --- |
| ![日志](docs/screenshots/09-logs.webp) | ![配置历史](docs/screenshots/10-config-history.webp) |

</details>

## 功能概览

- **Web 服务**：反向代理、静态网站和 URL 重定向；按 HTTP/HTTPS 监听管理，支持端口、路径、克隆、启停及安全策略。
- **静态文件管理**：通过面板上传、浏览、下载、重命名和删除网站文件；删除站点时可选择是否同时删除文件。
- **TLS 证书**：申请和自动续期证书、查看申请进度、上传自定义证书；站点可复用匹配的泛域名证书。
- **动态 DNS**：支持 A、AAAA、CNAME；可选网卡地址或公网 IP，支持定时更新和手动更新。
- **DNS 凭据**：支持 Cloudflare、DNSPod、阿里云等服务商，凭据使用独立主密钥加密存储。
- **TCP / UDP 转发**：管理 TCP、UDP 或双协议规则；与 Web 服务分别展示近 30 天的请求或连接、上行、下行和趋势统计。转发流量在连接结束后记账。
- **访问安全**：IP/CIDR 封禁、访问控制、登录限流、自动封禁、默认安全配置和安全响应头。
- **运行维护**：CPU、内存和磁盘监控；访问日志、程序日志、流量趋势、配置历史、备份和恢复。
- **账户与界面**：管理员和只读账号、中文/英文、五套基础主题及三套原创二次元主题；配套导航、按钮与动效，尊重系统减少动态效果偏好。
- **统计与异常定位**：点击 Web 错误率，按站点、状态码、次数查看近期保留日志；在设置 → 存储中可确认清空所有流量统计并重新计数。原始日志、账户、证书与业务配置保留。

## Linux 安装

一键安装适用于使用 **systemd** 的 Linux，提供 `amd64`、`arm64` 和 `armv7` 版本。安装会校验 Release 包的 SHA-256、创建服务并设置开机自启；下载显示进度并支持重试和超时。需要 `curl` 或 `wget`、`tar`、`python3` 和系统 CA 证书；安装器会尝试通过系统包管理器补齐依赖。

使用 root 运行：

```sh
curl -fsSL https://raw.githubusercontent.com/ringer520/viya-proxy/main/install.sh | sh
```

也支持 `wget -qO- https://raw.githubusercontent.com/ringer520/viya-proxy/main/install.sh | sh`。

普通用户也可执行同一命令；安装器会通过 `sudo` 重新下载并运行。普通用户需要预先安装且获准使用 `sudo`。安装完成后，面板默认监听 `0.0.0.0:8181`，可从局域网访问 `http://服务器IP:8181`。首次打开时创建管理员账号。

> 首次初始化允许从可访问面板的任意 IP 创建管理员。初始化前请将面板端口限制在可信网络，避免把未初始化的管理面板暴露到公网。

### 面板端口冲突

交互安装检测到默认端口被占用时会提示选择新端口，也可显式指定：

```sh
curl -fsSL https://raw.githubusercontent.com/ringer520/viya-proxy/main/install.sh | VIYA_INSTALL_LISTEN=0.0.0.0:8182 sh
```

启用了系统防火墙时，请允许局域网访问面板 TCP 端口，以及所用 Web 服务端口（通常为 80/443）。

固定安装版本可使用 `curl -fsSL https://raw.githubusercontent.com/ringer520/viya-proxy/main/install.sh | VERSION=v0.3.1 sh`。环境变量应放在管道右侧。

### 服务管理与更新

```sh
sudo systemctl status viya.service
sudo journalctl -u viya.service -f
sudo systemctl restart viya.service
```

systemd Linux 安装可在“面板设置”中检查并执行 Viya 自更新。Docker 环境请通过拉取新镜像更新。

## Docker 安装与更新

数据库、证书、静态网站文件、Caddy 数据、日志和主密钥统一保存在容器内 `/var/lib/viya`。建议只映射**一个宿主机目录**：

```sh
sudo mkdir -p /opt/viya/data
cd /opt/viya
```

创建 `compose.yaml`：

```yaml
services:
  viya:
    image: ringer520/viya-proxy:latest
    container_name: viya
    restart: unless-stopped
    ports:
      - "8181:8181"       # 面板
      - "80:80"           # HTTP 网站及 HTTP-01 验证
      - "443:443"         # HTTPS 网站
      - "443:443/udp"     # HTTP/3（可选）
    volumes:
      - /opt/viya/data:/var/lib/viya
```

```sh
docker compose up -d
docker compose logs -f viya
```

访问 `http://服务器IP:8181`。面板端口冲突时可改宿主机侧端口，例如 `18181:8181`。宿主机的 80/443 如被占用，需解决冲突或调整映射；HTTP-01 验证要求公网可访问域名的 80 端口，否则使用 DNS-01。网站实际监听端口也需与宿主机映射一致。

更新镜像：

```sh
docker compose pull
docker compose up -d
```

更新前备份 `/opt/viya/data`，并保留其中的 `master.key`，否则已有 DNS 凭据无法解密。不要使用 `docker compose down -v` 更新，它会删除 Docker 卷。

## 备份与恢复

安装后的备份工具位于 `/opt/viya/scripts/`，需要 Python 3.11 或更新版本。也可使用 Release 解压目录中的 `scripts/`。

普通备份包含一致性 SQLite 副本、托管网站文件、证书和运行数据，默认不含主密钥及访问日志。主密钥 `/etc/viya/secrets/master.key` 需要单独保护。

```sh
sh /opt/viya/scripts/backup-linux.sh
```

完整迁移建议短暂停止服务并包含主密钥，脚本完成后自动启动原先运行的服务。以下命令以 root 运行，普通用户在命令前加 `sudo`：

```sh
VIYA_BACKUP_INCLUDE_KEY=1 VIYA_BACKUP_STOP_SERVICE=1 sh /opt/viya/scripts/backup-linux.sh
```

备份保存在 `/var/backups/viya/`，含文件清单、SHA-256 校验和及主密钥指纹。完整备份包含账户、凭据与私钥，未额外加密，文件权限为 `0600`，应复制到受保护的离机位置。数据目录外的网站根目录需要另行备份。

先校验备份，再演练恢复检查（两者均不修改现有安装）：

```sh
sh /opt/viya/scripts/restore-linux.sh --verify /path/to/viya-backup.tar.gz
VIYA_RESTORE_KEY=1 sh /opt/viya/scripts/restore-linux.sh --dry-run /path/to/viya-backup.tar.gz
```

恢复完整备份时显式允许使用包内密钥；恢复普通备份则省略 `VIYA_RESTORE_KEY=1`，并提供原主密钥：

```sh
VIYA_RESTORE_KEY=1 sh /opt/viya/scripts/restore-linux.sh /path/to/viya-backup.tar.gz
```

恢复会先备份当前数据，验证后替换数据，并检查 Viya/Caddy 就绪状态；启动失败会回退数据和密钥。脚本适用于 systemd 安装，目标机应先安装 Viya，并使用与备份相同的数据目录。可用 `VIYA_DATA_DIR`、`VIYA_MASTER_KEY_FILE`、`VIYA_BACKUP_DIR` 指定路径。

面板中的加密配置导出不含面板账户、账户密码、会话和网站文件，但含业务凭据与证书，不应公开分享。Docker 用户应停止容器后备份整个映射数据目录。

在“面板设置 → 存储”可查看占用并设置统计保留策略；日志轮转在“日志”中配置。手动清理只处理过期统计、近期访问记录及过期会话，不删除网站、证书和恢复点；SQLite 空闲页会复用，不承诺数据库文件立即缩小。


## OpenWrt 一键安装

支持使用 procd 的 OpenWrt **amd64、arm64、armv7**；不支持 MIPS。SSH 登录路由器，以 root 执行：

```sh
wget -qO- https://raw.githubusercontent.com/ringer520/viya-proxy/main/install.sh | sh
```

通用入口会识别 OpenWrt 并调用专用安装器。也可直接下载脚本，便于指定版本和端口：

```sh
wget -O /tmp/viya-install.sh https://raw.githubusercontent.com/ringer520/viya-proxy/main/deploy/install-openwrt.sh
VERSION=v0.3.1 VIYA_INSTALL_PORT=8181 sh /tmp/viya-install.sh
```

安装器验证 SHA-256，检查空间和端口，创建 procd 服务并启用开机自启。新安装默认局域网访问 **`http://路由器IP:8181`**，数据位于 `/opt/viya/data`，主密钥位于 `/etc/viya/secrets/master.key`。`/opt` 必须是持久化目录或已挂载的外部存储；请为程序、数据和升级备份保留充足空间，下载与解压还需要 `/tmp` 至少约 128 MiB 可用空间。小内存路由器建议使用外部存储或在独立 Linux 主机部署。

需要 `curl` 或 `wget`、`tar`、`sha256sum`、`netstat`、UCI 和 HTTPS CA 证书。缺少证书时，opkg 系统可执行 `opkg update && opkg install ca-bundle`；使用 apk 的 OpenWrt 可安装 `ca-certificates`。安装器不会自动开放 WAN 防火墙端口。

OpenWrt 的 80 端口默认由 LuCI 的 uhttpd 占用，而 Viya 的 Web 监听默认就是 80/443。安装器会指出占用该端口的进程并继续安装，不会中断；但在释放该端口或把 Web 监听改到其他端口之前，**已保存的站点无法对外服务**，表现为配置应用失败。安装结束时的 `ACTION REQUIRED` 段落会重复这项提示。两种处理方式：

```sh
# 让 LuCI 让出 80（之后 LuCI 在 8080）
uci set uhttpd.main.listen_http='0.0.0.0:8080' '[::]:8080'
uci commit uhttpd && /etc/init.d/uhttpd restart
# 或者保持 LuCI 不动，在面板「Web 服务 → 监听端口」里把 80/443 改为其他端口
```

新服务可通过 UCI 修改监听地址，再重启：

```sh
uci set viya.main.listen='0.0.0.0:8182'
uci commit viya
/etc/init.d/viya restart
logread -e viya
```

已有安装会保留原服务脚本及数据路径，升级前复制数据、密钥和程序到 `/opt/viya/backups/install-*`。旧版自定义服务可能仍使用脚本中的监听地址，请按现有配置调整。Linux/systemd 的安装和升级流程经过隔离模拟验证；OpenWrt 脚本已通过 POSIX Shell 语法检查，尚未做真机安装验证，需在实际设备确认固件、CPU、存储与网络兼容性。

## 从源码构建

开发环境需要 Go 1.27+、Node.js 22 和 pnpm 9。前端嵌入 Viya 二进制，运行时不需要 Node.js 或外部数据库；控制面板与定制 Caddy 分别构建。

```sh
pnpm --dir web install --frozen-lockfile
pnpm --dir web build
go build ./cmd/viya
```

前端产物输出到 `internal/web/dist/`，该目录由构建生成、不入库（已在 `.gitignore` 中忽略）。因此请务必先执行 `pnpm --dir web build`，否则 `go build` 会因 `go:embed` 找不到文件而失败。

Windows PowerShell 构建 Linux 包：

```powershell
./scripts/build-linux.ps1
./scripts/package-linux.ps1
```

推送 `v*` 标签会触发 GitHub Actions，构建 amd64、arm64、armv7 Linux 安装包与 SHA-256 文件，并发布多架构 Docker 镜像。版本说明位于 `docs/releases/`，也是 GitHub Release 正文。

## 卸载

以下命令删除 systemd 服务和程序，保留数据库、证书、网站文件及主密钥：

```sh
sudo systemctl disable --now viya.service
sudo rm -f /etc/systemd/system/viya.service
sudo rm -rf /etc/systemd/system/viya.service.d /opt/viya
sudo systemctl daemon-reload
```

确认不再需要数据后，再单独删除 `/var/lib/viya`、`/etc/viya`、`/var/log/viya` 和 `/var/backups/viya`。它们可能包含数据库、证书、网站文件、解密 DNS 凭据所需主密钥及备份。

Docker 卸载执行 `docker compose down`；持久数据仍在 `/opt/viya/data`。确认永久删除后再单独移除该目录。

## 协议

Viya 使用 [MIT License](LICENSE)。
