# Viya — Caddy Reverse Proxy Manager

Viya 是面向 Linux 原生部署的 Caddy 管理服务。Go + SQLite 控制面负责校验、保存和发布配置；Vue 3 中文界面嵌入 Go 程序；Caddy 作为受监督的子进程运行并通过 loopback Admin API 应用 JSON 配置。Docker 留待原生版本稳定后再打包。

## 已实现

- 代理站点 CRUD、自动 HTTPS、HTTP-01、DNS-01、内部 CA、HTTP-only 和自定义证书；支持上游请求头。
- Cloudflare、腾讯云 DNSPod、阿里云凭据 AES-256-GCM 加密存储。
- DDNS A/AAAA 公网地址探测，默认 5 分钟周期；三家 DNS Provider 适配、手动更新、运行历史。检测到同名同类型多值 RRset 时拒绝更新，避免覆盖其他地址。
- Caddy Layer4 TCP、UDP、TCP+UDP 端口转发，冲突校验。
- 站点 Basic Auth、CIDR 白名单/黑名单、每 IP 限流；访问日志、SSE 实时流及 Fail2Ban Lite。5 分钟内对同一站点/IP 达到 20 次 400/401/403 时临时封禁 15 分钟；404、502 不计入，管理端回环地址不自动封禁。
- SQLite 配置历史、启动对账、Caddy 进程异常恢复、首启管理员、bcrypt、服务器端会话、CSRF 与登录限速。
- Linux systemd 非 root 运行、amd64 构建、安装与数据库一致性备份/恢复脚本。

## 已在 Debian 12 VM 验证

工作目录测试 VM：`192.168.1.16`。已构建并部署发布包，systemd 下以非 root 用户运行，管理 API 健康状态和 Caddy 就绪状态均通过。已实测 HTTPS 反代 200、Layer4 TCP/UDP echo 往返、TLS none 的 HTTP 访问控制、Fail2Ban 从 401 到封禁 403、自定义证书 TLS 200、Caddy 配置验证、日志写入与服务启动恢复；通过浏览器完成首启管理员、站点新增/真实 HTTP 转发、DNS 凭据保存且密钥不回显。在线备份后执行完整恢复，恢复服务健康检查通过。前端 TypeScript 检查和 Go 构建通过。

外部真实 DNS 更新与公网 ACME 签发需要用户自己的域名和最小权限 DNS 凭据；本项目不带默认凭据，也未把没有执行的公网签发标记为通过。其他 DNS Provider 的真实授权、完整表单逐项 UI 验收、干净 Debian 安装及机器重启演练仍待补充。内部 CA 在非 root 服务中不会自动写入系统信任库，管理员需自行分发并信任根证书。

## 本地开发与构建

要求 Go 1.27+、Node.js 和 pnpm。Caddy 插件版本锁定于 `build/caddy/go.mod`。

```powershell
pnpm --dir web install
pnpm --dir web typecheck
pnpm --dir web build
& 'C:\Program Files\Go\bin\go.exe' build ./...
./scripts/build-linux.ps1
./scripts/package-linux.ps1
```

`build-linux.ps1` 生成 `bin/viya-linux-amd64` 和包含 Layer4、DNS、限流插件的 `bin/caddy-linux-amd64`。发布压缩包输出到 `release/`，运行时不需要 Node、Go 或 xcaddy。

## Debian 12 安装

解压发布包，进入包目录后运行：

```sh
sudo sh ./deploy/install-debian.sh
systemctl status viya
journalctl -u viya -f
```

管理界面/API 默认只绑定 `127.0.0.1:8181`。从可信工作站使用 SSH 隧道访问：

```sh
ssh -L 8181:127.0.0.1:8181 root@DEBIAN_HOST
```

首次访问创建唯一管理员账户，密码至少 8 位。Caddy Admin API 仅监听 loopback。服务默认站点监听 `:8080`，同时开放 `:80` 用于普通 HTTP 与 ACME HTTP-01；TLS 站点使用配置的站点监听端口。生产部署建议通过 systemd drop-in 将 `VIYA_HTTP_LISTEN=:80`，让 Caddy 使用标准 80/443 端口；NAT、防火墙和云安全组仍需按网络拓扑设置。

登录后可从侧栏左下角打开“面板设置”：管理界面监听端口可修改为 `1024–65535`，保存后服务会自动重启；管理员密码可在验证当前密码后修改，至少 8 位。通过域名反代访问管理面板时，修改监听端口后还需同步更新该代理站点的上游端口。

选择“Caddy 内部 CA”时，服务不会自动修改系统信任库。根证书位于 `/var/lib/viya/.local/share/caddy/pki/authorities/local/root.crt`；请通过安全渠道复制到需要访问站点的客户端，并按操作系统/浏览器要求手动信任。

## 备份与恢复

```sh
sudo sh ./scripts/backup-linux.sh
sudo sh ./scripts/restore-linux.sh /var/backups/viya/viya-YYYYMMDDTHHMMSSZ.tar.gz
```

备份使用 SQLite online backup API，并包含 Caddy 证书状态和自定义证书；压缩包权限为 0600，视为包含私钥的敏感文件。安装版主密钥单独存放在 `/etc/viya/secrets/master.key`，不进入备份；必须另外安全保管并在恢复前保留原密钥，否则数据库里的 DNS 凭据无法解密。旧版 `/etc/crpm/secrets/master.key` 和 `/var/lib/crpm/master.key` 会随数据迁移复制，DNS 凭据的加密关联数据也会在 Viya 首次启动时安全迁移。访问日志不进入备份。恢复前脚本会自动生成当前数据备份；旧版备份中的 `crpm.db` 也可直接恢复。

## 运行配置

| 环境变量 | 默认值 | 用途 |
| --- | --- | --- |
| `VIYA_LISTEN` | `127.0.0.1:8181` | 管理界面/API |
| `VIYA_HTTP_LISTEN` | `:8080` | Caddy 站点监听端口；HTTP-only 站点另在 `:80` 服务 |
| `VIYA_DATA_DIR` | `./data` | SQLite、证书与 Caddy 运行数据 |
| `VIYA_MASTER_KEY_FILE` | `$VIYA_DATA_DIR/master.key` | DNS 凭据加密主密钥；systemd 安装使用 `/etc/viya/secrets/master.key` |
| `VIYA_CADDY_BIN` | `./bin/caddy` | 定制 Caddy 可执行文件 |
| `VIYA_CADDY_ADMIN` | `http://127.0.0.1:2019` | Caddy Admin API，必须 loopback |
| `VIYA_COOKIE_SECURE` | `false` | 管理端经 HTTPS 提供时设为 `true` |
| `VIYA_TRUSTED_PROXIES` | `127.0.0.1,::1` | 可提供 `X-Forwarded-Proto` 的可信反代 IP/CIDR，逗号分隔 |

不要将管理端口或 Caddy Admin API 暴露到不可信网络。完整架构、需求验收状态和遗留的外部验收条件见 [实施计划](docs/implementation-plan.zh-CN.md)；管理员日常配置顺序和失败恢复路径见 [操作流程](docs/operation-flow.zh-CN.md)。



