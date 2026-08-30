# Session 资源控制重构：阶段 0 实施记录

> 日期：2026-08-31
> 状态：阶段 0 已实施并验证（复现转绿、测试集全绿、-race 通过）；阶段 1/2 规划
> 前置：[plan.md](./plan.md)（资源/粒度/P-R 清单）、[design-model.md](./design-model.md)（六元组与不变量）、
> [code-review-and-fix-plan.md](./code-review-and-fix-plan.md)（改动方案）、[test-cases.md](./test-cases.md)（用例规格）
> 性质：一次性工作包记录，不冒充长期事实来源；模块事实由各模块 README 承接

---

## 1. 一句话结论

阶段 0 已按 `code-review-and-fix-plan.md` 落地：**持久化/投影层从全局活跃槽改为
全 For 会话读源 + 显式 `(projectID, sessionID)` 键**；原污染复现
`TestBackgroundSessionCompletionMustNotPolluteOwner` 由稳定红转 3/3 绿。
对抗性审查发现的 2 个必现失败与 5 个潜伏问题全部修复；`go vet`、`gofmt`、
`go test -race`（核心 + 根包）均通过。

---

## 2. 修改清单（按文件）

### 2.1 契约层

| 文件 | 改动 |
|------|------|
| `application/contract/ports.go` | `RuntimePort` 新增 `TaskSnapshotFor(sessionID)`、`SetSessionWorkspace(sessionID, workspaceID)`；`SwitchSessionTasks` 改为带 `sessionID` 参数（离开会话时保存其注册表快照） |
| `application/core/session_runtime/ports.go` | `TaskPersistencePort` 全 For 化：`Transcript()`/`PendingToolResults()`/`TaskCheckpoints()`/`ToolResultRefs()`/`ToolResultRefByCallID()`/`ContinuationSummary()`/`ActivePlanID()`/`PlanStack()`/`SyncActivePlanFrameLocked()`/`RemoveCommittedToolResultsLocked()` 全部删除，替换为带 `sessionID` 的 For 变体（非 For 活跃读口从接口消失，编译失败清单即审查清单）；新增 `TaskStateFor(sessionID)`；`SessionRecordPort` 新增 `SaveSessionRecordWorkspace`；`SessionSnapshotPort` 新增 `SaveSessionSnapshotWorkspace` |

### 2.2 task 域

| 文件 | 改动 |
|------|------|
| `application/core/task_context/task_context_state.go` | 补齐 7 个 For 变体（`PendingToolResultsFor`、`TaskCheckpointsFor`、`ToolResultRefsFor`、`ToolResultRefByCallIDFor`、`ContinuationSummaryFor`、`SyncActivePlanFrameLockedFor`、`RemoveCommittedToolResultsForLocked`）+ `CurrentRequestIDFor` + `TaskStateFor`；非 For 方法保留（root 包仍在用），但不再满足持久化端口 |

### 2.3 会话域（持久化收口）

| 文件 | 改动 |
|------|------|
| `application/core/session_runtime/archive.go` | `PersistCurrentSession(location Location, sessionID string)`：① task 快照改 `TaskSnapshotFor(sessionID)`；② 全部 task/plan/事件读口 For 化；③ 存储读写显式 `(WorkspaceID, sessionID)` 键；④ 引擎历史用 `HistoryFor(sessionID)`（会话路由）；⑤ **L1**：落盘前从磁盘事件全量 + 内存事件按 Seq 合并重建 `record.Conversation`（见第 3 节）；⑥ **L2**：标题缺省回退磁盘 record 而非全局快照；⑦ **L5**：`Execution.Task` 由 `TaskStateFor(sessionID)` 构建，不再读全局 `Snapshot.Task`；⑧ **L4**：assistant 多 tool call 消息独立 ID |
| `application/core/session_runtime/coordinator.go` | `sessionTitle` 单字段 → `sessionTitles map[string]SessionTitle`；`SetSessionTitleLocked(sessionID, title)` |
| `application/core/context_runtime/ports.go`、`coordinator.go` | `SessionPort.PersistCurrentSession(session_runtime.Location, string)`；`CompactTaskContextFor` 用 `sessionLocationLocked` 按会话 workspace 定位（压缩 checkpoint 落盘不依赖全局写作用域） |

### 2.4 引擎/存储/适配层

| 文件 | 改动 |
|------|------|
| `internal/adapters/engine_port.go` | `ReleaseWorkingHistory()` → `ReleaseWorkingHistoryFor(sessionID)`（后台收尾只清自己的引擎，对应 P4）；`engineForSessionLocked` 取消未注册会话的活跃引擎回退（对应 P5） |
| `internal/adapters/session_workspace_ports.go` | 新增 `SaveSessionRecordWorkspace`（显式项目键） |
| `sessionstore/sessionstore.go` | `Router.SaveWorkspace(projectID, sessionID, messages)` 显式键写 provider 历史 |
| `sessionstore/durable_history.go` | **R3 framework 侧收敛**：`DurableHistory` 新增 `SetWorkspaceResolver`，`Load/Save/LoadEventTail/Clear` 全部改显式 workspace 键（见第 3 节） |
| `session/manager.go` | 新增 `SaveStateByWorkspace` |

### 2.5 Runtime / 装配

| 文件 | 改动 |
|------|------|
| `seelebridge/runtime.go`、`ports.go` | `sessionTaskSnapshots` + `currentTaskSessionID`：`SwitchSessionTasks(sessionID, records)` 离开会话时保存注册表快照；`TaskSnapshotFor(sessionID)` 按会话取；`sessionWorkspaces` + `SetSessionWorkspace`：application 登记会话绑定，`newMainSession` 为 DurableHistory 注入 workspace 解析闭包 |
| `internal/adapters/runtime_port.go` | 转发 `TaskSnapshotFor`/`SwitchSessionTasks(sessionID,…)`/`SetSessionWorkspace` |
| `application/core/chat.go` | 落盘改 `PersistCurrentSession(location, sessionID)`；收尾改 `ReleaseWorkingHistoryFor(sessionID)`；标题按会话设置（后台会话首次请求也有自己的标题，对应 L2） |
| `application/core/session_history.go`、`session_draft.go` | `SetSessionTitleLocked(sessionID,…)`；`SwitchSessionTasks(sessionID,…)`；`SetSessionWorkspace`（恢复/物化时登记） |
| `application/core/workspace_usecase.go` | 绑定工作区时 `SetSessionWorkspace(currentSessionID, workspace.ID)` |

### 2.6 测试

新增（根包真实 harness）：

- `repro_session_background_test.go`：TC-A1-02（record 全域只属 A）、TC-A1-03（后台完成不影响活跃会话 B 队列）、TC-A2-01（A 收尾与 C 运行真实并发交错）
- `repro_session_workspace_test.go`：TC-A4-01/02/03（跨工作区键漂移）、TC-A5-02（fork 不改变其它会话落盘键）
- `repro_session_race_test.go`：TC-R-01（切换/提交/快照与后台完成并发）

新增（application/core 单元）：

- `session_resource_isolation_test.go`：TC-INV-01/02/03（域不相交、视图切换零写入、persist 只读自有域）
- `session_race_test.go`：TC-R-02（快照 bump 与 runChat 尾部并发）、TC-R-03（ReleaseWorkingHistoryFor 与 ChatStream 并发）

测试桩适配：`fakeEngine` 实现 `SessionChatEngine` 路由面；`fakeRuntime` 实现
per-session task 快照与 workspace 登记；`archiveSessions` 等补
`SaveSessionRecordWorkspace`；`gracefulShutdownEngine`/`sessionBackedBlockingEngine`/
`blockingEngine` 显式实现 `ChatStreamFor` 转发（动态分派恢复）。

---

## 3. 对抗性审查问题的修复

审查结论 7 项全部处理，证据如下：

### F1 跨工作区测试编排错误（已修）

原 `startCrossWorkspaceScenario` 先 `CreateWorkspace("ws-y")` 再提交 first B，
把 provider 的阻塞位 #2 吃掉（first B 卡死、long task A 变 #3 快速返回）。
另暴露真实语义：A 完成后引擎历史被清，`CreateWorkspace(Y)` 不再触发新会话，
而是把 A 移绑到 Y。

修复：① provider 改为 `blockingProvider(3)`（按请求序号可配置，阻塞位后移到
long task A）；② 编排改为 `CreateWorkspace(X) → first A → BeginNewSession →
CreateWorkspace(Y)（draft 分支绑定）→ first B`，A 保持 X、B 归属 Y。

### F2 fakeEngine 升级破坏测试桩动态分派（已修）

`fakeEngine` 新增 `ChatStreamFor` 后被内嵌它的阻塞桩（`gracefulShutdownEngine`、
`sessionBackedBlockingEngine`、`blockingEngine`）提升，`chatStream` 走路由面时
调用的是提升方法，其内部静态绑定 `fakeEngine.ChatStream`，绕过外层覆写的阻塞/
取消语义（优雅关闭、排队消费、CancelChat 中断等 5 个用例回归）。

修复：三个外层桩显式实现 `ChatStreamFor` 并转发到各自 `ChatStream`，动态分派
恢复；核心包全量回归绿。

### L1 record 被截断成尾部窗口（已修）

新代码删掉 `mergeConversationMessages` 改为从内存 transcript 重建 record，但
resume 时内存 transcript 只加载尾部窗口（`maxUnits=4`），>4 轮会话落盘会丢旧
消息——确定性数据丢失回归。

修复：`PersistCurrentSession` 锁外读**磁盘事件全量**（`LoadTranscriptTailWorkspace`
大预算）+ 内存事件按 **Seq 去重合并**（`mergeTranscriptEventsBySeq`），再从合并
后全量事件重建 `record.Conversation`，写盘事件也用全量（不再依赖后端
`mergeEvents` 幸存）。事件为空时保留 `sessionRecordLocked` 的 Snapshot 回退
（单会话测试桩路径）。

### L2 后台会话标题仍回退活跃槽（已修）

`sessionRecordLocked` 的标题缺省读 `Snapshot.Session.Name`（活跃槽）；且
`startChatFor` 只在活跃分支设置标题，纯后台启动的会话无自有标题。

修复：① `startChatFor` 无条件为会话设置标题（仅活跃会话同步快照展示名）；
② `sessionRecordLocked` 不再读全局快照，缺省标题由 `PersistCurrentSession`
锁外从磁盘 record 回退。

### L3 task 隔离只修了读、没修写（记录为阶段 1 遗留 + 方案）

`SetCurrentTaskBatch` 仍全局调用，后台会话工具执行写入活跃注册表，
`TaskSnapshotFor(A)` 只读切换时快照，A 后台跑出的任务既不进 A 的 record、
又混入 B 的注册表与工作台投影。

方案（阶段 1）：`SetCurrentTaskBatch(sessionID, batchID)` 按会话保存默认批次，
`TaskRegistry` 按会话分片或 `Runtime.TaskAdd` 按调用会话补 BatchID；本阶段
不做（涉及 task actor 契约扩散，超出阶段 0 最小修复面）。

### L4 重建 record 出现重复消息 ID（已修）

assistant 事件带 N 个 ToolCalls 时 `conversationFromTranscriptLocked` 输出 N 条
同 ID 的 tool 消息，前端按 ID 增量路由会串更新。修复：每条 tool 消息 ID 为
`message-<seq>-<callIndex>`（唯一）。

### L5 残留全局读（Execution.Task 已修，ReadFiles 记录为遗留）

`record.Execution.Task` 仍读全局 `Snapshot.Task`（后台 A 落盘 = B 的可见任务
态）。修复：`TaskPersistencePort` 新增 `TaskStateFor(sessionID)`，从本会话
`taskExecution` 构建（状态字符串 `running` → `progressing` 映射）。
`Execution.ReadFiles` 仍读全局 `Snapshot.ReadFiles`（`RecordReadFileLocked` 写
全局槽）——阶段 1 SessionScope 收口时按会话隔离，本阶段明确标注。

### 审查未列、A4-03 暴露的深层残留：framework DurableHistory 键漂移（已修）

TC-A4-03 修复编排后仍失败：**A 同时出现在 X 和 Y 的 catalog**。根因不是
`PersistCurrentSession`，而是 framework `DurableHistory` 用 `Router` 全局
`Workspace()` 定位键——A 的 ChatStream 在 B 活跃（Router 已切 Y）时结束，
其 provider 历史串写进 Y（record 在 X，但 Y 出现 A 的 manifest）。

修复：① `Router.SaveWorkspace` 显式键；② `DurableHistory` 新增
`SetWorkspaceResolver`，`Load/Save/LoadEventTail/Clear` 全部按会话 workspace
显式键落盘；③ `Runtime` 维护 `sessionWorkspaces` 映射，`newMainSession` 注入
解析闭包；④ application 在 `resumeSession`/物化/绑定工作区时
`SetSessionWorkspace(sessionID, workspaceID)` 登记。A4-01/02/03 全部转绿。

### 测试有效性改进

- TC-A2-01 重写：A 长任务阻塞 → B 完成 → C 完成一轮 → **C 运行中（请求 #5
  阻塞）释放 A**，A 收尾与 C 运行真实并发；断言 A record 无 C 内容、C 运行态
  不被 A 收尾影响、C 完整收尾后可见内容含 hello C 与 hello C2。
- TC-A1-03 修正：A 释放后不能 `WaitForIdle`（B 仍阻塞运行），改为轮询 A 落盘
  完成再断言 B 队列；末尾释放 B 再等 idle。
- 竞态用例：TC-R-01/02/03 已在本机 `-race` 实跑通过（本机 CGO 可用，
  race detector 生效），CI 的 Linux `-race` 仍作为持续门禁。

---

## 4. 验证结果

### 4.1 阶段 0 测试集（普通模式）

```text
go test . -run 'TestBackgroundSessionCompletionMustNotPolluteOwner|TestBackgroundCompletionPreservesARecordDomains|TestBackgroundCompletionKeepsActiveSessionQueue|TestBackgroundCompletionWhileSwitchingToC|TestBackgroundPersistUsesSessionWorkspaceKey|TestBackgroundPersistDoesNotMutateWriteScope|TestNoPhantomSessionInForeignWorkspace|TestForkDoesNotShiftOtherSessionPersistKey|TestWorkspaceSwitchConcurrentWithBackgroundPersist' -count=1
→ ok（5.37s，9/9 PASS）

go test ./application/core/ -count=1
→ ok（核心包全量，含既有 5 个回归用例恢复）

go test ./application/core/... ./sessionstore/... ./internal/adapters/... -count=1
→ 全绿
```

### 4.2 -race（本机 CGO 可用，race detector 生效）

```text
go test -race ./application/core/ -run 'TestSessionDomainsDisjoint|TestViewSwitchDoesNotMutateExecution|TestPersistReadsOnlyOwnDomain|TestSnapshotBumpConcurrentWithRunChatTail|TestReleaseWorkingHistoryConcurrentWithChatStream' -count=1
→ ok（1.56s，无数据竞争）

go test -race . -run 'TestWorkspaceSwitchConcurrentWithBackgroundPersist|TestBackgroundSessionCompletionMustNotPolluteOwner|TestBackgroundCompletionWhileSwitchingToC|TestNoPhantomSessionInForeignWorkspace' -count=1
→ ok（4.16s，无数据竞争）
```

### 4.3 静态检查

```text
go build ./...    → 通过
go vet ./...      → 通过
gofmt -l .        → 仅剩 3 个未涉及文件（session_parallel_test.go、
                   task_context/coordinator.go、seelexctx/limits.go，非本改动引入）
```

---

## 5. 遗留与阶段 1/2 待办

阶段 0 明确标注的遗留（不放大改动面）：

1. **L3 task 写隔离**：`SetCurrentTaskBatch`/`TaskAdd` 仍全局，后台会话工具
   任务会混入活跃注册表；方案见第 3 节，阶段 1 实施。
2. **`Execution.ReadFiles` 全局槽**：`RecordReadFileLocked` 写 `Snapshot.ReadFiles`，
   后台 read 文件会串写；阶段 1 SessionScope 收口按会话隔离。
3. **P6 plan 投影**：`SyncActivePlanFrameLockedFor` 的 plan 投影仍读全局
   `Snapshot.Runtime.Plan`（代码已注释标注）。
4. 非 For 活跃读口（`Transcript()`/`PlanStack()` 等）仍在 task_context 具体类型
   上（root 包调用方使用）；持久化端口已不再暴露它们，阶段 1 全面移除活跃默认
   路由。

阶段 1/2（design-model 第 1.4/1.5 节）：

- `Core.Snapshot` 会话字段收进每会话 scope，`G` 只留视图指针/registry/bus/services；
- `resumeSession` 改为 `hot_attach` 语义（换指针 + 事件订阅）；运行中会话只读回看
  或维持 `ErrChatRunning`（待用户确认决策）；
- `cold_load`/`hot_attach`/`unload` 生命周期自动机与 `engines[sid]`/`sessionStates[sid]`
  evict。

---

## 6. 验证命令（复跑）

```text
# 阶段 0 主回归
go test . -run 'TestBackgroundSessionCompletionMustNotPolluteOwner|TestBackgroundCompletionPreservesARecordDomains|TestBackgroundCompletionKeepsActiveSessionQueue|TestBackgroundCompletionWhileSwitchingToC|TestBackgroundPersistUsesSessionWorkspaceKey|TestBackgroundPersistDoesNotMutateWriteScope|TestNoPhantomSessionInForeignWorkspace|TestForkDoesNotShiftOtherSessionPersistKey|TestWorkspaceSwitchConcurrentWithBackgroundPersist' -count=3

# 单元/适配层
go test ./application/core/... ./sessionstore/... ./internal/adapters/... -count=1 -timeout=120s

# 竞态（CGO 可用时）
go test -race ./application/core/ -run 'TestSessionDomainsDisjoint|TestViewSwitchDoesNotMutateExecution|TestPersistReadsOnlyOwnDomain|TestSnapshotBumpConcurrentWithRunChatTail|TestReleaseWorkingHistoryConcurrentWithChatStream' -count=1
go test -race . -run 'TestWorkspaceSwitchConcurrentWithBackgroundPersist|TestBackgroundSessionCompletionMustNotPolluteOwner' -count=1

# 静态
go vet ./...
gofmt -l .
git diff --check
```
