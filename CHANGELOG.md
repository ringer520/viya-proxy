# 更新日志

每个发布版本的完整说明在 [`docs/releases/`](docs/releases/)。这里只保留一行索引，方便快速跳转；Release 页面的正文也取自同一份文件。

## 未发布

暂无。

## v0.6.7

[完整说明](docs/releases/v0.6.7.md) — 修复 v0.6.6 升级回归：旧 generation 记录匹配不上导致轮转日志被重采、流量重复计数。新增旧游标兼容与按文件类型过滤；附数据重建说明。无 schema 变更。

## v0.6.6

[完整说明](docs/releases/v0.6.6.md) — 修复流量统计「重启 / 升级后归零」：Caddy 日志按 logger 分流、采集游标改为按文件独立并自愈越界、配置恢复不再清空 L4 流量、概览展示保留期内累计值。无 schema 版本变更，`traffic_files` 启动时自动补列。

## v0.6.5

[完整说明](docs/releases/v0.6.5.md) — 修复文件与共享生成错误挂载地址、消除监听默认值文档矛盾、自更新不再吞掉 Caddy 引擎替换失败；文档版本纳入发布门禁；补齐社区文件与仓库门面。无数据库迁移。

## v0.6.4

[完整说明](docs/releases/v0.6.4.md) — 安全重启（受管环境检测 + 退出码 75）、配置变更审计 `config_audit`、请求 ID 贯穿。schema 6 → 7。

## v0.6.3

[完整说明](docs/releases/v0.6.3.md) — 双监听（具体局域网 IP 自动补回环，同一进程一套会话）、命令行参数边界、单实例锁、启动自检与可观测性。无迁移。

## v0.6.2

[完整说明](docs/releases/v0.6.2.md) — 移除配置历史功能与死代码、备份瘦身、恢复点保留 5 个。schema 5 → 6，`config_revisions` 被删除。

## v0.6.1 / v0.6.0

[0.6.1](docs/releases/v0.6.1.md) · [0.6.0](docs/releases/v0.6.0.md) — 「文件与共享」产品化改造，替换旧文件服务；公网 HTTPS WebDAV 与局域网 NAS 反代。

## v0.5.x 及更早

[v0.5.3](docs/releases/v0.5.3.md) · [v0.5.2](docs/releases/v0.5.2.md) · [v0.5.1](docs/releases/v0.5.1.md) · [v0.5.0](docs/releases/v0.5.0.md) · [v0.4.0](docs/releases/v0.4.0.md) · [v0.3.1](docs/releases/v0.3.1.md)
