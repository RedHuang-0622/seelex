# GUI Backend

## 模块定位

`gui` 是 Wails/WebView 桌面适配器。它复用 `application.Service`，负责 Go/JavaScript 边界、事件转发、目录选择和关闭协调，不拥有聊天、session、project 或 Plan 业务状态。

## 文件结构

| 文件 | 职责 |
|---|---|
| `bridge.go` | Wails 暴露方法、Application interface 和事件 relay。 |
| `assets.go` | `//go:embed frontend/dist`。 |
| `run_wails.go` / `run_stub.go` | build tags 下的真实 GUI 与不可用 stub。 |
| `dialogs_gui.go` / `dialogs_stub.go` | 平台目录选择适配。 |
| `shutdown.go` | 等待 active/queued chat 完成的 graceful close。 |
| [`frontend/`](frontend/README.md) | 原生 HTML/CSS/ES modules 前端。 |

## Bridge 契约

`Application` 是 GUI 需要的最小端口。`Bridge` 暴露 Snapshot、Submit、BeginNewSession、Cancel、Interaction、Plugin/Account/Effort/Full Access、history pagination、workspace、session storage settings、`UpdateWorkItemStatus`（工作表格 todo 三态）等方法，并把 Application Event 统一转发为 `seelex:event`。`BeginNewSession` 只进入 Application draft，GUI 不通过 `/new` 字符串命令抢先创建 Session。

子代理 `subagent.changed`、`subagent.tool.started`、`subagent.tool.completed` 与其他 Application Event 使用同一 relay，不在 Bridge 内改写 payload。`tool_full_chain_test.go` 从 `Bridge.Submit` 进入，覆盖 ToolHookBridge → Application/EventHub → Bridge emitter → `seelex:event`，并验证 `Bridge.SetFullAccess(true)` 会释放既有审批、投影 Snapshot、relay `runtime.changed`。

工作表格增量 `worktable.changed` 使用同一 relay（payload 只带表格投影，不整份
runtime）；`Bridge.UpdateWorkItemStatus(id, status)` 只做参数透传，业务校验在
application 层（v1 仅支持 `todo:<index>` 的 pending/doing/done）。

task 体系增量 `task.changed`（逐任务状态/打点/retry）同样经 relay；主动
`taskadd` 是模型可调用的 harness 工具（注册表幂等去重），不经 Bridge。

Bridge 方法只做参数转换和调用，不维护镜像业务状态。DSN 等敏感配置必须由 backend redaction 后再返回 renderer。

## 关闭语义

Wails `BeforeClose` 首次触发时调用 `BeginGracefulShutdown`，后台等待 `WaitForIdle`，active session 与已接受队列完成后再允许窗口退出。重复关闭不得启动多个 waiter。

## Build tags

- 普通构建：GUI stub，默认 TUI 不依赖 WebView。
- Wails GUI：`-tags "gui,desktop,production"`，启用 Wails runtime。这里的 `production` 是 Wails 构建约定的一部分，只区分真实桌面实现与 stub，不表示 Seelex 已达到 production-ready；项目成熟度以根 README 的 Developer Alpha 声明为准。
- 前端文件编译时嵌入，修改 `frontend/dist` 后必须重建二进制。

## Review 指南

- Bridge 是否仍为薄适配器，业务分支应进入 Application。
- 事件订阅 goroutine、Wails context 和关闭 channel 是否可退出。
- renderer 可见数据是否已脱敏。
- build-tag 两套实现是否保持相同导出 API。
- 新 Bridge 方法是否有 fakeApplication contract test。

## 测试

```text
go test ./gui -count=1
go build -tags "gui,desktop,production" ./...
node --test gui/frontend/dist/*.test.mjs
```

权威设计文档位于 [`docs/gui`](../docs/gui/README.md)。

## 事件与关闭生命周期

桌面宿主在启动时调用 `Bridge.Start`，在关闭时调用 `Bridge.Stop`；前者先发送 `seelex:ready` 快照，再将 Application Event 原样转发为 `seelex:event`。关闭运行中的会话会先进入 graceful drain，最长等待 5 秒；超时表示工具、审批或 Provider 未收敛，Bridge 会取消当前聊天并继续退出，避免窗口无限处于拒绝关闭状态。
