## 改动内容

<!-- 一个 PR 只做一件事。说明改了什么、为什么这样改。 -->

## 关联 issue

<!-- Closes #123，或「无」。功能类改动建议先开 issue 讨论。 -->

## 如何验证

<!-- 复现/验证步骤，以及实际结果。界面改动请附脱敏截图。 -->

## 门禁结果

- [ ] `go vet ./...` / `go test ./...`
- [ ] `golangci-lint run`
- [ ] `pnpm --dir web typecheck` / `build` / `lint` / `test`
- [ ] `python3 scripts/check_i18n.py`（新增中文文案已补 `en.json`）
- [ ] 安装脚本改动时跑过 `sh -n install.sh`
- [ ] 版本改动时跑过 `python3 scripts/check_release_version.py <tag>`

## 检查清单

- [ ] 没有把真实域名、IP、账号、密钥或凭据写进代码、日志与截图
- [ ] 新增输入在边界做了校验，SQL 值全部参数化
- [ ] 500 错误使用通用文案，内部细节只进日志
- [ ] 数据库变更有可回滚迁移，并确认是否应进配置导出/恢复点
- [ ] 新增后台任务监听 `a.ctx.Done()`，没有在请求处理器里跑长任务
- [ ] 涉及 API 或版本时，`docs/项目说明.md` 与 `docs/releases/<tag>.md` 已同步
- [ ] 没有自动替用户修改既有站点或反代配置（只告警）
