# 贡献指南

感谢你愿意为 Viya 提交代码、文档或问题反馈。这是一个单人维护的业余开源项目，目标是**稳定、可自托管、不添运行时依赖**；提交前请先看下面几条约定，能省下往返沟通的时间。

## 先讨论，再动手

功能类改动请先开一个 issue 说明「要解决什么问题」和「为什么现有行为不够用」，尤其是涉及面板访问、Caddy 配置生成、认证/权限、数据库 schema 的改动。这些地方牵一发而动全身，直接提 PR 很可能被要求重写。

适合直接提 PR 的：修 bug、补测试、文档纠错、i18n、明显的边界校验缺失。

## 项目结构

| 路径 | 作用 |
| --- | --- |
| `cmd/viya/` | Go 控制平面：HTTP API、SQLite、Caddy 配置生成与进程监管 |
| `internal/web/dist/` | 前端构建产物，由 `go:embed` 嵌入，**不入库** |
| `web/src/` | Vue 3 + TypeScript 前端，`locales/en.json` 是中文键的英文词条表 |
| `deploy/` | systemd unit、Debian / OpenWrt 安装脚本 |
| `build/caddy/` | 自定义 Caddy 构建（L4 等插件） |
| `docs/releases/` | 每个 tag 一份版本说明，发布工作流直接读取 |
| `scripts/` | 构建辅助与门禁脚本 |

## 本地构建

需要 Go 1.27+、Node.js 22、pnpm 9。

```sh
pnpm --dir web install --frozen-lockfile
pnpm --dir web build          # 必须先执行，否则 go:embed 会失败
go build ./cmd/viya
```

## 提交前门禁

```sh
go vet ./...
go test ./...
golangci-lint run
pnpm --dir web typecheck
pnpm --dir web build
pnpm --dir web lint
pnpm --dir web test
python3 scripts/check_i18n.py
python3 -m unittest discover -s scripts -p 'test_*.py'
sh -n install.sh
```

新增或改动版本号时还要跑 `python3 scripts/check_release_version.py <tag>`：它会校验两份 README 的安装片段、`docs/项目说明.md` 里的「当前发布 / 最新版本说明」以及对应的 `docs/releases/<tag>.md` 是否一致。

## 几条硬约定

- **i18n**：界面用中文字符串作键（`t("保存成功")`），英文词条写进 `web/src/locales/en.json`。后端返回给前端的 `errors.New("中文")` 也要能被 `t()` 翻译，否则 `check_i18n.py` 会挂。
- **数据库变更**：一律走可回滚的迁移，递增 `schemaVersion`，并确认新表是否需要进配置导出/恢复点白名单。审计类、运行时统计类数据不要进导出。
- **安全**：所有输入在边界校验；SQL 值必须参数化，动态部分只允许硬编码白名单列名；不要把路径、表名、凭据或原始错误回显给客户端（500 用通用文案，细节只进日志）；不要新增 `localStorage` 里的令牌。
- **不改用户配置**：产品行为只告警、不自动替用户改站点或反代配置。
- **后台任务**：goroutine 必须监听 `a.ctx.Done()`，长任务不放在请求处理器里，配置应用走 `applyWorker` 去抖。
- **测试**：单测可以 mock 仓储层保持快速，涉及迁移/事务的行为用真实内存 SQLite；不要为了速度在集成测试里 mock 服务层。

## 提交 PR

- 一个 PR 只做一件事，标题用简短的中文或英文祈使句；
- 说明「改了什么 / 为什么 / 怎么验证」，附上跑过的门禁结果；
- 界面改动附一张脱敏截图（域名、IP、账号请打码）；
- 涉及 API 变动时，同步更新 `docs/项目说明.md` 与对应 release note；
- CI 的 `checks` 工作流必须全绿。

## 行为准则

保持友好、就事论事。维护者可能回复较慢，但会认真看每一条反馈。
