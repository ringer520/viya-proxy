# Viya 本地开发与实施方案

编制日期：2026-10-29；实施状态更新于 2026-10-30。M0-M2 与 M3-M7 的主要控制面已实现。已完成浏览器首次初始化和站点真实转发、Linux 发布包安装与在线备份恢复演练；公网 DNS/ACME 及干净 Debian、整机重启等环境验收仍需按下文列出的条件完成。

## 1. 目标与默认范围

在当前 Windows 工作区开发，以 Linux 原生运行为首期交付目标，完成 Caddy 反向代理管理系统。前端中文，支持代理站点、证书、DNS 凭据、DDNS、TCP/UDP 转发、安全规则、日志及配置历史。Docker 打包留到 Linux 版本稳定后的独立阶段，不作为当前开发依赖或验收条件。

第一阶段以单管理员、单节点为范围。先完成“在界面创建 HTTP 代理 → Caddy 实际转发 → 重启后恢复 → 无效修改不影响已有代理”的闭环，再依次接入 HTTPS、DNS、DDNS 和四层代理。

本方案采用以下默认决策：

- Go + Gin；SQLite 使用 database/sql + modernc.org/sqlite，避免 Windows 本地开发依赖 CGO。通过 SQL migration 管理表结构。
- Vue 3 + TypeScript + Vite + TailwindCSS + shadcn-vue + Lucide；Pinia 保存会话与界面状态，Axios 请求后端。
- Caddy 自定义构建，插件与 Go 版本在项目初始化时做兼容性验证并锁定；不把需求中的 Go 1.22 当作固定工具链版本。
- 后端使用独立进程管理 Caddy，通过 Admin API 全量应用生成的 JSON。
- 首先接入 Cloudflare，再接入腾讯云 DNSPod 和阿里云。统一接口保留全部三家支持。
- 开发数据根目录为 ./data；Linux 默认数据目录为 /var/lib/viya，配置目录为 /etc/viya，日志目录为 /var/log/viya，均可配置。
- Linux 首先支持 amd64，预留 arm64 构建；交付压缩包、systemd unit、安装及升级说明。

## 2. 架构与部署形态

```text
浏览器
  ├─ 开发：Vite :5173 ── /api 代理 ── Go :8181
  └─ 发布：Go :8181 同时提供内嵌 SPA 和 /api
                                      │
                  SQLite ← 配置服务 / 发布协调器
                                      │
                             Caddy Admin API
                                      │
                              Caddy 数据平面
                        HTTP(S) / WebSocket / TCP / UDP

DDNS worker ── DNS Provider Adapter ── 云 DNS API
日志 worker ← Caddy JSON 日志 → 聚合统计 / SSE
```

### 本地开发

- 构建依赖 Node.js、Go、xcaddy；开发运行 viya 和自定义 Caddy 二进制。Linux 发布包内嵌前端，运行时无需 Node.js、Go 或 xcaddy。
- Windows 可进行前后端开发和基础验证；Linux 原生环境用于发布验收，可使用可用的 WSL2、虚拟机或 Linux 主机。交叉编译成功不替代 Linux 实际运行验证。
- 开发端口：管理后端 127.0.0.1:8181，Vite 127.0.0.1:5173，Caddy Admin 127.0.0.1:2019，HTTP 测试入口 :8080，HTTPS 测试入口 :8443，示例上游 127.0.0.1:9000。
- 用 app.localhost 测试代理。脚本用 curl --resolve 显式指定域名和入口，不依赖系统是否自动解析 *.localhost。
- 本地 HTTPS 用 Caddy internal issuer；信任本地根证书属于显式开发步骤，不自动改动系统信任库。
- 公网 ACME 单独验证：有真实域名和 DNS 权限后先走 staging；HTTP-01 需要公网 80 可达，DNS-01 无需开放验证端口，但需要 DNS TXT 写权限与传播时间。
- 本地端口 8080/8443 不等于公网 ACME 的挑战端口；公网部署需要 80/443 正确监听并可达，存在 NAT 时另行配置转发。

### Linux 原生部署（当前交付目标）

- Vue 构建文件通过 go:embed 打入 viya；发布包包含 viya 和自定义 caddy 两个二进制。真正单二进制需要嵌入 Caddy，暂不作为首期目标。
- systemd 管理 viya 服务；viya 管理 Caddy 子进程的启停、健康检查、异常重启退避和输出回收。停止服务时关闭整个进程组，避免残留 Caddy 占用端口；不同时启用独立的 caddy.service 管理同一个引擎。
- 使用专用非 root 用户 viya。初期通过 8080/8443 验证；正式绑定 80/443 时，在 systemd unit 中仅授予所需的 CAP_NET_BIND_SERVICE，验证能力传递到子进程，不要求整个服务以 root 运行。
- 二进制放在 /opt/viya/releases/<version>/，/opt/viya/current 指向当前版本；配置位于 /etc/viya/config.yaml，主密钥位于受限的 /etc/viya/secrets/；数据、证书和 Caddy 存储位于 /var/lib/viya/，日志位于 /var/log/viya/。
- 程序显式设置 Caddy 的数据和配置存储位置，不依赖登录用户 HOME；数据库、证书、运行配置和日志目录仅授予服务所需权限。
- 管理端口默认绑定 127.0.0.1:8181；本地使用或通过 SSH 转发访问。需要远程管理时配置 HTTPS。Linux 可选权限受控的 Unix socket 用于 Admin API；使用 TCP 时仅监听 127.0.0.1:2019。
- 提供前台运行、systemd 安装、启动、停止、状态、日志、备份、恢复和升级说明。升级前备份数据库及迁移相关数据；二进制回退不能替代数据库恢复。
- 防火墙和云安全组放行由部署者配置；程序显示监听失败原因，不自动修改主机网络规则。

### Docker 打包（后续独立阶段）

Linux 原生版本稳定后复用同一发布产物，增加镜像、数据卷、端口映射和容器进程生命周期适配。当前不编写 Dockerfile/Compose，不以容器行为约束原生版的端口和目录设计。

## 3. 原始需求的关键修订

| 原始内容 | 实施决策 |
| --- | --- |
| 保存数据库后直接调用 /load | 引入配置 revision、应用状态与启动对账，避免“保存成功”被误认为“运行成功” |
| websocket_support 开关 | Caddy 原生支持 WebSocket；首版默认支持，不展示无实际效果的开关。若以后要求禁止，明确实现 Upgrade 拒绝规则 |
| ssl_type=http-01 | 自动证书的挑战策略显式配置；如界面承诺 HTTP-01，就禁用其他挑战，不能把 Caddy 默认自动策略称为仅 HTTP-01 |
| 基础限流 | 反代流量由 Caddy 处理，需要额外限流模块；Go API 的限流只能保护管理 API |
| 所有 4xx/5xx 都算攻击 | 默认仅对明确认证失败等事件计数，按站点、IP、时间窗口封禁；普通 404/502 不直接视为攻击 |
| DNSPod secret_id/secret_key | 使用腾讯云 DNSPod 对应的 tencentcloud 模块，与旧 DNSPod Token/ID 类型区分 |
| 任意新增四层监听端口 | Linux 原生直接绑定主机端口，检查占用和权限；是否可从外部访问还受防火墙、NAT 与云安全组影响 |
| 凭据加密即可避免泄露 | 数据库加密之外，运行配置、Caddy autosave、日志、导出、备份也要按敏感数据处理 |
| 基础安全/WAF | v1 交付访问控制、限流和临时封禁；完整请求内容检测 WAF 另立里程碑 |

## 4. 配置发布与故障恢复

SQLite 中业务配置表示期望状态；Caddy 当前配置表示实际运行状态，两者通过 revision 关联。只允许后端发布协调器写 Caddy，界面不允许直接编辑运行中的任意 JSON。

推荐发布流程：

1. 接收修改并校验域名、端口、上游、凭据引用、权限和请求中的预期版本。
2. 在短数据库事务中更新期望状态并记录不可变 revision，标记 pending；事务提交后释放数据库写锁。
3. 单一发布 worker 按序处理 revision；构造稳定排序的完整 Caddy JSON 并计算摘要。明确处理或跳过被更新版本取代的任务。
4. 编译时检查已安装模块、重复域名、监听冲突和依赖；对候选配置运行 caddy validate。validate 不能证明 DNS 签发或上游可达。
5. 调用 POST /load，成功后记录 applied_revision，失败则保存脱敏错误。Caddy 会在配置加载失败时保留旧配置。
6. 超时不能直接当作失败：读取当前 Caddy 配置并比对摘要/规范化内容，判断是否已生效，再决定重试。
7. 前端同时显示期望版本、运行版本与应用错误，并提供重试和恢复到历史版本。恢复操作产生新 revision。
8. 生效后原子更新最后成功配置文件；启动时核对数据库、最后成功配置和 Caddy 运行状态，修复“Caddy 成功、数据库状态未更新”等崩溃窗口。

保存接口返回 202 与 revision_id；前端轮询或订阅应用状态。代理配置已加载与证书签发成功是独立状态：ACME 是异步任务，界面需要展示签发中和错误。

对并发修改使用 optimistic locking，过期版本返回 409；发布操作串行化。首版不做自动把所有无关失败修改回滚的隐式业务逻辑，用户可以修复期望状态或恢复到明确版本。

## 5. 模块设计

### 5.1 站点代理

- 域名归一化、IDN 转换、重复校验；精确域名优先于泛域名，未知 Host 返回明确默认响应。
- 一个代理站点支持多个域名；首版一个上游，数据模型预留多上游。
- 上游拆分 scheme、host、port；HTTP 与 HTTPS transport 分开生成，支持 IPv6。默认校验上游 TLS，不自动关闭证书验证。
- 请求头和响应头分开配置，支持 set/add/delete；禁止换行等非法值，不把上游 URL 路径直接当作 dial 地址。
- 本地首版仅 HTTP；随后增加自动 HTTPS、DNS-01、internal 和自定义证书。
- 管理 API/Caddy Admin 地址及明显自循环目标禁止作为上游；内网地址是此产品的正常用途，不统一禁止。

### 5.2 证书与 DNS 凭据

- 证书由 Caddy 申请和续期，Go 不另写 ACME 客户端。
- tls_mode：off、internal、acme_http01、acme_dns01、custom；泛域名的公网 ACME 申请要求 DNS-01。
- 凭据在 SQLite 中使用 AES-256-GCM，每次加密生成随机 nonce，AAD 绑定凭据标识、提供商和加密版本；保留 key_id 支持轮换。
- 主密钥来自独立文件或部署 secret，不与数据库放在同一备份包。密钥缺失时禁止静默生成替代密钥解密旧数据。
- 查询接口只返回凭据是否已配置；不返回密文或明文。更新接口明确区分保留、替换和清空。
- Caddy DNS 模块运行时需要可用凭据：首版允许内部 API payload 与受限运行文件包含必要值，但禁止进入普通日志、页面预览或默认导出。Caddy autosave 也按密钥文件保护。
- 自定义证书校验证书与私钥匹配、SAN 覆盖和有效期，使用随机文件名及受限目录；禁止任意服务器路径输入。

### 5.3 DDNS

- Provider Adapter 提供列出 zone、查找记录、按记录 ID 更新和连通性检测能力；DNS-01 与 DDNS 共用凭据模型，但不混淆 TXT 挑战和 A/AAAA 更新逻辑。
- 每个任务保存 zone_id、provider_record_id、相对记录名、A/AAAA、TTL、enabled、interval、last_observed_ip、last_applied_ip、next_run_at 和 last_error。
- IPv4/IPv6 使用可确认地址族的独立探测流程，校验确实得到相应公网 IP；不把普通双栈请求的结果直接当作 IPv6。
- IP 探测失败、响应不合法或无法确认变化时保持原记录；优先对首次或变化结果做二次确认。
- 首次运行先读云端记录；仅 IP 改变时更新，保持记录的其他属性。记录不存在时需要明确的创建策略。
- 同任务禁止重叠执行；设置超时、指数退避和抖动，控制每个提供商的调用频率。
- 不用会覆盖 zone 其他记录的更新方式；支持手动运行，审计每次结果。

### 5.4 TCP/UDP

- listen_address + listen_port + protocol + upstream；both 在编译器中展开为 TCP、UDP 两个监听。
- 根据实际插件版本验证 UDP dial 与监听写法，不能套用 HTTP reverse_proxy JSON。
- 对监听地址/地址族/协议/端口检查重叠，保留管理端口、Admin 端口、HTTP/HTTPS 和 HTTP/3 使用的端口。
- UI 显示主机实际监听地址、端口和加载错误；端口是否可从公网访问需独立验证。新增规则无需修改容器映射；低于 1024 的端口需要对应的运行权限。
- 首版做普通转发；TLS SNI 分流、透明代理、PROXY protocol 和源地址保留作为后续功能。

### 5.5 安全与日志

- 首次本地初始化创建管理员，不内置默认口令。密码 bcrypt；高熵服务端 session，HttpOnly、SameSite cookie；HTTPS 环境启用 Secure。
- Cookie 登录配套 CSRF 防护、Origin/Host 校验、登录限流和会话失效机制；开发通过 Vite 代理保持相同请求入口。
- 每站点 Basic Auth 使用 bcrypt，不记录明文；IP 策略支持 CIDR，明确“显式拒绝 → 有白名单时要求命中 → 认证 → 限流 → 代理”的顺序。
- 直接连接使用连接来源 IP；部署在 CDN/上级代理后，仅信任配置中的代理网段，结合实际 Caddy matcher 选择 client_ip，不能直接相信任意 X-Forwarded-For。
- 数据平面限流选择额外 Caddy 插件，里程碑开始时锁定实现并测试每 IP 窗口、突发和 429 行为。不要在 Go 控制面模拟业务限流。
- 临时封禁保存过期时间，批量合并后发布，避免每条日志触发一次全量 reload；保留管理来源豁免及手动解除方式。
- JSON access log 轮转、保留期限和体积上限；只存聚合统计与必要事件到 SQLite，不把全部请求长期写进业务库。
- SSE 使用有界缓冲、过滤与断线重连；慢客户端不会堵住日志读取。过滤 Authorization、Cookie、敏感查询参数。

## 6. 数据模型

所有业务表包含 created_at、updated_at；可编辑实体带 version，启用 SQLite foreign_keys、busy_timeout 与 WAL，并控制连接数量。

| 表 | 主要用途/字段 |
| --- | --- |
| users | username、password_hash、disabled |
| sessions | user_id、token_hash、expires_at |
| dns_providers | provider_type、encrypted_credentials、nonce、key_id、crypto_version |
| proxy_hosts | name、enabled、tls_mode、dns_provider_id、version |
| proxy_domains | proxy_host_id、规范化域名，全局唯一 |
| proxy_upstreams | proxy_host_id、scheme、host、port、TLS 参数 |
| header_rules | proxy_host_id、direction、operation、name、value |
| access_policies / access_policy_rules | 站点关联策略、CIDR、认证、限流 |
| custom_certificates | SAN、到期时间、受控证书/私钥引用 |
| layer4_rules | listen_address、listen_port、protocol、upstream、enabled |
| ddns_records / ddns_runs | 云端记录标识、调度状态、最近结果与历史 |
| config_revisions | 期望配置快照、hash、pending/applied/failed/superseded、脱敏错误 |
| system_state | desired_revision、applied_revision、schema_version |
| ip_bans | proxy_host_id、IP/CIDR、reason、expires_at |
| audit_events | actor、action、resource、result、request_id |

配置快照默认存凭据引用，不重复复制明文；可恢复性要同时考虑凭据版本。不得删除仍被站点、任务或保留版本引用的凭据，或需先处理这些引用。

数据备份通过 SQLite online backup 或等价一致性机制生成，不能在 WAL 活跃时只复制 viya.db。运行数据还包括 Caddy 证书存储、私钥、自定义证书及最后成功配置；备份和恢复必须覆盖这些状态，主密钥单独管理。

## 7. API 与界面

REST 前缀 /api/v1；统一错误结构 code/message/details/request_id。列表支持分页；密码和密钥不进入错误详情。

| 模块 | 主要 API |
| --- | --- |
| 初始化/认证 | GET /setup/status、POST /setup、POST /auth/login、POST /auth/logout、GET /auth/me |
| 代理 | /proxy-hosts CRUD、POST /proxy-hosts/{id}/test |
| DNS | /dns-providers CRUD、POST /dns-providers/{id}/test |
| DDNS | /ddns-records CRUD、POST /ddns-records/{id}/run、GET /ddns-records/{id}/runs |
| 四层 | /layer4-rules CRUD |
| 安全 | /access-policies CRUD、/ip-bans 查询与解除 |
| 配置 | GET /config/status、GET /config/revisions、GET /config/revisions/{id}、POST /config/revisions/{id}/restore、POST /config/reconcile |
| 日志/运行 | GET /logs/stream、GET /system/status、GET /system/capabilities |

GET /health/live 检查后端存活；GET /health/ready 检查数据库、Caddy 与配置协调状态。详细版本、插件和错误仅通过鉴权接口返回。

左侧导航：概览、代理站点、证书、DNS 凭据、DDNS、端口转发、访问控制、日志、系统设置。概览先展示实际可测的引擎状态、配置状态、站点数和错误；流量图在有真实聚合数据后接入。

站点编辑遵循“基础 → HTTPS → 访问控制 → 请求头”的渐进表单。提交后保留应用反馈；证书签发中不能显示为 HTTPS 已就绪。第一阶段不支持任意 Caddy JSON 编辑，保留脱敏配置预览。

## 8. 代码目录建议

```text
cmd/viya/                    # 程序入口
internal/api/                # 路由、handler、认证 middleware
internal/domain/             # 领域类型和校验
internal/service/            # 业务操作
internal/store/              # SQLite repository 和事务
internal/caddy/              # 配置编译、Admin client、发布协调器
internal/process/            # Caddy 子进程生命周期
internal/crypto/             # AES-GCM 与密钥管理
internal/dns/                # 提供商适配器
internal/ddns/               # 调度与更新
internal/logs/               # JSON 日志、SSE、聚合
internal/security/           # 封禁、策略
internal/web/                # go:embed 静态资源
migrations/                  # SQL migrations
web/src/                     # Vue 应用
build/caddy/                 # 自定义 Caddy 构建与版本锁
deploy/systemd/              # Linux systemd unit 和示例配置
scripts/                     # Windows 开发、Linux 构建/安装/升级/验收脚本
tests/integration/           # 真 Caddy、HTTP/WS/TCP/UDP 测试
docs/                        # API、架构决策、操作手册
data/                        # 本地运行数据，排除出 Git
```

## 9. 实施里程碑与验收

| 阶段 | 交付 | 验收门槛 |
| --- | --- | --- |
| M0 环境与引擎验证 | Go/Vue 骨架、工具链锁、定制 Caddy、示例上游、开发脚本 | 列出所需模块；加载最小 JSON；HTTP 请求确实到达示例上游；Linux 原生最小链路运行成功 |
| M1 核心控制面 | SQLite migrations、管理员认证、代理 CRUD、编译器、revision 与恢复 | 创建/修改/删除能影响真实转发；无效配置保留旧运行状态；重启恢复；并发修改返回可解释结果 |
| M2 可用管理界面 | 初始化/登录、代理列表和编辑、应用反馈、基础日志 | 首启、代理创建与真实 HTTP 转发、DNS 凭据保存/不回显已通过浏览器；认证保护已实现；WebSocket 与其余表单流程仍需逐项验收 |
| M3 HTTPS 与 DNS | internal、HTTP-01、Cloudflare DNS-01、自定义证书 | 本地 TLS 测试通过；公网 staging 签发需真实域名环境；泛域名通过 DNS-01；错误权限可定位 |
| M4 DDNS 与更多 DNS | A/AAAA worker、运行历史、腾讯云 DNSPod、阿里云 | 假 Provider 测试变化/不变/超时；授权测试域名完成各家真实更新；不覆盖同 zone 其他记录 |
| M5 四层与访问控制 | TCP/UDP、Basic Auth、CIDR 策略 | Linux 主机 TCP/UDP echo 验证；端口冲突和权限错误可定位；认证挑战与白黑名单顺序正确 |
| M6 限流与临时封禁 | 固定限流插件、封禁窗口/过期/解除、日志聚合 | 业务入口返回预期 429；普通 404/502 不触发默认封禁；封禁自动到期并恢复 |
| M7 Linux 原生交付 | 发布压缩包、内嵌 SPA、systemd、健康检查、备份恢复、部署手册 | Debian 12 发布包安装、systemd 非 root 运行、80 端口监听、健康检查与完整数据恢复已通过；干净系统安装、重启/异常恢复与独立升级回退仍需验收 |
| M8 后续 Docker 打包 | 基于原生产物增加镜像、数据卷和容器部署说明 | 不属于当前交付范围；原生版稳定后单独安排 |

依赖顺序：M0 → M1 → M2 → M3 → M4；M5 依赖 M1/M2 和插件验证；M6 依赖日志和策略基础；M7 在各阶段持续维护 Linux 构建，最终集中验收。M8 不阻塞前述任何阶段。

首个可运行版本的停止点是 M2，同时交付最小 Linux 运行包和启动说明：真实代理已工作，状态清楚，可重启恢复。M7 完善正式安装与运维交付。公网 DNS 测试需要后续提供专用测试域名和最小权限凭据；凭据通过本地初始化或界面输入，不写入仓库。

## 10. 测试重点

- 单元测试集中于配置生成、域名/上游/CIDR 校验、加解密、DDNS 判断和发布状态机。
- 集成测试必须使用锁定版本的真实 Caddy；不能只用 mock Admin API 证明配置可运行。
- 故障场景：端口占用、模块缺失、无效 TLS 设置、Admin 超时但已成功、数据库写失败、Caddy 被终止、发布中重启、凭据失效、日志轮转。
- 网络验证包括 HTTP、上游 HTTPS 校验、WebSocket、TCP、UDP、IPv4/IPv6；按环境报告跳过项，不将未执行项目标记通过。
- Linux 验收覆盖文件权限、非 root 低端口绑定、systemd 启停与异常恢复、主机重启、备份恢复和升级；Windows 本地通过不代表 Linux 已通过。
- 外部 ACME/DNS 验收独立于日常测试，使用 staging 与专用记录；不在常规测试中修改真实生产 DNS。
- 低影响界面样式无需镜像式测试；保留少量关键浏览器流程覆盖真实用户操作。

## 11. 已知环境与当前实施状态

工作区为 `D:\\Codex\\viya`；Go 1.27、Node.js、pnpm 可用。Debian 12 VM 已在 PVE 上运行，管理地址为 `192.168.1.16`。Viya 已通过 systemd 以专用非 root 用户运行，前端内嵌在 Go 程序中。

已落地并部署到 Debian 12（`192.168.1.16`）：Go/Vue 内嵌 SPA、SQLite、代理站点 CRUD、Caddy 动态配置/校验/历史/启动对账、子进程恢复；首启管理员、bcrypt、过期服务端会话、CSRF 与登录限速；加密 DNS 凭据（安装版主密钥独立存放于 `/etc/viya/secrets/master.key`）；HTTP-01、DNS-01、内部 CA、HTTP-only、自定义 PEM 证书；自定义请求头、Basic Auth、CIDR 白黑名单与每 IP 限流；Layer4 TCP/UDP/both；DDNS A/AAAA 公网地址解析、Cloudflare/Tencent/AliDNS 适配、历史与定时 worker；滚动 JSON 访问日志、SSE、临时 IP 封禁与管理端解封；systemd 原生安装、构建/打包和一致性数据库备份/恢复脚本。

Debian 实测：既有 HTTPS 反代返回 200；Layer4 TCP/UDP echo 往返成功；自定义证书和 `app.localhost` 内部 CA TLS 请求均返回 200；HTTP-only 站点经过 80 端口提供访问；Fail2Ban 对真实重复 401 请求在阈值后返回 403；Caddy 访问日志产生 JSON 记录；强制结束 Caddy 子进程后 supervisor 自动拉起且原站点恢复 200；服务正常重启且配置 revision 显示 applied。Caddy 与 Vue 插件构建均通过。

本轮补充验收：在 Debian VM 安装最新发布包，systemd 下 API 返回 `ok=true`、`caddy_ready=true`；浏览器完成管理员首次创建、站点新增并观察配置 revision `applied`，VM 对新域名 Host 请求返回 200；保存测试用假 DNS token 后界面只显示凭据已保存；旧版数据目录主密钥迁移至 `/etc/viya/secrets/master.key`，迁移前后 SHA-256 相同且权限为 `0600`；使用独立密钥完成在线 SQLite/运行态备份和完整恢复，服务仍 active 且 ready，恢复后确认初始化状态和基线配置已还原；内部 CA 的安装版 Caddy JSON 明确关闭系统信任库自动写入，本机 TLS 请求返回 200；强制终止 Caddy 后进程 supervisor 自动重启，健康检查与原站点转发恢复。测试临时管理员、站点与假凭据均不保留。

尚需完成：真实 Cloudflare/DNSPod/AliDNS 凭据权限与 DNS 更新、真实域名 ACME staging/公网 80/443 连通性、公网 NAT/防火墙、WebSocket 握手与消息、所有前端表单流程、干净 Debian 安装、主机重启/异常恢复与升级回退、master.key 异地保管演练。未提供可用 DNS 域名/凭据时不执行真实 DNS 修改或公网签发。Docker 继续放在原生版之后。

## 12. 技术依据

- Caddy Admin API、/load 及失败恢复：https://caddyserver.com/docs/api
- Caddy 自动 HTTPS 与挑战条件：https://caddyserver.com/docs/automatic-https
- reverse_proxy、WebSocket 与 transport：https://caddyserver.com/docs/caddyfile/directives/reverse_proxy
- caddy-l4 及实验性说明：https://github.com/mholt/caddy-l4
- 四层监听地址与协议：https://github.com/mholt/caddy-l4/blob/master/docs/servers.md
- Cloudflare DNS 模块：https://github.com/caddy-dns/cloudflare
- 腾讯云 DNSPod 模块：https://github.com/caddy-dns/tencentcloud
- 阿里云 DNS 模块：https://github.com/caddy-dns/alidns
- 非标准限流模块说明：https://caddyserver.com/docs/modules/http.handlers.rate_limit

这些资料支持本方案的能力边界，不能替代 M0 中针对锁定版本的编译和运行验证。


