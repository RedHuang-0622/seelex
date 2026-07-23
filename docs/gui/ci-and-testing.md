# GUI CI 与测试设计

## 1. 门禁目标

GUI 变更必须同时证明：

1. Application/Bridge 契约没有破坏；
2. 前端 ES Modules 语法有效；
3. 协议 reducer、客户端竞争、Markdown、组件和 Effort Controller 测试通过；
4. Wails production tags 能在 Windows 编译；
5. 全仓 test/vet/race/release-safety 仍由主 CI 覆盖。

## 2. 触发策略

实现位置：`.github/workflows/ci.yml:5-10`。

| 事件 | 分支 | 原因 |
|------|------|------|
| push | `main`、`gui` | 两条长期分支都必须直接获得反馈 |
| pull_request | base 为 `main`、`gui` | feature→gui 与 gui→main 都受保护 |
| workflow_dispatch | 任意选择 ref | 允许维护者手工重跑 |

此前 workflow 只监听 `main`，因此 direct push `gui` 不触发；PR 到 main 才会触发。当前配置已修正。

首次远端验证：提交 `df0434b` 的 push run [30004410641](https://github.com/RedHuang-0622/seelex/actions/runs/30004410641) 已证明 `gui` trigger、独立 `GUI tests`、三平台 build、race/coverage 和 release-safety 全部生效。

## 3. Job 划分

```text
build (ubuntu/windows/macos)
  ├─ gofmt / go build / go vet / go test
  └─ Windows: GUI production tags build

GUI tests (ubuntu)
  ├─ JS module syntax
  ├─ Node frontend tests（含 Effort controller）
  └─ application + gui contract tests

race-and-coverage (ubuntu)
  └─ full repository -race + coverage artifact

release-safety (ubuntu)
  ├─ candidate packages
  ├─ account config allowlist
  └─ govulncheck
```

Jobs 相互独立，任一失败都会阻止受保护分支合并。

## 4. GUI tests job

实现位置：`.github/workflows/ci.yml:83-110`。

环境：Ubuntu latest、Go 1.25、Node 22、10 分钟 timeout。

| 步骤 | 命令 | 失败含义 |
|------|------|---------|
| JS syntax | `find ... '*.js' ... node --check` | ES module 语法无效 |
| frontend tests | `node --test gui/frontend/dist/*.test.mjs` | reducer/Markdown/components/Effort 契约回归 |
| Core/Bridge tests | `go test ./application ./gui -v -count=1` | DTO、会话或 Wails adapter 契约回归 |

不使用 npm install，因为当前前端没有第三方 runtime/test dependency；Node 只作为 test runner。

## 5. Production build

实现位置：`.github/workflows/ci.yml:50-52`。

Windows runner 执行：

```text
go build -tags "gui,desktop,production" ./...
```

`gui` 选择 Wails 实现；`desktop,production` 满足 Wails 手工构建约束，避免“will not build without the correct build tags”。Ubuntu/macOS 不重复执行桌面链接，避免缺少系统 WebView development packages 导致与逻辑无关的失败。

## 6. 本地等价验证

PowerShell：

```powershell
Get-ChildItem gui/frontend/dist -Filter *.js |
  ForEach-Object { node --check $_.FullName }
node --test gui/frontend/dist/*.test.mjs
go test ./application ./gui -v -count=1
go build -tags "gui,desktop,production" ./...
go test ./... -count=1 -timeout=180s
go vet ./...
```

Windows 本机若 `CGO_ENABLED=0`，`go test -race` 会报告需要 CGO。不能把这视为 race 通过；全仓 race 结果以 Ubuntu `race-and-coverage` job 为准。

## 7. 测试分层

| 层 | 覆盖 | 当前证据 |
|----|------|---------|
| Pure unit | reducer、Markdown、presentation model、Effort controller | 26 个 Node tests |
| Contract | Application DTO/Event、Bridge bindings | Go application/gui tests |
| Integration | 全仓依赖与 session/runtime fakes | `go test ./...` |
| Concurrency | EventHub、Chat、tool、Snapshot | Ubuntu `-race` + `application/race_test.go` |
| Build | Wails build tags、embedded assets | Windows build + Bridge embedded asset test |
| WebView E2E | 键盘、滚动、modal、系统 WebView | 当前手工，待自动化 |

## 8. 失败定位

| 检查失败 | 优先检查 |
|----------|----------|
| GUI JS syntax | 最近编辑的 ES module、import/export |
| Node protocol test | protocol version、seq/revision、payload schema |
| Markdown test | escape/token/URL allowlist、think block |
| components test | stable key、tool pairing、queue model |
| Effort test | 四档顺序、预览/提交边界、失败回滚、Max selector |
| Bridge test | `gui.Application`、bound method、embedded file |
| Windows GUI build | tags、Wails version、embedded resource、平台 toolchain |
| race | Event payload 深拷贝、Service/EventHub/Bridge lock 顺序 |

## 9. 后续增强

- 引入 workflow/action 静态 linter（例如 actionlint）作为 CI 自检。
- 用 Playwright 加载静态 shell + fake Bridge，覆盖键盘、modal 和 DOM reconciliation。
- 在 Windows runner 增加启动 smoke test；真实系统 WebView E2E 需要可交互 runner。
- 给 GUI job 增加测试结果 artifact/JUnit 输出，便于趋势统计。
