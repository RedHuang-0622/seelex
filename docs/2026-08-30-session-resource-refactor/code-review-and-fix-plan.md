# 代码现状审查与具体改动方案（Session 资源控制重构）

> 日期：2026-08-30
> 状态：审查结论已实测确认；阶段 0/1/2 已于 2026-08-31 实施并验证
> （实施记录见 [implementation-record.md](./implementation-record.md)，含对抗性
> 审查 7 项问题修复证据与阶段 1/2 收口记录）
> 前置：[plan.md](./plan.md)（资源清单、竞争/污染源）、[design-model.md](./design-model.md)（六元组模型与四条不变量）
> 性质：一次性工作包文档，不冒充长期事实来源；实施完成后由模块 README 承接事实

---

## 1. 结论摘要

当前实现是 **"M1 单槽 + M2 补丁"的混合态**：

- 执行层已会话级：`sessionChat[sid]`（[session_scope.go](../../application/core/session_scope.go)）、
  `sessionStates[sid]`（[task_context/coordinator.go](../../application/core/task_context/coordinator.go)）、
  `engines[sid]` + `For` 变体（[engine_port.go](../../internal/adapters/engine_port.go)）、事件按会话路由、fork 显式项目键。
- 持久化与投影层仍是全局单槽：`Core.Snapshot.Conversation`、`Engine.History()`、
  `Router.projectID`、`Runtime` 任务注册表、`sessionTitle` 单字段。

后果与 [plan.md](./plan.md) 的 P1–P6 / R1–R7 逐条对上：**后台会话收尾会把"键是 A、内容是 B"的记录写进 A 的档案**，
复现测试稳定失败（见第 2 节）。这就是 [design-model.md](./design-model.md) 说的"全局单槽被当成 `M_i/R_i` 用"
违反不变量 Ⅱ/Ⅲ 的直接体现。

修复最短路径：把 `PersistCurrentSession` 的全部读源从全局槽换成会话级 For 变体 +
显式 `(projectID, sessionID)` 键；预期复现测试转绿，P1/P2/P3 主干断裂。

---

## 2. 实测证据

沙箱外执行（沙箱内该测试因 `BindProjectRoot` 符号链接解析被拒无法跑到断言）：

```text
go test . -run TestBackgroundSessionCompletionMustNotPolluteOwner -count=2 -timeout=120s
--- FAIL: TestBackgroundSessionCompletionMustNotPolluteOwner (0.45s)
    repro_session_disappear_test.go:201: A conversation after background completion:
        [system:已恢复会话: sess_1788098048473696800 user:first A assistant:ok user:hello B assistant:ok]
    repro_session_disappear_test.go:203: BUG REPRO: session A lost its own in-flight user
        message "long task A" after background completion; conversation = ...
```

2/2 稳定失败，断言输出与 [plan.md](./plan.md) 第 6 节记录一致。
测试源码：[repro_session_disappear_test.go](../../repro_session_disappear_test.go)。

---

## 3. 现状：全局单槽清单（对照 design-model 六元组）

| # | 域 | 全局槽 | 代码位置 | 对应问题 |
|---|----|--------|----------|----------|
| 1 | M 工作内存 | `Core.Snapshot.Conversation` | [chat.go](../../application/core/chat.go:512) `appendVisibleDelta` 直写全局；[session_history.go](../../application/core/session_history.go:206) resume 置空重建；[archive.go](../../application/core/session_runtime/archive.go:176) 持久化读它 | P1/P3，违反 Ⅱ/Ⅲ |
| 2 | R 引擎历史 | `Engine.History()`（活跃引擎） | [archive.go](../../application/core/session_runtime/archive.go:52) 后台 persist 读活跃引擎；[engine_port.go](../../internal/adapters/engine_port.go:192) 未注册会话回退活跃引擎 | P1/P5 |
| 3 | R task/plan/事件 | 非 For 读口（活跃会话） | [task_context_state.go](../../application/core/task_context/task_context_state.go:629) `Transcript()` / `PendingToolResults()` / `TaskCheckpoints()` / `ToolResultRefs()` 等；[session_runtime/ports.go](../../application/core/session_runtime/ports.go:31) `TaskPersistencePort` 只声明非 For 方法 | P2 |
| 4 | I 身份 | `sessionTitle` 单字段 | [session_runtime/coordinator.go](../../application/core/session_runtime/coordinator.go:43) | P1（标题串写） |
| 5 | B 写作用域 | `Router.projectID` | [sessionstore.go](../../sessionstore/sessionstore.go:239) `SetWorkspace`；[sessionstore.go](../../sessionstore/sessionstore.go:257) `SaveCommit` 用 `Workspace()` 定位键 | R3 / P1 键漂移 |
| 6 | X 收尾 | `ReleaseWorkingHistory()` 无会话参数 | [chat.go](../../application/core/chat.go:218)；[engine_port.go](../../internal/adapters/engine_port.go:613) 清 `port.engine` | P4 |
| 7 | G 全局 Runtime | `TaskSnapshot` / `SwitchSessionTasks` / `SetCurrentTaskBatch` / `projectRoot` | [seelebridge/ports.go](../../seelebridge/ports.go:31) | R6 / P3 |
| 8 | 生命周期 | 无 `hot_attach` / `unload` | `ActivateSession` 恒走 `resumeSession`（冷加载）；`engines` / `sessionStates` 只增不减 | 设计缺口 |

其中第 3 项已存在的 For 变体：`TranscriptFor`（[task_context_state.go](../../application/core/task_context/task_context_state.go:633)）、
`PlanStackFor`（677 行）、`ActivePlanIDFor`（[coordinator.go](../../application/core/task_context/coordinator.go:197)）、
`TaskProjectionLocked(sessionID)`（[task_context_state.go](../../application/core/task_context/task_context_state.go:313)）。
缺失的 For 变体见第 5.2 节。

---

## 4. 主链路：A 后台完成 → 交叉写盘

以 [repro_session_disappear_test.go](../../repro_session_disappear_test.go) 场景为例，每步对应代码证据：

1. A 提交 `long task A` 后运行中：`startChatFor(A)` 把 A 的 user/assistant 消息写入
   **全局 `Snapshot.Conversation`**（[chat.go](../../application/core/chat.go:131) `appendMessageLocked`）。
2. 切到 B：`resumeSession(B)` 将 `Snapshot.Conversation` 置空并按 B 的 record 重建
   （[session_history.go](../../application/core/session_history.go:206)）；A 的在途消息只留在 `engines[A]`。
3. B 完成一轮：B 落盘（正常），随后 `ReleaseWorkingHistory()` 清掉的是**活跃引擎 B** 的工作历史
   （[chat.go](../../application/core/chat.go:218)）。
4. A 后台完成 → `PersistCurrentSession(A)`（[archive.go](../../application/core/session_runtime/archive.go:36)）：
   - ① 读全局 `Snapshot.Conversation` = B 的内容（176 行）；
   - ② 读活跃 `Engine.History()` = B 的引擎（52 行）；
   - ③ 读活跃 task/plan/事件读口 = B 的 transcript/plan/checkpoints（37、165–173 行）；
   - ④ 标题取全局 `sessionTitle` = B 的标题（159 行）；
   - ⑤ 写键 = 当前 `Router.Workspace()` + A（`SaveCommit` 非显式键路径）。
5. 切回 A：record 呈现 "A 旧内容 + B 的 hello B"，`long task A` 永久丢失。

---

## 5. 具体改动方案（契约先行，按阶段）

### 阶段 0：最小修复——后台持久化只读自己的域（预期复现测试转绿）

原则：全部在既有模块内扩展（task_context 补 For 变体、session_runtime 改读源），
不开新包、不新增上帝类型（遵守 MEMORY.md「新功能归属决策」）。

#### 5.1 接口契约：`TaskPersistencePort` 改为全 For 读口

文件：[session_runtime/ports.go](../../application/core/session_runtime/ports.go:31)

现状：接口声明 `Transcript()` / `PendingToolResults()` / `TaskCheckpoints()` /
`ToolResultRefs()` / `ToolResultRefByCallID()` / `ContinuationSummary()` /
`ActivePlanID()` / `PlanStack()` / `SyncActivePlanFrameLocked()` /
`RemoveCommittedToolResultsLocked()` 全部为"活跃会话"语义。

改为：

```text
TaskProjectionLocked(sessionID)              // 已有
TranscriptFor(sessionID)                     // 已有
PlanStackFor(sessionID)                      // 已有
ActivePlanIDFor(sessionID)                   // 已有
PendingToolResultsFor(sessionID)             // 新增
TaskCheckpointsFor(sessionID)                // 新增
ToolResultRefsFor(sessionID)                 // 新增
ToolResultRefByCallIDFor(sessionID, callID)  // 新增
ContinuationSummaryFor(sessionID, requestID) // 新增
SyncActivePlanFrameLockedFor(sessionID, now) // 新增
RemoveCommittedToolResultsForLocked(sessionID, committed) // 新增
```

非 For 方法从接口删除。删除后**编译失败清单即审查清单**——每个调用点都必须显式带会话参数，
从编译期堵死"读活跃槽"路径。删除前先 `rg -n "tasks\.(Transcript|PlanStack|ActivePlanID|...)\(\)"` 全量盘点调用方。

#### 5.2 补齐缺失的 For 变体

文件：[task_context_state.go](../../application/core/task_context/task_context_state.go:629)

新增 7 个方法：把现有非 For 方法体的 `c.activeSessionLocked()` 换成
`c.sessionStateLocked(sessionID)`。注意 `SyncActivePlanFrameLockedFor` 里
`c.Snapshot.Runtime.Plan` 仍来自全局——阶段 1 收口前，该变体先保持"plan 投影来自活跃快照"
并加注释标注遗留风险（P6），本阶段目标是消除 record 级串写，不放大改动面。

#### 5.3 `PersistCurrentSession` 显式键 + 会话读源

文件：[session_runtime/archive.go](../../application/core/session_runtime/archive.go:36)

现状签名：`PersistCurrentSession(sessionID string)`。

改为：

```text
PersistCurrentSession(location Location, sessionID string)
```

内部改动点：

| 行 | 现状 | 改为 |
|----|------|------|
| 37 | `c.tasks.Transcript()` | `c.tasks.TranscriptFor(sessionID)` |
| 38 | `c.tasks.PendingToolResults()` | `c.tasks.PendingToolResultsFor(sessionID)` |
| 43 | `store.LoadSessionRecord(sessionID)`（全局作用域） | `store.LoadSessionRecordWorkspace(location.WorkspaceID, sessionID)` |
| 52 | `store.SaveSessionSnapshot(sessionID, c.Core.Deps.Engine.History(), ...)` | `SaveSessionSnapshotWorkspace(location.WorkspaceID, sessionID, HistoryFor(sessionID), ...)` |
| 56 | `c.tasks.RemoveCommittedToolResultsLocked(...)` | `RemoveCommittedToolResultsForLocked(sessionID, ...)` |
| 68 | `store.SaveSessionRecord(sessionID, record)`（全局作用域） | 新增 `SaveSessionRecordWorkspace`（或在实现上复用 `SaveStateWorkspace` 显式键） |
| 158 | `c.tasks.SyncActivePlanFrameLocked(now)` | `SyncActivePlanFrameLockedFor(sessionID, now)` |
| 165–173 | `ActivePlanID()` / `PlanStack()` / `TaskCheckpoints()` / `ToolResultRefs()` | 对应 For 变体 |
| 176 | `c.Core.Snapshot.Conversation` | 本阶段先传 `conversation []model.Message` 参数（调用方从会话 scope 取）；阶段 1 收口后从 scope 读 |
| 223 | `c.tasks.ToolResultRefs()` | `ToolResultRefsFor(sessionID)` |

引擎历史显式按会话读：优先 `contract.SessionChatEngine.HistoryFor(sessionID)`
（[contract/ports.go](../../application/contract/ports.go:79) 已声明）；
无会话路由能力时返回错误而非回退活跃引擎（与 [service_input.go](../../application/core/service_input.go:64)
的 `engineHistoryFor` 语义对齐，但去掉活跃回退）。

调用方：`runChat` 尾部（[chat.go](../../application/core/chat.go:215)）已有 `sessionID`；
`BeginNewSession` 落盘路径（[session_draft.go](../../application/core/session_draft.go:42)）补 `LocateSession` 取 location。

#### 5.4 会话标题收进 per-session

文件：[session_runtime/coordinator.go](../../application/core/session_runtime/coordinator.go:43)

现状：`sessionTitle model.SessionTitle` 单字段，`SetSessionTitleLocked` 无会话参数。

改为：`sessionTitles map[string]model.SessionTitle`，`SetSessionTitleLocked(sessionID, title)`；
`sessionRecordLocked` 取 `sessionTitles[sessionID]`（缺省回退 `Snapshot.Session.Name`）。
现有调用点（resume/物化/首请求）全部补第一个参数。

#### 5.5 引擎收尾带会话参数

文件：[chat.go](../../application/core/chat.go:218) / [engine_port.go](../../internal/adapters/engine_port.go:613)

现状：`ReleaseWorkingHistory()` 清 `port.engine`（活跃引擎）。

改为：`ReleaseWorkingHistoryFor(sessionID)` 清 `engines[sessionID]`；
`contract` 增加对应接口断言。同时**取消 `engineForSessionLocked` 的活跃回退**
（[engine_port.go](../../internal/adapters/engine_port.go:194)）：未注册会话返回 nil → 调用方显式报错，
杜绝 P5 后台提交打到活跃引擎。

#### 5.6 Runtime 任务快照按会话

文件：[seelebridge/ports.go](../../seelebridge/ports.go:31)

现状：`TaskSnapshot()` 返回全局注册表（`SwitchSessionTasks` 切换时整体替换），
后台 A 落盘 `Tasks` 字段取到的是 B 的注册表。

改为：`TaskSnapshotFor(sessionID)`（按会话分片或随会话切换保存注册表快照）；
`PersistCurrentSession` 用 `TaskSnapshotFor(sessionID)`。

#### 5.7 阶段 0 验证

```text
go test . -run TestBackgroundSessionCompletionMustNotPolluteOwner -count=3
go test ./application/core/... -count=1 -timeout=120s
go test ./sessionstore/... ./internal/adapters/... -count=1
go vet ./...
gofmt -l .
go build ./...
```

收尾条件：复现测试 3/3 PASS；`rg -n "Snapshot\.Conversation|Deps\.Engine\.History\(\)"` 只剩阶段 1 明确标注的遗留点。

### 阶段 1：SessionScope 收口（目标形态）

1. 把 `Core.Snapshot` 中会话相关字段（`Conversation` / `Chat` / `Task` / `Plan` /
   `ReadFiles` / `Interaction` / `HistoryOffset` / `TotalMessages` 等）收进每会话 scope；
   `Snapshot` 只保留视图指针（当前会话 ID）与只读投影，符合 design-model 的 `G := (V, registry, bus, services)`。
2. `resumeSession` 不再"置空重建"，改为"换指针 + 订阅事件流"（`hot_attach` 语义）：
   运行中会话允许只读回看（快照 + 增量事件）或继续拒绝（`ErrChatRunning`）——这是
   [design-model.md](./design-model.md) 第 5 节待定决策点，实施前需用户确认。
3. `session_runtime.Coordinator` 的"活跃默认路由"全面移除：无会话参数的方法删除或强制 `sessionID`。
4. `Router` 显式键硬化：非 `Workspace` 参数路径的写盘方法（`SaveCommit`/`SaveState`/`SaveSessionRecord`）
   逐个改为显式 `projectID` 或标记 deprecated。
5. `Runtime`（projectRoot / task 注册表 / subagent 邮箱 / current batch）随会话 scope 或显式参数化。

### 阶段 2：热加载与卸载

1. 实现 `cold_load` / `hot_attach` / `unload` 生命周期自动机（design-model 第 1.4 节）。
2. `engines[sid]` / `sessionStates[sid]` 增加 evict：`unload(i)` = `persist(i)` 后释放内存，registry 留 COLD 元数据。
3. 每会话队列与后台执行语义维持现状（已符合 X_i 独立），补 `V` 移动不触碰 `X/M/R` 的测试断言。

---

## 6. 测试用例

完整测试用例已独立存放：[test-cases.md](./test-cases.md)。
该文档是测试规格的唯一事实源，包含：场景 A1–A5 全量用例（TC-A1–A5）、
不变量单元断言（TC-INV-01–04）、生命周期用例（TC-LC-01–04，阶段 2）、
竞态用例（TC-R-01–03）、验证命令与通过标准。

本文只保留与第 5 节改动点的映射索引：

| 改动点（第 5 节） | 对应用例 |
|-------------------|----------|
| 5.1 / 5.2 For 变体接口 | TC-A1-02、TC-INV-03 |
| 5.3 显式键 + 会话读源 | TC-A1-01/02、TC-A4-01/02/03、TC-A5-02、TC-INV-01/03 |
| 5.4 标题 per-session | TC-A1-02 |
| 5.5 `ReleaseWorkingHistoryFor` + 取消活跃回退 | TC-A2-01/02/03、TC-R-03 |
| 5.6 `TaskSnapshotFor` | TC-A1-02 |
| 阶段 1 SessionScope / hot_attach | TC-A3-02、TC-LC-01/02 |
| 阶段 2 unload | TC-LC-01–04 |

---

## 7. 风险与回滚

- 阶段 0 每个改动点独立可回滚；先改接口再改实现，编译失败清单兜底遗漏调用点。
- 本方案不触碰 `.seelex` 用户数据、配置与 dist；仅代码与测试改动。
- 遗留风险（本阶段明确标注、不放大）：`SyncActivePlanFrameLockedFor` 的 plan 投影仍读全局
  `Snapshot.Runtime.Plan`（P6）；`Runtime` 全局槽在阶段 1 前仍被后台会话触碰（R6）。
- 待用户确认的决策：热加载是否允许只读快照回看；项目元数据刷新后 `P_i` 是否版本化（design-model 第 5 节）。
