# Compact interface, logs and password-free service configuration

本轮修改并发布：
- 各页去除重复大标题和常驻说明，保留面包屑、紧凑操作栏、区块标题；帮助收进问号弹层。
- Web 搜索移到站点列表标题旁；证书来源与日志图标独立一行，按同一列宽对齐；手机宽度无横向溢出。
- 菜单改为「日志」，包含访问日志、Viya/Caddy 程序日志、日志配置。程序日志 DEBUG/INFO/WARN/ERROR；访问日志保留 INFO 全量用于统计。访问与程序日志分别配置单文件 1–1024 MiB，上限轮转；保留归档 1–30 份、1–365 天。
- 程序日志只接收 Caddy 受控字段，不保存原始管理请求头；URL 用户信息、查询和片段脱敏。API 请求细节是 DEBUG，默认 INFO 不产生轮询噪声。
- 导入导出使用 v3 JSON，不导出/恢复 users、sessions；目标管理员及登录会话保持不变。仍保存业务规则、DNS 凭据、证书与偏好。便携凭据使用独立随机传输密钥，不导出安装 master.key。本轮当时为免密码文件；后续改为备份密码 + scrypt + AES-256-GCM 整包加密（见 `hardening-and-performance.zh-CN.md`），旧免密码文件仍可导入。
- 旧 v2 加密文件拒绝导入并提示使用新版配置。导入前自动生成受限权限的恢复文件，失败恢复业务配置与运行表。
- DDNS 自动更新、TCP/UDP 转发统一滑动开关；DDNS 列表使用 Network 图标。转发开关校验旧版本和端口冲突，应用失败恢复原状态。
- 移除主组件中已不用的访问日志缓存和 SSE 连接，由日志工作区按需读取，访问表显示所属站点。

验证：Vue 类型检查、Go 全部测试、Linux/Windows 构建；真实本地 Caddy 验证日志级别/大小、WARN 不丢访问记录、跨密钥恢复（本轮为免密码导出，后续版本改为备份密码加密）、账号与 CSRF 会话不变、账号表注入拒绝、TCP 开关与旧版本保护；浏览器确认无 h1、日志配置页签、手机宽度与日志图标同列。

依赖：日志轮转使用 lumberjack v2.2.1（https://github.com/natefinch/lumberjack）。

发布完成（2026-10-30 21:00，Asia/Shanghai）：
- Debian 192.168.1.16，保留现有 systemd 配置及 Caddy 二进制，仅替换 Viya 应用。
- 恢复备份：/var/backups/viya/before-compact-logs-20261030T130036Z.tar.gz（0600，包含发布前数据与应用）。
- 应用 SHA-256：396f4c1451425f6ed2b44e203f728a64e0fa39786cee42154606ffbae76d4396。
- 公网 https://viya.santi.fun/ 的 HTTPS 证书校验和健康接口通过；/assets/index-D5wZa9hr.js 与本地构建逐字节哈希一致；未登录访问程序日志返回 401。
- 管理员配置哈希不变；3 个站点、1 条证书申请、1 个 DNS 凭据、2 个 DDNS 任务保留，数据库 integrity_check / foreign_key_check 通过，systemd active、Caddy ready。
