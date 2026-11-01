# Viya

<p align="center"><img src="assets/logo.png" alt="Viya" width="112"></p>

<p align="center"><strong>自托管的 Caddy 可视化管理面板</strong><br>用一个控制台管理反向代理、静态网站、TLS 证书、动态 DNS 和 TCP/UDP 转发。</p>

<p align="center">简体中文 · <a href="README.en.md">English</a></p>

## 快速导航

- [界面预览](#界面预览) · [功能概览](#功能概览)
- [Linux 安装](#linux-安装) · [Docker 安装与更新](#docker-安装与更新)
- [备份与恢复](#备份与恢复) · [OpenWrt 手动安装](#openwrt-手动安装)
- [从源码构建](#从源码构建) · [卸载](#卸载)

## 界面预览

运行概览展示服务器资源、24 小时流量趋势和需要关注的服务状态。其他管理页面可展开查看。

![Viya 运行概览](docs/screenshots/02-overview.png)

<details>
<summary>展开查看其他界面</summary>

| Web 服务 | TCP / UDP 端口转发 |
| --- | --- |
| ![Web 服务](docs/screenshots/03-proxy-sites.png) | ![端口转发](docs/screenshots/04-layer4.png) |

| 动态 DNS | SSL 证书 |
| --- | --- |
| ![动态 DNS](docs/screenshots/05-ddns.png) | ![SSL 证书](docs/screenshots/06-certificates.png) |

| DNS 凭据 | IP 封禁 |
| --- | --- |
| ![DNS 凭据](docs/screenshots/07-dns-providers.png) | ![IP 封禁](docs/screenshots/08-ip-bans.png) |

| 日志 | 配置历史 |
| --- | --- |
| ![日志](docs/screenshots/09-logs.png) | ![配置历史](docs/screenshots/10-config-history.png) |

</details>

## 功能概览

- **Web 服务**：反向代理、静态网站和 URL 重定向；按 HTTP/HTTPS 监听管理，支持端口、路径、克隆、启停及安全策略。
- **静态文件管理**：通过面板上传、浏览、下载、重命名和删除网站文件；删除站点时可选择是否同时删除文件。
- **TLS 证书**：申请和自动续期证书、查看申请进度、上传自定义证书；站点可复用匹配的泛域名证书。
- **动态 DNS**：支持 A、AAAA、CNAME；可选网卡地址或公网 IP，支持定时更新和手动更新。
- **DNS 凭据**：支持 Cloudflare、DNSPod、阿里云等服务商，凭据使用独立主密钥加密存储。
- **TCP / UDP 转发**：管理 TCP、UDP 或双协议规则，并查看连接与流量统计。
- **访问安全**：IP/CIDR 封禁、访问控制、登录限流、自动封禁、默认安全配置和安全响应头。
- **运行维护**：CPU、内存和磁盘监控；访问日志、程序日志、流量趋势、配置历史、备份和恢复。
- **账户与界面**：管理员和只读账号、中文/英文界面、多种主题及简洁显示模式。

## Linux 安装

一键安装适用于使用 **systemd** 的 Linux，提供 `amd64`、`arm64` 和 `armv7` 版本。安装会校验 Release 包的 SHA-256、创建服务并设置开机自启。需要 `curl` 或 `wget`、`tar`、`python3` 和系统 CA 证书；安装器会尝试通过系统包管理器补齐依赖。

使用 root 运行：

```sh
curl -fsSL https://raw.githubusercontent.com/ringer520/viya-proxy/main/install.sh | sh
```

普通用户也可执行同一命令；安装器会通过 `sudo` 重新下载并运行。普通用户需要预先安装且获准使用 `sudo`。安装完成后，面板默认监听 `0.0.0.0:8181`，可从局域网访问 `http://服务器IP:8181`。首次打开时创建管理员账号。

> 首次初始化允许从可访问面板的任意 IP 创建管理员。初始化前请将面板端口限制在可信网络，避免把未初始化的管理面板暴露到公网。

### 面板端口冲突

交互安装检测到默认端口被占用时会提示选择新端口，也可显式指定：

```sh
curl -fsSL https://raw.githubusercontent.com/ringer520/viya-proxy/main/install.sh | VIYA_INSTALL_LISTEN=0.0.0.0:8182 sh
```

启用了系统防火墙时，请允许局域网访问面板 TCP 端口，以及所用 Web 服务端口（通常为 80/443）。

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

### 从旧版双卷 Docker 迁移

旧版可能将业务数据放在 `/var/lib/viya`，主密钥单独挂载到 `/etc/viya/secrets`。先停止旧容器但保留容器和卷，然后复制数据：

```sh
docker compose stop
sudo mkdir -p /opt/viya/data
# 用 docker volume ls 中的实际卷名替换 <旧业务数据卷>
docker run --rm -v <旧业务数据卷>:/from:ro -v /opt/viya/data:/to alpine sh -c 'cp -a /from/. /to/'
# 用 docker ps -a 中的旧容器名替换 <旧容器名>
docker cp <旧容器名>:/etc/viya/secrets/master.key /opt/viya/data/master.key
```

确认新目录包含数据库和 `master.key`，再改用上面的单目录 Compose 映射。运行 `docker compose down`（不要加 `-v`）后执行 `docker compose up -d`。登录检查 DNS 凭据、证书和站点文件后再清理旧卷。

## 备份与恢复

完整 Linux 备份脚本包含在 Release 包中。下载并解压匹配服务器架构的包后运行：

```sh
sudo sh ./viya-linux-amd64/scripts/backup-linux.sh
```

备份写入 `/var/backups/viya/`，包含一致性 SQLite 副本和运行数据，但**不包含主密钥**。请另外安全保存 `/etc/viya/secrets/master.key`。恢复时必须提供同一套安装的主密钥：

```sh
sudo sh ./viya-linux-amd64/scripts/restore-linux.sh /var/backups/viya/viya-时间戳.tar.gz
```

面板配置导出适合迁移业务配置，不包含账号、密码或会话，不能代替完整备份。Docker 用户应停止容器后复制整个 `/opt/viya/data` 至其他设备或安全位置。

## OpenWrt 手动安装

OpenWrt 使用 procd，不适用 systemd 一键安装脚本。下载对应架构的 Release 包并解压，将二进制和服务脚本复制到设备：

```sh
mkdir -p /opt/viya/bin
cp viya-linux-armv7/bin/viya-linux-armv7 /opt/viya/bin/viya
cp viya-linux-armv7/bin/caddy-linux-armv7 /opt/viya/bin/caddy
cp viya-linux-armv7/deploy/openwrt/viya /etc/init.d/viya
chmod +x /etc/init.d/viya
/etc/init.d/viya enable
/etc/init.d/viya start
```

示例 procd 服务默认绑定 `127.0.0.1:8181`；远程访问请使用 LuCI 反代或 SSH 隧道。请按路由器 CPU 和内存评估证书与转发负载。

## 从源码构建

开发环境需要 Go 1.27+、Node.js 22 和 pnpm 9。前端嵌入 Viya 二进制，运行时不需要 Node.js 或外部数据库；控制面板与定制 Caddy 分别构建。

```sh
pnpm --dir web install --frozen-lockfile
pnpm --dir web build
go build ./cmd/viya
```

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
