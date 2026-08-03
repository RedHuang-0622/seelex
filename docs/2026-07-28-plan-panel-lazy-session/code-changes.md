# 代码变更摘要

## 新增/修改/删除文件

| 文件 | 类型 | 说明 | 设计模式 |
|---|---|---|---|
| `application/core/session_draft.go` | 新增 | draft session 进入、幂等处理、首次请求物化和项目 binding。 | 显式状态机、延迟初始化 |
| `application/core/service.go` | 修改 | 串行化 session transition，接入对话物化、项目切换和恢复。 | Mutex 协调、Application Service |
| `application/core/command.go` | 修改 | `/new` 统一进入懒创建 draft，不再立即生成 ID。 | 用例复用 |
| `application/model/state.go` | 修改 | `SessionState` 增加可选 `draft` 字段。 | 版本化 DTO |
| `gui/bridge.go` | 修改 | 增加窄接口 `BeginNewSession`。 | Ports and Adapters |
| `gui/frontend/dist/index.html` | 修改 | Plan 从隐藏 Runtime modal 迁移到右侧常驻栏。 | 单一挂载点 |
| `gui/frontend/dist/app.js` | 修改 | draft 编辑态不进入左侧列表；首问物化后才按名称展示正式会话，同时处理 Plan section 可见性和新 Bridge 调用。 | Snapshot projection、Lazy materialization |
| `gui/frontend/dist/styles.css` | 修改 | 清理不再存在的 draft 列表项样式。 | 组件作用域样式 |
| `seelebridge/scoped_tools.go` | 修改 | Windows scoped shell 使用系统 PowerShell 绝对路径和非交互参数，修复全量测试超时。 | 平台策略 |
| `Makefile`、`scripts/build-gui.ps1` | 修改 | 增加受保护的 clean/build 目标；Dev GUI 强制显式提供 ignored 账号配置并不透明复制，Publish GUI 固定仅含 example 且拒绝本地配置参数。 | Guarded build pipeline、Explicit secret boundary |
| `AGENTS.md`、根 README、`scripts/README.md` | 修改 | 固化 clean → build 顺序和推荐命令。 | 工程契约 |
| `release_test.go` | 修改 | 用契约测试固定 Makefile 清理与构建依赖关系。 | Contract test |
| `application/core/*_test.go`、`gui/bridge_test.go` | 修改 | 覆盖懒创建、幂等、项目继承、恢复退出 draft、Bridge 和 DOM 布局。 | Contract tests |
| 模块 README 与 `front-review.md` | 修改/新增 | 同步会话生命周期、Plan 挂载和审查边界。 | 文档即契约 |

## API 变更

| API | 变更 | 兼容性 |
|---|---|---|
| `Service.BeginNewSession() error` | 新增进入未物化会话草稿的 Application 用例。 | Additive |
| `gui.Application.BeginNewSession() error` | GUI 调用方新增最窄端口。 | GUI adapter 需要实现 |
| `Bridge.BeginNewSession() error` | 暴露给 Wails 前端。 | Additive |
| `SessionState.draft` | 新增 optional JSON 字段。 | 协议 v1 向后兼容 |
| `/new` | 从立即创建改为进入 draft，首次真实对话才物化。 | 行为修复 |

## 设计模式使用

| 模式 | 文件 | 效果 |
|---|---|---|
| Lazy initialization | `session_draft.go`、`app.js` | 用户发送首个请求前不生成 Session ID、存储空记录或左侧列表行。 |
| State machine | `SessionState.Draft` | 明确 draft 与 materialized session 的转换。 |
| Application service | `Service.BeginNewSession` | GUI/TUI 不自行拼接两次 Submit 模拟业务事务。 |
| Keyed identity | `app.js` | Name 仅显示，真实 Session ID 继续用于 resume/delete/binding。 |
| Derived view | `plan-dsl.js` + `app.js` | Plan 继续从权威 JSON 派生，只调整常驻挂载位置。 |

## 接口抽象

| 接口 | 实现方 | 使用方 |
|---|---|---|
| `gui.Application.BeginNewSession` | `application.Service` | `gui.Bridge` |
| `Bridge.BeginNewSession` | `gui.Bridge` | GUI `new-session` 按钮 |

## 循环依赖检查

- [x] Application 未依赖 GUI。
- [x] SessionStore 未感知 draft。
- [x] GUI 只通过 Application-owned DTO/API 消费状态。

## 验证记录

- `go test ./application/core ./gui` 定向契约：通过。
- `go test ./application/... ./gui -count=1 -timeout=120s`：通过。
- `go test ./... -count=1 -timeout=180s`：通过。
- `go test ./application/core -count=10 -timeout=180s`：通过。
- `go test ./seelebridge -run '^TestRuntimeProjectScopedToolsUseBoundProject$'`：通过，Windows shell 超时已修复。
- `go vet ./application/... ./gui`：通过。
- `go build ./...`：通过。
- `go build -tags "gui,desktop,production" ./...`：通过。
- JavaScript `node --check`：通过。
- `node --test gui/frontend/dist/*.test.mjs`：32/32 通过。
- `gofmt -l .`、`git diff --check`、源码安全 grep：通过。
- Makefile 结构契约、有效/恶意版本字符串 guard 语义：通过；本机未安装 GNU Make，实际 target 执行等待 Linux CI。
- GUI 配置打包契约：Dev Make 入口必须提供 `LOCAL_CONFIG`；PowerShell 的 Publish 模式拒绝该参数；GitHub public release 显式选择 Publish。
- Windows 实际打包验证：Publish 目录仅含 `config/accounts.example.yaml`；最终 Dev 目录与 ZIP 同时含 `config/accounts.example.yaml` 和 `config/accounts.yaml`，未读取或打印配置内容。
- Application/Core 覆盖率：70.3%；低于 test-suite 建议阈值，属于现有模块整体覆盖缺口。
- 本地 `CGO_ENABLED=0` 且无 C 编译器，无法运行 `-race`；Linux CI race job负责最终验证。
- 本机未安装 `govulncheck`，安全扫描由 CI job负责。

## Commit 记录

尚未提交。建议拆分为两个提交：

```text
fix(gui): lazy-create sessions and pin plans
fix(seelebridge): stabilize Windows scoped shell
```
