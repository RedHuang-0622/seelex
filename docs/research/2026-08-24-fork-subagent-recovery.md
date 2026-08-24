# fork 子代理异常中断状态恢复可行性调研

日期：2026-08-24
状态：调研结论（已实现，2026-08-24，部分）。落地范围见文末「实现记录」；未落地项保持"规划"标识，需按本仓库架构文档、模块 README 与测试流程执行。
范围：fork_subagents / plan_run 的 kind:agent 节点在进程中断、超时、provider 失败后的状态恢复；子代理会话、上下文栈、事件轨、worktree、worktable 的持久化与重建边界。

## TL;DR

1. **“子代理串行、执行者是 main”的观感不是 workplan 调度问题**：事件数据（`session-e447d942e76010e0/framework-events.json`）显示三个 fork 节点几乎同时 queued/running，heartbeat 的 elapsed 同步增长，DAG 是真并行。串行观感来自两点：单个节点会话内连续几十轮 LLM（如 `turn #46`）的长串行；以及节点事件、阶段事件、task Assignee 全部以主会话 `sess_<nano>` 归因/兜底。
2. **未发现“降级到 mainagent”的策略**：fork 建 task 时 Assignee 被动兜底为 `main:<mainSessionID>`，真正认领为 `subagent:<节点会话ID>` 依赖内存态 SubAgentTree + application worktable 投影；节点会话未注册、未刷新或重启后树清空时，Assignee 停留在 main。
3. **中断恢复可行，但工作量主要在“子代理侧持久化”，不是 stack 本身**：上游 Seele workplan 已提供 checkpoint/resume 原语（`WithCheckpoint`/`Resume`），但 seelex 未接线；子代理会话历史、fork 树、阶段日志、上下文快照全部是内存态，重启即空。
4. **“有 stack 存储记录”目前只对主会话成立**：`SessionContextStore` 持久化 CompactStack 四栈，但子代理会话没有独立历史/栈；且子代理会话的压缩器现在指向主会话的栈存储，恢复时需先做节点级隔离。

## 背景与现状

### 用户观察到的现象

- worktable 中子代理任务执行者显示为 `main:<mainSessionID>`，期望是 `subagent:<节点会话ID>`；
- 子代理看起来串行执行；节点详情出现连续 `阶段 turn #8..#29`，后续甚至到 `turn #46`；
- 节点详情“继承上下文 (Inherited Context) > 来源会话”显示主会话；
- fork 过程中出现异常（一次 `fork_subagents` 工具调用失败、多轮节点级 `llm completion failed`），期望能断点恢复而不是整体重跑。

### 阶段日志 “turn #N” 是什么

`阶段 turn #46 completion (deepseek-v4-flash)` 是节点第一视角实时流的一行 stage 事件：

- `阶段`：GUI 对 `SubagentLiveEvent.Kind == "stage"` 的标签；
- `turn`：阶段类型 `NodeStageTurn`，定义“每次 LLM 调用边界（同会话内按序递增）”（[node_result.go](../../seelebridge/internal/model/node_result.go)）；
- `completion`：Seele telemetry 中 LLM 调用的 span 名；
- `(deepseek-v4-flash)`：模型名，由 [stage_hook.go](../../seelebridge/internal/telemetry/stage_hook.go) 拼入 preview。

含义：该节点会话已连续执行第 46 次模型调用。单个 fork 节点内几十轮 LLM 是“单节点长串行”，接近节点预算上限（默认 `plan_node_max_loops`，effort high=48/max=96），不是多个并行子代理。阶段日志本身逐条带 `At`（[subagent_sessions.go](../../seelebridge/session/subagent_sessions.go) 的 `log.At = time.Now()`）；导出里所有行时间相同是导出/展示层问题。

### 为什么“看起来是 main 在执行”

1. **事件归因统一挂主会话**：框架 runner 事件缺失 session_id 时由 `correlateMainSessionID` 补主会话（[events.go](../../seelebridge/events.go)）；plan/seelex.subagent 阶段事件用 `binding.SessionID` 写主会话（[plan/events.go](../../seelebridge/plan/events.go)）；B 类摘要明确“节点会话事件按主会话 session_id 落库”（[events_unified.go](../../seelebridge/events_unified.go)）。因此详情页与事件库看到的来源会话都是主会话。
2. **task Assignee 的被动兜底**：`addTaskLocked` 在无显式 Assignee 时用 `defaultIdentity = main:<mainSessionID>`（[task.go](../../seelebridge/task/task.go)）；认领成 `subagent:<会话ID>` 由 application 的 `syncSubagentTask` 完成（[work_table.go](../../application/core/work_table.go)），依赖内存态 SubAgentTree 的 `node.SessionID`。任一环节缺失（会话未注册、投影未刷新、重启后树清空），Assignee 就停留在 main。

以上是归属与展示问题，不是执行路径降级。当前代码中未找到“子代理失败/超时后自动退回 mainagent 串行执行”的策略开关。

## 可行性分析

### 上游已提供的能力

Seele workplan 内核（v0.1.2）自带 checkpoint/resume：

- `workplan/runtime/checkpoint`：`Store` 接口、`MemoryStore`、`Manager.Save/Load`；
- `workplan/runtime/runner`：`Run()` 全量执行、`Resume(snapshotID)` 从 checkpoint 续跑；
- `workplan` 门面：`Checkpoint(id)` 注入 checkpoint 节点、`Resume(ctx, snapshotID)`。

seelex 的 `RunPlan`（[tool_provider.go](../../seelebridge/plan/tool_provider.go)）每次都 `workplan.NewFromPlan + wp.Run`，**未传 `WithCheckpoint`，也没有 Resume 入口**。上游原语可用但处于未接线状态。

### 已具备的前置（可直接复用）

| 前置 | 现状 | 证据/说明 |
|---|---|---|
| 节点级执行事实持久化 | 已具备 | A 类（plan lifecycle、`AppendNodeResult` 节点输出、`seelex.subagent` 阶段）与 B 类（llm/tool 摘要带 NodeID）落 sessionstore 事件库，可按 `session_id + node_id` 查询 |
| 确定性节点会话 ID | 已具备 | `nodeSessionID = "node-" + stableHash(systemPrompt)`（[runtime_plan.go](../../seelebridge/runtime_plan.go)），注释明确“供未来 checkpoints 定位” |
| 主会话 stack 存储 | 已具备 | `SessionContextStore` 持久化 Plan/Task/Skill/Compact 四栈（[session_context.go](../../sessionstore/session_context.go)）；主会话 DurableHistory 窗口加载已接线 |
| worktable/task 回填 | 部分 | `SessionRecord.Tasks` 随会话落盘，恢复时 `SwitchSessionTasks(record.Tasks)` 回填注册表（[session_history.go](../../application/core/session_history.go)） |
| worktree 现场保留 | 部分 | 失败/中断现场保留在磁盘，路径与分支确定（`<base>-seelex-<nodeID>`、`seelex/<nodeID>`），`NodeWorktreeInfo` 暴露恢复数据面；但 nodeID→worktree 注册表是内存态，重启后需扫描重建 |
| fork 幂等复用 | 已具备 | `reusableForkSummaries` 对“全部子代理已完成”短路复用；部分完成仍整 DAG 重跑（[fork/tool.go](../../seelebridge/fork/tool.go)） |

### 缺失的前置（真正的 gap）

| 前置 | 现状 | 影响 |
|---|---|---|
| 子代理会话历史落盘 | 缺失 | `bridge.NewAgentFactory` 默认不给节点 Session 挂 durable history（工作历史隔离）；`nodeSessionComponents()` 无 History。重启后节点会话记录丢失 |
| 子代理上下文栈/快照落盘 | 缺失 | SubagentSessions、SubagentTree、StageLogs、ContextSnapshot 全内存态；[subagent_tree.go](../../seelebridge/session/subagent_tree.go) 明确“树随进程生命周期存在，会话恢复/重启后为空” |
| 子代理压缩栈隔离 | 缺失 | 节点会话复用 `seelexController`，`Stacks`/`SessionIDProvider` 指向主会话的 `SessionContextStore`（[runtime_context.go](../../seelebridge/runtime_context.go)）；节点压缩帧会写入主会话栈，既破坏隔离也不能按节点恢复 |
| plan/run 级 checkpoint 接线 | 缺失 | `RunPlan` 无 `WithCheckpoint`/Resume；部分完成的 fork 重跑会重新执行整个 DAG |
| worktree 注册表重建 | 缺失 | 重启后需按 `git worktree list` 与 `seelex/<nodeID>` 分支扫描恢复 nodeID→path/branch/baseCommit |
| task 认领恢复 | 缺失 | 认领依赖内存 SubAgentTree；重启后 Assignee 无法自动切回 `subagent:<会话ID>` |

## 建议落地路径（规划，未实施）

按 MEMORY.md「新功能归属决策」：这是对既有子代理执行生命周期的改进，应扩展既有模块，不新开包、不塞 composition root。

1. **节点会话持久化**（`sessionstore` + `seelebridge/session`）：给 `nodeSessionComponents` 挂 `DurableHistory`（按 `node-<hash>` 路由），并给节点会话独立 `SessionContextStore`（或按 node 键分片），窗口加载与压缩帧按节点落盘。
2. **恢复锚点重建**（`seelebridge/session` + `application/core/work_table.go`）：启动/恢复时从事件轨重建 fork 树（run/node 终态、输出、阶段），回填 worktable 认领（Assignee → `subagent:<节点会话ID>`）。
3. **checkpoint 接线**（`seelebridge/plan`）：`RunPlan` 传 `workplan.WithCheckpoint(store)`，store 用 sessionstore 实现 `checkpoint.Store`；中断后经 `Resume(snapshotID)` 续跑，配合“已完成节点跳过”。
4. **worktree 索引重建**（`seelebridge/worktree`）：按 `seelex/<nodeID>` 分支或 checkpoint 记录恢复 nodeID→path/branch/baseCommit。
5. **压缩栈隔离修复**（`seelebridge/runtime_context.go`）：子代理压缩器切到节点自己的栈，避免恢复时把子代理帧混进主会话。

## 风险与边界

- 事件轨目前以主会话 session_id 为存储键，节点级恢复必须改用 `Scope.NodeID` 关联；不要把节点事件误当成“降级到主会话执行”的证据。
- 子代理会话一旦持久化，就要同步处理窗口加载、压缩、TurnArchiver 与超大工具结果归档的节点级隔离，避免把主会话上下文治理放大到 N 个节点时失控。
- checkpoint 恢复只解决“续跑”，不解决“半途副作用回滚”；worktree 现场与 git 分支是主要回滚/接管入口。
- 中断点语义需要显式定义：进程被杀（无 checkpoint 写入）只能回到最近一次 checkpoint；fork 工具级超时/取消已通过 `forkCtx` 传播（[fork/tool.go](../../seelebridge/fork/tool.go)），恢复时要与 run 终态一致。

## 验证建议

- 现有并行/归因测试保持绿色：`go test ./seelebridge/... -count=1`（重点 `node_scope_test.go`、`fork_*_test.go`、`events_unified_test.go`）。
- 新增验收口径（规划）：
  1. 节点会话历史/上下文快照在重启后可从存储重建；
  2. 部分完成的 fork 重跑只执行未完成节点，已完成节点输出复用；
  3. worktable 恢复后 Assignee 为 `subagent:<节点会话ID>`，不再停留 main；
  4. 中断后 `Resume(snapshotID)` 续跑成功，事件轨 run/node 关联连续。
- 手工验证：运行真实 fork 后杀进程重启 GUI，检查事件轨重建的节点详情与 worktree 现场。

## 参考

- [seelebridge/README.md](../../seelebridge/README.md)（模块边界）
- [seelebridge/session/README.md](../../seelebridge/session/README.md)（子代理会话/树 actor 生态位）
- [sessionstore/session_context.go](../../sessionstore/session_context.go)（四栈持久化）
- [seelebridge/plan/tool_provider.go](../../seelebridge/plan/tool_provider.go)（RunPlan 装配点）
- Seele v0.1.2：`workplan/runtime/checkpoint`、`workplan/runtime/runner`（checkpoint/resume 原语）
- 事件样本：`dist/seelex-vdev-windows-amd64-gui/.seelex/sessions-json/project-1c035123773440ea/session-e447d942e76010e0/framework-events.json`

## 实现记录（2026-08-24）

按「建议落地路径」已实现：

1. **checkpoint 接线**（`seelebridge/plan`）：`Executor` 新增
   `SetCheckpointStore/CurrentCheckpointStore`，`RunPlan` 经
   `newPlanRunner` 以 `runner.WithCheckpoint(store)` 装配；`persistCheckpoint`
   在 plan_run 结束后把最终快照（键 = 入口节点 ID）写入
   `sessionstore.CheckpointStore`；新增 `ResumePlan(ctx, snapshotID)`（Runtime
   门面 + Executor 实现）经 `runner.Resume` 从快照节点续跑，事件轨
   run/node 关联与 RunPlan 同一契约。
2. **节点会话持久化**（`sessionstore` + `seelebridge/session`）：按用户约定
   以 `<mainSessionID>-<subSessionID>.json` 落盘（主会话目录 `subagents/`
   下，主会话索引可直接列举）；`SubagentSessions` actor 运行期在注册/阶段/
   结果/终态时写记录。生命周期收敛：节点结束（done/failed）时最终结论经
   `seelex.subagent.result` 事件写入主会话事件库（"结论跟随 mainagent"），
   随后删除节点记录文件。
3. **恢复锚点重建**（`seelebridge/session` + `seelebridge` 根包 +
   `application/core`）：`Runtime.RestoreSubagentAnchors(sessionID)` 从
   残留记录恢复崩溃遗留节点（详情数据面 + fork 树 + worktree 现场），从主
   会话事件库结论事件重建已完成节点；`resumeSession` 切换/恢复时调用，
   worktable 认领回填 `subagent:<节点会话ID>`（`syncSubagentTask` 既有逻辑
   按 `node.SessionID` 认领）。
4. **worktree 索引重建**：`WorktreeManager.Restore` 从残留记录重建
   nodeID → path/branch/baseCommit（崩溃遗留现场）；`beginNodeWorktree`
   同步 `NoteWorktree` 落盘。
5. **压缩栈隔离修复**：节点上下文控制器改用独立内存栈
   （`nodeController`），子代理压缩帧不再写入主会话 `SessionContextStore`。

未落地（保持规划）：

- checkpoint 的**运行中**逐节点捕获（当前 plan_run 结束后落最终快照；进程
  被杀只能回到最近一次结束快照，需要 runner 侧 `RunWithCheckpoint` 或
  每节点 `Manager.Save` 接线）；
- 部分完成的 fork 只跑未完成节点（当前 `ResumePlan` 从快照节点重跑，
  已完成输出经 `WorkflowContext` 保留引用，未做 DAG 级跳过）；
- `NodeSessionStore` 的 SQLite/PostgreSQL/Redis 后端（当前 JSON backend 已
  实现，其它后端返回明确错误）；
- 节点级 DurableHistory 挂载（当前节点历史随记录落盘/删除，未挂框架
  `DurableHistory` 窗口加载）。
