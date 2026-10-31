# 安全加固与长期运行优化

修改日期：2026-10-31
范围：`cmd/viya`、`internal/secretstore` 的调用方、`web/src`；不改变数据库结构，不改变现有接口路径。

## 一、安全

### 1. 配置备份改为密码加密

此前导出的 v3 文件把 DNS 凭据的传输密钥和密文放在同一个 JSON 里，等于可以自解密；文件同时包含 `custom-certs/` 下的证书私钥。现在：

- 导出时必须以至少 8 位备份密码（`POST /api/v1/config/export` 的 `password` 字段）加密；
- 密码经 scrypt（N=32768, r=8, p=1）派生 32 字节 KEK，整个 payload（表数据、文件、偏好、传输密钥）用 AES-256-GCM 加密，AAD 为 `viya:config-archive:v1`；
- 外层信封只保留 `format / created / encrypted / kdf / salt / nonce / ciphertext`，导出的文件用文本方式看不到域名、凭据或私钥；
- 密码不落盘，遗失后只能放弃该文件；
- 旧版免密码 v3 文件仍可导入（`payload` 字段路径保留），导入预览会返回 `encrypted:false`。

导入前的服务器本地恢复快照（`dataDir/backups/before-import-*.json`）仍为免密码，因为它与 `master.key`、证书私钥同处一个目录，0600 权限下不构成额外暴露，同时保证面板不可用时仍能人工取回。

### 2. 内部错误不再回传前端

`fail()` 现在对 5xx 统一记录完整错误到服务日志；若错误来自 SQLite、文件系统、网络或 Caddy 校验（`internalFailure`），响应只返回固定文案，避免泄露表名、列名、绝对路径和配置片段。调用方自己构造的业务错误（如“Caddy 仍未接受当前配置”）保持原文。

### 3. 登录不再可枚举用户名

用户名不存在时也执行一次 bcrypt 比较（使用启动时生成的哑哈希），消除“用户不存在约 60ms 返回”的时间差。

### 4. 站点/转发编辑使用乐观锁

`PUT /api/v1/proxy-hosts/{id}` 与 `PUT /api/v1/layer4-rules/{id}` 的 UPDATE 增加 `AND version=?`，0 行受影响时区分 404 与 409。前端编辑表单带 `version`，收到 409 后刷新列表并提示重新打开。此前 A 开着编辑弹窗、B 点了停用，A 保存会静默覆盖 B 的操作。

### 5. 导入路由不再按 URL 字符串放行

`requireAuth` 拆为 `requireAuth` / `requireAuthUnlocked`，导入路由在注册时显式选择不持 `configMu` 读锁，改路由名不会再导致中间件读锁与 handler 写锁互相等待。

## 二、功能正确性

- **写超时**：`WriteTimeout` 30s → 120s。此前 DDNS 手动更新（35s）和配置导入（90s）超过 30s 时响应被截断，用户看到“失败”后重试，同一笔操作执行两遍。
- **多 HTTPS 监听跳转**：HTTP 监听的 308 目标端口改为按站点自身 `listener_id` 对应的 HTTPS 端口，而不再统一用第一个 HTTPS 监听的端口。此前挂在 `:8443` 上的站点会被跳到 `:443`，命中 404 `Site unavailable`。
- **访问日志半行**：`readLogLines` 只在读到 `\n` 时推进 offset，Caddy 写到一半的行留到下次读取，不再解析失败即永久丢失。
- **站点保存错误分类**：按 SQLite 约束返回 409（域名重复）/400（外键、非空、CHECK）/500，不再一律提示“域名已存在”。
- **导入不再双重写文件**：删除只写不读的 `import-` 暂存目录，证书文件直接落盘。

## 三、长期运行

- **流量统计**：累计值改为 SQL 聚合（`GROUP BY site_id`），24 小时窗口带 `hour>=?` 条件并新增 `idx_traffic_hours_hour`；`traffic_hours` 保留 90 天，由 `watchTraffic` 每小时清理一次。
- **日志摄入**：新增 `traffic_files(path,size,mtime,offset,complete)` 索引，已完成的轮转文件（大小与 mtime 未变）直接跳过，不再每 2 秒对所有 `.gz` 重新解压头部。
- **配置历史**：`recordRevision` 写入后保留最近 50 条，页面仍只展示 10 条。
- **封禁触发的重载**：新增 `applyWorker`，访问日志触发的 `apply` 进入 500ms 合并窗口；一次攻击突发只产生一次 `caddy validate` + `POST /load`。
- **系统指标**：CPU 采样的 300ms 移到缓存锁外，并发请求由一个采样者服务，其余等待结果，多个标签页不再排队等锁。
- **DDNS 手动更新**：不再持 `ddnsMu` 达 35 秒。改为记录级 `ddnsRunning` 标记，网络请求期间调度器、导出和其他记录不受阻塞。
- **前端流量柱状图**：`trafficHourIndex` 每次刷新一次性算好每站点 24 个桶与峰值，模板只查表；此前每根柱子都会重新遍历 24 条并按 `find` 查找。

## 四、已知取舍

面板使用单连接 SQLite（`SetMaxOpenConns(1)`）。配置导入、流量入库等长事务期间，其他请求会排队，表现为导入时面板短暂“假死”。这是 SQLite 单写者的固有限制，当前选择是保持写入串行而不是放宽连接数；如需彻底消除，需要改为独立读连接池或迁出 SQLite。

## 五、验证

- `go build ./...`、`go vet ./...`、`go test ./... -count=1` 通过；
- `vue-tsc --noEmit` 与 `vite build` 通过；
- 备份相关单元测试更新为：加密往返、无密码/错误密码拒绝、弱密码拒绝、密文不含明文标记、旧免密码文件仍可导入。
