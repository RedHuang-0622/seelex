# PlanAct 上下文控制与完成协议

## 状态

实施中。本文记录本次工作包的目标、边界和验收条件；模块 README 只在实现完成后描述已落地的行为。

## 问题

当前 PlanAct 能强制预规划、加载权威 DAG 并限制 replan，但正常 ReAct 仍主要依赖模型自行决定何时停止。长任务会反复读取、测试或审查：即使已有足够证据，也未形成明确的用户交付终态。工具轮数上限只能阻断循环；它不能保留压缩后的任务状态，也不能保证产生结果。

## 决策

第一阶段不改变 `plan_load` 的外部 DAG 格式。Application 在单个 chat 请求内维护一个私有执行状态：

```text
preflight plan -> authoritative ReAct
  -> tool/node progress -> NodeCheckpoint
  -> ContextController compacts history when needed
  -> deterministic completion check
  -> task_complete | task_failed
  -> user-approved, guarded replan only after failure
```

`task_complete` 与 `task_failed` 是模型可见的内置终态工具；它们不执行外部副作用。终态数据用来形成用户答复、保存会话可恢复的事实摘要，并为已有的 replan 路径提供有界证据。

## 请求作用域

`TaskExecutionState` 归属 Application 的单次 `chatRequest`，不得存入 Runtime 全局状态。它持有：

- 原始 objective、effort、已加载 Plan 的 canonical JSON；
- 当前节点和全部 `NodeCheckpoint`；
- 已完成/失败节点、验证证据、待办和产物；
- 连续无进展次数与请求级预算状态；
- `running`、`completed`、`failed`、`replanning` 终态。

这样 Plan authority、checkpoint、上下文裁剪和 replan evidence 都不会在并行会话之间串台。

## NodeCheckpoint 与上下文控制

每个 `plan_run` 节点结束、Plan 失败、或工具输出达到阈值时，Application 写入结构化 checkpoint。checkpoint 只保存目标、状态、文件/产物、命令 exit code、关键输出摘要、事实、待办和失败证据；不保存模型思维链或原始大日志。

ContextController 的输入是 Engine history 与请求状态。它始终保留系统提示词（由 Engine 管理）、当前原始用户请求、authority plan、当前节点、未完成依赖和必要证据；被 checkpoint 覆盖的旧工具结果、重复读取结果和冗长成功日志会被替换为一个机器标记的摘要消息。触发依据为 token/输出大小/状态变化，绝不使用 wall-clock timeout。

第一版使用现有 `ChatEngine.History` 和 `ReplaceHistory` 完成压缩，避免改变 Seele Engine 协议。`seelexctx.EstimateTokens` 用于稳定估算，不把 provider token 统计当作唯一依据。

## 终态与验收

`task_complete` 需要提交用户摘要、完成节点、产物、验证证据和剩余风险；`task_failed` 需要提交失败类别、失败节点、可复现证据、部分进展和是否建议 replan。

确定性 Judge 是主判定：检查 Plan 必需节点、目标工具/测试结果、变更/产物存在性和终态 payload 的完整性。若模型在自然回复中没有调用终态工具，Application 仍可在 ChatStream 返回后以可验证的状态生成 checkpoint；但对于要求 Plan 的任务，缺少完成或失败终态会作为协议错误显示，不能静默当作成功。

LLM Judge 不在第一阶段引入；它会增加第二个无界 ReAct 风险。后续若需要，只允许单次、无工具、固定小上下文的解释性判定。

## 预算和无进展

节点语义分为 `read_only`、`change`、`verify`、`deliver`。第一阶段由运行期观察推断，第二阶段再把它变成兼容的 DAG 节点字段。

- `change` 需要可观察变更或明确失败；不对只读、验证、交付节点强制写入。
- 连续重复读取/测试且没有新事实、文件变更、节点状态或验证结果时，增加无进展计数并要求模型结束或失败。
- effort 的工具轮数/调用数仍是最后熔断器。熔断后允许一次只读终态交付回合；不允许继续调查或测试。
- 报告导出、写入交付物等必要工具属于 `deliver`，在正常完成协议中始终可用。

## Replan 边界

只有 `task_failed`、确定性验收失败或用户明确变更目标才可进入 replan。`ReplanRequest` 从 checkpoint 提取 objective、旧 Plan、失败和证据，继续使用现有 idempotency key、并发、窗口和 provider 请求预算。模型“还想再检查一次”不是 replan 理由。

## 实现切片

1. 在 `application/core` 增加请求私有 `TaskExecutionState`、checkpoint DTO、终态 payload 校验与内置工具注册。
2. 在 tool hook 和 Plan node callback 中记录进展、归纳工具结果并写 checkpoint。
3. 增加 ContextController：按 token/工具输出阈值将历史替换为 checkpoint context，且不保存内部 authority/终态标记到用户会话。
4. 将 ReAct budget 从直接报错调整为“最终终态决策”回合；未能终态时返回可解释的失败。
5. 将计划和系统提示词更新为“有界验证、终态必选、失败证据”的 Claude 风格规则，并用确定性 harness 回归。
6. 补充 Application 单元测试、PlanAct prompt harness、全仓测试和 Windows Dev GUI 构建。

## 验收

- 长审查任务在证据充分后可收敛为 `task_complete`，不依赖用户打断。
- 连续无进展工具循环会要求完成/失败而非无限继续。
- 大工具输出压缩后仍保留当前目标、authority plan、未完成工作、关键失败及验证结果。
- `task_failed` 能生成有界、可审计的 replan evidence；不自动执行替代 Plan。
- 系统提示词、authority marker 与内部压缩消息均不会暴露至 frontend snapshot 或持久化的普通用户输入。
- 无 wall-clock timeout；高 effort 仍允许长编码任务。

## 后续设计：按任务片段压缩与按需展开

### 设计目标

将 provider history 明确降级为短期执行窗口，而不是会话事实源。一个可执行任务的
闭合片段（Plan 节点完成、失败、阻塞或显式阶段 checkpoint）完成后才进行常规压缩；
运行中的片段保留其最近工具链。若后续需要被压缩片段的细节，模型通过受限的只读工具
按 evidence ID 分页取回，而不是重新扫描项目或恢复整段原始 transcript。

软阈值只在片段闭合时触发；硬阈值仍可在片段中触发，但只能归档超大工具输出并保留
当前目标、authority Plan、活动片段与未完成依赖。阈值采用构造注入的默认策略（而非
环境变量）：以 provider 可用 context 的比例计算；第一版建议软阈值 50%、硬阈值 75%，
并为 system prompt、Plan 与活动片段预留固定预算。

### 设计模式选择

| 模式 | Go 实现 | 应用位置 | 理由 |
|---|---|---|---|
| Strategy | `ContextWindowPolicy` 接口 + 注入实现 | 片段闭合与硬阈值判定 | 按 provider/模型切换预算，不把阈值散落在 hook 中。 |
| Repository | `TaskEvidenceStore` 接口 | 已封存任务片段及其证据 | provider history 被替换后仍可受控读取详情；可先内存实现，再接 sessionstore。 |
| Adapter | `EngineHistoryProjector` | archive → `Engine.ReplaceHistory` | 将执行档案投影为合法、短小的 provider 消息，隔离 Engine 协议。 |

### 方案对比

| 维度 | A：滑动窗口 + 单摘要 | B：任务片段档案 + 按需展开 |
|---|---|---|
| 压缩时机 | 每轮或 token 超阈值 | 节点/阶段闭合；硬阈值才中断活动片段 |
| 保留内容 | 最近 N 条消息与全局摘要 | authority Plan、活动片段、封存片段索引及结构化证据 |
| 详细数据恢复 | 无；通常需重新调用工具 | `task_context_get` 按片段、证据和 cursor 读取 |
| 循环抑制 | 依赖模型理解摘要 | 每个片段有状态、尝试次数和可验证进展 |
| 实现成本 | 低 | 中等 |
| 可回滚性 | 高 | 高：保留现有 History 路径作为 feature flag 回退 |

推荐 B。方案 A 可作为短期止血，但仍会在长任务中丢掉“已经做过什么”的可检索证据，
并使不同输出的重复扫描被误判为新进展。

### 核心数据与接口

`TaskExecutionState` 继续是请求私有 owner；持久化只保存已封存片段的最小可恢复记录。
原始超大结果不进入 Snapshot、普通 conversation 或 prompt；存入 `TaskEvidenceStore` 后仅以
opaque ID 引用。对外的 `TaskState` 只公开片段数量、压缩原因和状态，不公开证据正文。

```go
// application/core：调用方拥有接口。
type ContextWindowPolicy interface {
    ShouldSealAfterBoundary(usage ContextUsage) bool
    RequiresEmergencyArchive(usage ContextUsage) bool
    ProjectionBudget(usage ContextUsage) int
}

type TaskEvidenceStore interface {
    PutSegment(ctx context.Context, segment TaskSegment) (string, error)
    Get(ctx context.Context, requestID, segmentID string) (TaskSegment, error)
    ReadEvidence(ctx context.Context, requestID, evidenceID string, cursor, limit int) (EvidencePage, error)
}

type TaskSegment struct {
    ID, NodeID, Objective, Status string
    PlanDigest                 string
    Facts, RemainingWork       []string
    Evidence                   []EvidenceRef
}

type EvidenceRef struct {
    ID, ToolName, ArgumentsDigest, ResultDigest string
    ExitCode int
}
```

新增只读内置工具 `task_context_get`。它只允许读取当前 request 的已封存片段，必须指定
`segment_id` 或 `evidence_id`，返回受 `limit` 限制的一页内容和下一个 cursor；不得提供
“展开全部历史”参数。`task_checkpoint` 是无副作用的内部工具：模型在完成一个阶段时提交
`node_id/status/facts/evidence_ids/remaining_work`，运行时校验 Plan 节点与证据归属后封存。
没有 Plan 时可使用运行时生成的 `phase-N`。

### Provider history 投影规则

每次压缩后的 history 只包含以下私有投影，且完整替换必须保持合法 assistant/tool 配对：

1. system prompt（Engine 管理）；
2. 原始用户目标；
3. canonical authority Plan 与所有节点状态；
4. 已封存片段的索引：状态、事实、evidence ID、剩余工作；
5. 当前活动片段的有限工具 transcript；
6. 一个明确指令：细节需要时调用 `task_context_get`，禁止为恢复上下文而重复全局扫描。

活动片段完成后，先由运行时把其工具事件归纳为可验证的 evidence metadata，再投影下一
窗口。超大单次工具输出在下一 provider 请求前归档为证据分页，而非删除整个 transcript。
若证据未能写入 archive，保留原 history 并将任务标为可解释的 `task_failed`；不能以丢失
证据的 checkpoint 继续执行。

### 进展与终态

进展不再以“工具结果文本是否不同”定义。只有以下事件推进 epoch：Plan/片段状态转换、
新 evidence ID、已验证测试结论、文件变更、交付产物或用户决策。相同工具与规范化参数的
重复调用即使输出排序或时间戳不同，也累计为该片段的无进展尝试。达到策略上限时只允许
一次无工具终态回合。

`task_complete`、`task_failed` 或 `task_needs_user_decision` 被接受后，运行时切换为
`delivery_only`：拒绝后续调查工具，但允许一轮纯文本交付。这样终态工具本身不会成为新的
ReAct 循环入口。

### 实施步骤

| # | 步骤 | 主要文件 | 说明 |
|---|---|---|---|
| 1 | 定义片段、证据页、策略与 Store 接口 | `application/core/task_execution.go` | 不改变 frontend DTO 的私有证据正文边界。 |
| 2 | 用内存 Store 实现 request 内归档；扩展 `SessionExecutionRecord` 的最小片段索引 | `application/core`, `application/model/state.go` | 先不持久化大正文，验证生命周期与权限。 |
| 3 | 在 tool hook、Plan 状态变更和 `task_checkpoint` 处关闭片段 | `application/core/chat.go` | 只在边界软压缩，紧急归档走单独路径。 |
| 4 | 实现 history projector 与 `task_context_get` | `context_controller.go`, `history_safety.go` | 保留目标/Plan/活动片段，保证 provider history 协议有效。 |
| 5 | 以语义进展替换结果 hash 进展 | `react_budget.go`, `task_execution.go` | 对重复只读调用施加片段级上限。 |
| 6 | 接入 sessionstore 持久化与恢复 | `session_archive.go`, `sessionstore/` | 仅在内存版稳定后实施；按 workspace + session + request scope 隔离。 |

### 测试策略

- Plan 节点 `inspect` 完成后压缩：下一轮仍能看到原始目标、完整 Plan、节点状态和证据 ID。
- 144KB `glob` 输出：结果可分页展开，活动节点不重置，且 provider history 在预算内。
- 同一 `glob`/`plan_status` 参数多次调用、仅输出顺序变化：触发无进展终态，不能刷新进展。
- `task_context_get`：拒绝跨 request、跨 workspace、无 ID、超大 limit 和“全部展开”。
- session 恢复：只恢复索引与最小 continuation；前端 Snapshot/普通对话均不泄露私有 evidence 正文。
- terminal accepted 后：允许文本交付，拒绝新的读写工具。

### 循环依赖检查与回滚

接口留在 `application/core`（调用方），store 实现在 `sessionstore` 或 core 内部适配器；
`application/model` 只放 DTO，不能依赖 core，因此不存在 `core → sessionstore → core` 循环。
以 `ContextWindowPolicy` 的 feature flag 选择旧控制器或新 projector；新 Store 写入失败时保留
未压缩 history 并显式失败，不删除证据。回滚只切回旧策略，不迁移或删除已保存的 session 记录。
