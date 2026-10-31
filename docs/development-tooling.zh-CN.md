# Viya 开发前插件、Skill 与工具调研

调研日期：2026-10-29。范围：Go/Gin、Vue/TypeScript、SQLite、自定义 Caddy、Linux 原生部署。本文是选型建议；本轮未安装软件、Skill 或修改全局配置。Context7 已发起插件安装建议，需用户完成连接后才能使用。

## 1. 结论与最小组合

先准备 Linux 验证环境、Go、自定义 Caddy 和项目级质量检查。插件只新增 Context7；复用已安装 GitHub。Skill 优先考虑 security-best-practices；浏览器自动化 Skill 按实际调试方式再装。项目约束通过简短 AGENTS.md 固化，专项 Skill 在出现重复工作流后编写。

插件用于接入能力或资料；Skill 提供工作流程；命令行工具负责真正的构建、检查与测试。这三者不能互相替代。官方说明：[Codex customization](https://learn.chatgpt.com/docs/customization/overview)。

## 2. 本机检查结果

| 项目 | 本次观察 | 处理建议 |
| --- | --- | --- |
| Node.js / npm | v24.21.0 / 11.19.0 | 先沿用，初始化时核对 Vite 等依赖的 engines |
| Git、Python、rg、curl.exe、SSH | 命令可用 | 复用 |
| pnpm | 命令可找到，位于 Codex 缓存运行时 | 若选用 pnpm，固定项目 packageManager 版本，验证可独立安装和运行，避免依赖 Codex 私有路径 |
| Go、caddy、xcaddy | PATH 中未找到 | 开发前补齐；不据此断言机器完全未安装 |
| WSL | wsl.exe 存在，但命令报告 WSL 未安装 | 准备 WSL2 或实际 Linux 主机；安装 WSL 可能涉及系统组件与重启 |
| golangci-lint、govulncheck、gh、sqlite3、Air | PATH 中未找到 | 按下文优先级补充 |
| GitHub 插件 | 插件目录返回 installed=true，工具可见 | 复用；具体仓库权限和 Git push 凭据仍需开发时验证 |
| 浏览器控制 | 当前会话已有 computer-use 能力 | 手工式 UI 验证可复用；项目自动化测试另用 Playwright Test |

## 3. 插件建议

| 优先级 | 插件 | 对本项目的价值 | 结论 |
| --- | --- | --- | --- |
| 推荐新增 | Context7 | 辅助检索 Vue、Vite、Gin 等库的文档和示例，减少凭记忆写 API | 目录已查到，尚未安装；先只加这一个 |
| 复用 | GitHub | 查看源码、issue、PR、CI，后续发布协作 | 已安装，不重复建议安装 |
| 后续按需 | Codex Security | 有代码和仓库后再评估专项分析能力及账号可用性 | 当前搜索未返回精确匹配，不能确认可安装；不阻塞开发 |

Context7 是资料入口，不能保证覆盖锁定版本，尤其 caddy-l4、DNS 和限流插件；遇到差异以锁定版本源码、官方文档和真实 Caddy 加载结果为准。项目文档查询只发送所需技术问题，不提交 DNS 密钥或完整敏感运行配置。

本项目暂不需要部署平台、设计平台或外部数据库插件。SQLite 和 Linux 文件系统可用本地工具检查，不需要为了查看数据安装数据库 MCP。

插件搜索结果并非完整目录；若后续确有缺失能力，可继续在[插件目录](https://chatgpt.com/plugins)查找。

Context7 来源：[上游仓库](https://github.com/upstash/context7)。

## 4. Skill 建议

使用官方 Skill Installer 列表核对了可安装名称，并阅读候选 Skill 的实际说明。注意列表的 installed 标记只反映其检查的目录：例如 openai-docs 本会话已经内置，不能据列表中的 false 再重复安装。

| Skill | 建议 | 用途与限制 |
| --- | --- | --- |
| security-best-practices | 优先安装候选 | 支持 Go、JavaScript/TypeScript；用于用户明确要求的安全默认实现或安全审查。适合管理认证、Cookie/CSRF、密钥处理。不是 Caddy 配置验证器，也不保证有 Vue 专项指南 |
| playwright | 按需安装 | CLI 驱动浏览器做页面调试、表单和截图；官方包装脚本使用 Bash，Windows 使用前需验证 shell 环境；可先复用现有 computer-use |
| security-threat-model | 架构/代码形成后按需 | 用于明确发起的威胁建模，关注管理面、Caddy Admin、DNS 密钥与用户流量边界；不是日常自动触发流程 |
| gh-fix-ci / gh-address-comments | 接入 GitHub CI/PR 后 | 工作流出现后再装；gh-fix-ci 依赖 GitHub CLI 与认证，安装 GitHub 插件不等于具备这些条件 |
| playwright-interactive | 暂缓 | 需要其指定的持久 JS REPL 等前置能力；不能因当前存在名称相似工具就视为已满足 |
| openai-docs、skill-installer、skill-creator | 复用当前内置能力 | 无需重复安装 |

官方候选来源：[openai/skills](https://github.com/openai/skills/tree/main/skills/.curated)。

当前官方 curated 列表未发现专门的 Go/Gin、Vue 或 Caddy Skill。这不表示社区没有；本轮不推荐未经验证的通用“全栈开发大全”包。

项目专项知识先放在 AGENTS.md 和 docs 中。出现稳定重复流程后，再考虑制作以下 Skill：

- viya-caddy-validation：按锁定模块生成配置，validate、load、真实请求、失败恢复验证。
- viya-linux-release：Linux 构建、前端内嵌、systemd、文件权限、备份与恢复验收。

这些只是拟议名称，当前不存在、未创建。不要把完整需求复制进每次自动触发的 Skill。

## 5. 开发工具与安装时机

| 时机 | 工具 | 具体用途 |
| --- | --- | --- |
| 开发前 | Go | 编译后端；内置 gofmt、go test、go vet；版本与 Caddy 插件兼容性一起确定 |
| 开发前 | xcaddy + 自定义 caddy | 构建并运行项目真正依赖的代理引擎，固定核心与插件版本 |
| 开发前 | 可用 Linux 环境 | WSL2、虚拟机或 Linux 主机任选其一，实际验证进程、权限、监听与 systemd；若使用 WSL，确认 systemd 支持和网络差异 |
| 初始化项目时 | golangci-lint | Go 静态检查，固定兼容的版本，从少量高价值规则开始 |
| 初始化项目时 | govulncheck | 检查 Go 依赖中实际涉及的已知漏洞；定期和发布前运行 |
| 初始化项目时 | TypeScript + vue-tsc | 对 Vue 单文件组件做类型检查；不能用 vite build 成功替代 |
| 初始化项目时 | ESLint + eslint-plugin-vue + Prettier | 规范和格式，明确分工，避免重复规则冲突 |
| 初始化项目时 | Vitest | 仅测试有意义的前端逻辑，如校验、错误映射与应用状态展示 |
| M2 界面完成时 | @playwright/test + Chromium | 少量持久端到端测试：初始化、登录、代理 CRUD、失败反馈；与交互式 Playwright Skill 分开 |
| 按需 | gopls | 若使用支持 LSP 的代码编辑器，提供跳转、诊断与补全；不假设 Codex 会自动使用它 |
| 按需 | Air | Go 开发热重启；先处理好 Caddy 子进程回收，避免每次重启残留占用端口 |
| 按需 | GitHub CLI gh | 终端 PR、Actions 和发布；已有连接器足够时可稍后装 |
| 按需 | sqlite3、jq、socat | 查看数据库、处理 JSON、TCP/UDP echo 联调；应用运行不依赖这些工具 |

前端工具优先装为项目 devDependencies，Go 工具及 Caddy 构建也记录固定版本，避免全局 latest 改变结果。运行服务器只需要发布包与服务配置，开发工具不随运行环境强制安装。

关键依据：

- [xcaddy 官方仓库](https://github.com/caddyserver/xcaddy)：定制 Caddy 构建。
- [golangci-lint 官方安装说明](https://golangci-lint.run/docs/welcome/install/)：选择项目兼容的安装方式与版本。
- [Go 漏洞管理](https://go.dev/doc/security/vuln/)：govulncheck 与漏洞数据库。
- [Vue TypeScript 指南](https://vuejs.org/guide/typescript/overview.html)：Vite 与独立类型检查。
- [Playwright TypeScript 文档](https://playwright.dev/docs/test-typescript)：运行浏览器测试不会替代类型检查。

## 6. Caddy 运行插件单独管理

这里的 Caddy 模块属于产品运行依赖，与 Codex 插件区分：

- M0/M1：先锁定并验证 Caddy HTTP/反代基础能力，同时确认后续插件的兼容版本。
- M3：接入 github.com/caddy-dns/cloudflare；后续增加 tencentcloud、alidns。
- M5：接入 github.com/mholt/caddy-l4，针对 TCP 和 UDP 做真实验证。
- M6：评估并锁定一个限流模块，确认 JSON schema、版本兼容和运行行为；此时尚未选定，不把名字写成已安装依赖。

可在环境验证时尝试完整定制构建，以尽早发现模块冲突；里程碑功能验收仍分步进行。不要在开发者机器和 Linux 服务器上各自无版本地重建。

## 7. 比增加 Skill 更直接的项目约束

初始化代码仓库时创建简短 AGENTS.md，包含：

1. Linux 原生优先、Docker 后置，以及架构方案路径。
2. 实际可运行的构建、格式、类型、测试命令，未实现的命令不写成现成能力。
3. Caddy 配置修改必须经过真实引擎验证；mock 只覆盖控制面逻辑。
4. 发布串行化，期望状态与实际状态分开，加载失败保持旧代理。
5. DNS 密钥、私钥、数据库和运行配置不入 Git；日志与默认导出脱敏。
6. Linux 未实测、ACME 未签发、云 DNS 未更新时如实标记未验证。

首批质量检查建议：Go 格式/静态检查/单元测试、前端 lint/vue-tsc/build，加一个真实 Caddy HTTP 集成测试。涉及并发调度和发布时再加 race 检查；race 检查单独准备支持的工具链，不为了普通发布构建强制引入 CGO。界面上线后加关键 Playwright 流程。

## 8. 执行顺序

1. 确定 Linux 验证环境并准备 Go；复用现有 Node/Git。
2. 用户完成 Context7 插件建议流程；不影响独立的本地工作。
3. 如需安装 Skill，首选 security-best-practices；Playwright CLI Skill 在浏览器调试需要时追加。
4. 初始化仓库、版本锁、AGENTS.md 与质量检查脚本。
5. 构建自定义 Caddy，先跑通 HTTP 转发和失败恢复，再进入 UI 开发。

本轮结论不要求一次安装全部工具。真正提升准确性的依据是：版本有记录、文档对应实际版本、检查可重复执行、Linux 与 Caddy 经过真实运行验证。


