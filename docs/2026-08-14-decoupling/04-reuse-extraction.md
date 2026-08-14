# 可复用提炼清单：内容、放置位置、优先级与风险

日期：2026-08-14
性质：设计建议（未实施）

> 放置原则：**跨域通用件放 `internal/`**（Go internal 限制，防外部误用）；
> 运行态内部模型放 `seelebridge/internal/model`；对外契约单源放
> `application/contract/dto`；域内专用件放各自域包。遵循 AGENTS.md §3 每个
> 新包必须带 README。

## 1. 类型单源 + 显式 mapper（P0，低风险高收益）

**提炼内容**：

- `dto` 成为跨层结构的对外单源（`NodeStageLog/NodeSemanticResult/SubagentTool/
  NodeWorktreeInfo`）；
- `seelebridge/internal/model` 保留运行态权威（内部 actor 使用）；
- 新增 **`seelebridge/internal/mapper`**：集中 `internal/model ↔ dto` 转换
  （`StageToDTO / ToolEventToDTO / SemanticResultToDTO / WorktreeToDTO` 等），
  消除散落内联拷贝（如 `runtime_live.go` 的 `stageLiveEvent/toolLiveEvent`、
  `adapters.go` 的投影适配、`core/subagent_detail.go` 的 `adaptSubagentContext`）。

**放置**：`seelebridge/internal/mapper/`（README 说明"只做无业务转换"）。

**波及**：`dto/subagent_live.go`、`internal/model/node_result.go`、
`session/tool_events.go`、`application/model/state.go`（删重复定义，改 alias 或
引用 dto）、全部转换点。

**风险**：低——纯结构迁移；验收 = §01 第 4 条扫描。

## 2. 通用 actor 助手（P1，行为不变重构）

**提炼内容**：`actor.New[T](handler func(T), opts...) *Actor[T]`：

- 有界命令通道 + 单消费者 goroutine + `Done` 关闭 + `Wait`；
- 同步 reply 封装：`Call(ctx, cmd, timeout) (reply, ok)`（吸收各 actor 里的
  `send`/超时/关闭快返样板）；
- `Close()` 幂等。

**放置**：`seelebridge/internal/actor/`（不依赖任何域包；handler 由调用方闭包）。

**波及**：`session/subagent_sessions.go`、`session/subagent_context.go`、
`task/task.go`、`scheduler/scheduler.go`、`fs`、`mcp`；保留各 actor 的公共方法面
（外部调用方零改动）。

**风险**：中——核心执行路径；靠既有并发测试（`subagent_audit_test.go`、
`fork_concurrency_*`、`merge_back_concurrency_test.go`）护航；逐 actor 迁移，
每个迁移后全绿再下一个。

## 3. 输入队列双轨收敛（P0，中风险）

**提炼内容**：`application/core` 的输入队列收敛为**单一队列 + 单一消费点**：

- 删除 `deferredInputQueue` 与 `drainQueuedInputsAfterLoop` 的中间提升路径；
- 统一走 `OnIterationComplete → return false`（Session-backed）→ `runChat` 结尾
  单点 drain 起下一轮；
- 旧装配路径若保留，抽 `Queue` 接口两个实现，但状态唯一。

**放置**：不动包位置，改 `service_state.go` + `chat.go` 消费点。

**风险**：中——chat.go 是 1368 行核心文件；用现有 `TestInputQueue`、
`command_test.go` 队列用例 + 手工冒烟锁定。

## 4. 测试桩共享（P0，低风险）

**提炼内容**：`internal/testutil`：

- `EmbeddedChatEngine`：全方法 panic 的 `ChatEngine` 底座；
- `EmbeddedRuntimePort`（如需）；`FakeEngine` 可选参数化（history/chunks/错误注入）。

**放置**：`internal/testutil/`（测试专用；README 说明仅供测试）。

**波及**：`gui/tool_full_chain_test.go`、`application/core/service_test.go`、
`e2e/scenario/scripted_engine.go` 改为 embed 底座 + 只覆写所需方法。

**风险**：低；收益 = 接口加方法不再三处同步。

## 5. 插件热更新事务助手（P1，配合自迭代）

**提炼内容**：`plugin/apply.go`：

- `ApplyState(current, next map[string]Plugin) (diff, error)`：新增/删除/修改 diff；
- `Transaction(steps ...Step)`：准备新态 → 失败逆序回滚 → 旧态快照恢复
  （合并现有 `Load`/`Activate` 两套回滚写法）。

**放置**：`plugin/` 域内新文件（不是 internal——root plugin 是产品契约层）。

**波及**：`plugin/manager.go`（`Load/Activate` 复用 + 新增 `Reload/Add/Remove`）、
`main.go` 装配（watcher 可选）。

**风险**：中——激活语义敏感；用 `plugin/manager_test.go` + 热更新专项测试护航。

## 6. telemetry 组合器（P2，可选）

**提炼内容**：`telemetry.Chain(hooks ...Hook) Hook` + 单份透传实现，
替代 `DiagnosticHook/StageHook` 的手写透传样板。

**放置**：`seelebridge/internal/telemetry/chain.go`。

## 7. 事件桥与插件双轨合并（P2，依赖上游/产品决策）

- 事件桥：`seelebridge/events` 归一化适配器（workplan `event.Sink` ↔
  `telemetry.Hook`），依赖 Seele G5（sessionID 关联）；
- 插件双轨：root manager 唯一事实源 + `seelebridge/plugin` 纯执行面，
  属产品决策（是否支持多插件叠加），另立文档讨论。

## 8. 执行顺序建议

1. **P0 批次**（可并行子代理，低耦合）：类型单源 + mapper；测试桩共享；
   输入队列收敛（单独一人，核心路径）。
2. **P1 批次**：通用 actor（逐 actor 迁移）；插件事务助手（配合自迭代）。
3. **P2 批次**：telemetry 组合器；事件桥（等 Seele）；插件双轨（等产品决策）。

每批次验收：`gofmt -l .` 空、`go build ./...`、`go vet ./...`、
`go test ./... -count=1 -timeout=300s`、前端 `node --test gui/frontend/dist/*.test.mjs`、
涉及真实 API 时跑 manual smoke。
