# 重复模式盘点（actor / 注入门面 / hook 链 / 回滚链 / 测试桩）

日期：2026-08-14
性质：只读调研

## 1. 手写 actor 模式（约 7 处复制）

同一"channel 命令 + 单消费者 goroutine + done + WaitGroup + 同步 reply"模式反复出现：

- `seelebridge/session/subagent_sessions.go`（`cmd chan subagentSessionCmd` + `done` + `wg`）
- `seelebridge/session/subagent_context.go`（`SubagentContextActor`）
- `seelebridge/session/subagent_tree.go`（`SubagentTree`，events 通道）
- `seelebridge/task/task.go`（`TaskRegistry`）
- `seelebridge/scheduler/scheduler.go`（`State`，ticker 驱动）
- `seelebridge/fs`（`filesystem_actor`，写路径分片串行）
- `seelebridge/mcp/mcp.go`（管理器）

每份都有：命令类型 + `send`（带超时）+ `reply` 通道 + `Close` 幂等。模式统一，
但代码复制；新 actor 要再抄一遍，且容易在超时/关闭细节上出偏差
（历史上有 merge-back 死锁教训，属于该模式易错点）。

**收敛方向**：一个泛型 `actor` 助手（见 04 §2），保留"单消费者串行"不变量。

## 2. provider 注入门面（约 30 个 Setter）

- `EnginePort`：`SetNodeConversationsProvider / SetNodeContextProvider /
  SetNodeWorktreeProvider / SetNodeToolResultProvider / SetSubAgentTreeProvider /
  SetSubagentLiveProvider / SetHistoryPreparer` 等 7+ 个单字段 setter；
- `Runtime`：`SetTurnArchiver / SetBashDiagnosticObserver / SetSubagentToolCallback /
  SetSkillRegistry / SetScheduledPromptExecutor / SetSchedulerObserver /
  SetRuntimeVisibilityProjection / SetParentEvidenceProjection /
  SetProjectKnowledgeProvider / SetEventErrorHandler / SetEventPersister /
  SetPlanApprovalGate / SetPlanBranchBinding / SetPlanNodeCallback / SetPlanPolicy /
  SetProvider / SetPermissionConfig / SetFullAccess` 等 18+ 个。

每个 setter = 字段 + nil 防护 + 锁（部分）。接口化本身是解耦手段，但**散装 setter
面**让装配点（main.go）与测试要逐个调用，且容易漏配（运行时 nil 降级掩盖错误）。

**收敛方向**：
- 按域合并为**装配结构体**（如 `EnginePortDeps{...}` / `RuntimePlanDeps{...}`），
  构造时一次注入；保留个别运行期可变 setter（如权限/投影）；
- 或收敛为 `Deps` 结构 + `Apply(deps)`，禁止再新增单字段 setter。

## 3. hook 装饰链（telemetry）

`LifecycleHook → DiagnosticHook → StageHook`（`internal/telemetry`）每加一个观察面
就手写一个 Before/After/OnError 透传包装器（`diagnostic.go`、`stage_hook.go`）。

**收敛方向**：提供 `telemetry.Chain(hooks ...Hook) Hook` 组合器 + 一次透传实现，
新观察面只写 Before 逻辑，不写透传样板。

## 4. 事务回滚链（plugin 域重复两次）

- `plugin/manager.go` `Load` 的注册回滚（失败按逆序 ClearPluginSkills + UndefinePlugin）；
- `plugin/manager.go` `Activate` 的 prepare/restore 回滚（MCP attach 失败 → detach，
  工具/skills 失败 → 恢复旧插件）。

模式相同（准备新态 → 失败逆序回滚 → 恢复旧态），写法不同。

**收敛方向**：抽出 `plugin/apply.go` 的 `ApplyState` 事务助手（步骤列表 + 逆序
回滚 + 旧态快照），热更新（Add/Update/Remove）直接复用。

## 5. 测试桩重复（同一引擎 3 份全量实现）

`ChatEngine` 的全量假实现：

- `gui/tool_full_chain_test.go` `guiChainEngine`
- `application/core/service_test.go` `fakeEngine`
- `e2e/scenario/scripted_engine.go` `ScriptedEngine`

每加一个接口方法，三处同步补（本次 `SubscribeSubagentLive` 就补了三处）。

**收敛方向**：`internal/testutil` 提供"可嵌底座 + 未实现方法 panic"的共享桩
（Go 惯用：embed 一个全 panic 的 struct，测试只覆盖要用的方法）。

## 6. 验收标准

- 新增一个 actor 的样板行数 ≈ 0（用助手）；
- 新增一个 `ChatEngine` 方法，测试文件改动 = 1（共享桩）或 0；
- 新增 telemetry 观察面不再出现整段透传样板；
- 装配点不再新增单字段 setter（走 Deps 结构）。
