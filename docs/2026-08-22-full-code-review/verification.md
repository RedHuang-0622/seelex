# 验证记录

> 对应工作包：[README.md](README.md)。记录本次审阅的构建验证与统计口径，
> 保证结论可复核。

## 1. 构建验证

命令（仓库根目录）：

```text
go build ./...
```

结果：exit 0，无编译错误。GUI 构建标签（`gui,desktop,production`）未在
本次审阅中执行完整构建（需要 Wails 工具链），但 `gui/` 源码与测试已纳入
代码审阅范围。

## 2. 文件规模统计

口径：排除 `dist/`、`.venv/`、`.seelex/` 后，仓库全部 `*.go` 文件。

| 指标 | 数值 |
|---|---|
| Go 文件总数 | 394 |
| 源码文件（非 `_test.go`） | 231 |
| 测试文件（`_test.go`） | 163 |
| 源码总行数（约） | 40,054 |

## 3. 最大源码文件 Top 10（行数）

| 文件 | 行数 | 说明 |
|---|---:|---|
| `sessionstore/sessionstore.go` | 2447 | Router + Repository + 四后端（Q1） |
| `main.go` | 1143 | 组合根 + 产品工具注册（Q2） |
| `seelebridge/task/task.go` | 650 | worktable task 注册表 actor |
| `seelexctx/controller.go` | 578 | 上下文控制器 |
| `seelebridge/tools/router.go` | 573 | scoped 工具族 |
| `application/core/plan_tools.go` | 540 | plan 工具族 + 投影 |
| `seelebridge/runtime.go` | 514 | Runtime 装配 |
| `application/core/work_table.go` | 513 | 工作表格投影 |
| `application/model/state.go` | 507 | 权威 Snapshot DTO |
| `seelebridge/ports.go` | 490 | 端口委托 |

## 4. 覆盖情况参考

仓库 README 记录的可复现覆盖率口径（2026-08-03）：

| 包 | 覆盖率 |
|---|---|
| 全仓 | 62.8% |
| application/core | 75.6% |
| seelebridge | 66.7% |
| tui | 26.1% |
| workspace | 68.4% |

本次审阅未重新执行全量测试（`go test ./...` 需较长时延且非本次请求目标），
以上数字引用自 [README.md](../../README.md) 已记录口径。

## 5. 审阅覆盖面

本次审阅直接阅读的源码/文档（非穷举，但覆盖每个模块的核心实现）：

- 根：`main.go`（全文）、`README.md`、`DESIGN.md`、`MEMORY.md`、
  `go.mod`、`AGENTS.md`。
- 应用层：`application/application.go`、`contract/ports.go`、
  `event/hub.go`、`model/state.go`、`core/service*.go`、`core/chat.go`、
  `core/service_assembler.go`、`core/runtime_projection.go`、
  `core/internal/state/state.go`、`console/backend_console.go`。
- 防腐层：`seelebridge/runtime.go`、`ports.go`、`events.go`、
  `events_unified.go`、`security/*`、`tools/router.go`、`tools/policy.go`、
  `plugin/plugin.go`、`task/task.go`、`scheduler/scheduler.go`、
  `plan/executor.go`、`node/coordinator.go`、`fork/tool.go`、`mcp/mcp.go`。
- 上下文：`seelexctx/controller.go`、`README.md`。
- 存储与扩展：`sessionstore/sessionstore.go`（关键段）、`router_storage.go`、
  `project_record.go`、`README.md`、`plugin/manager.go`、`plugin/apply.go`、
  `plugin/loader.go`、`skill/skill.go`、`workspace/workspace.go`、
  `session/manager.go`、`mcpstack/stack.go`。
- 前端：`tui/tui.go`、`tui/state.go`、`gui/bridge.go`、`gui/shutdown.go`、
  `gui/frontend/dist/protocol.js`、`gui/frontend/dist/app.js`（结构扫描）、
  `docs/gui/architecture.md`。
- 测试：`e2e/scenario/harness.go`、模块级测试文件抽查。
