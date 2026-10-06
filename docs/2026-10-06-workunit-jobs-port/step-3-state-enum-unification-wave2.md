# 状态机枚举统一 · 第二波（model 平行词表 + §3 剩余格）

- 口径来源（用户口令）：**"状态用枚举和 iota 来规范，不要用零散的字符串做硬编码比较"**；
  以及本波点名的那一句：**"model 这个合并，初衷是不用新拉线，错误可以直接反馈"**——
  即"平行词表并成契约一份（同一格只有一个词表、到处引它），写错词由编译器当场报"。
- 前一波（`step-3-state-enum-unification-wave1.md`）收的是三格（记录状态 / 工具事件 / task）
  + `AsyncState` / `PlanRunStatus`；本波收的是总表 §3 的**整张剩余清单**。

## 1. 结论（一格一句话）

| 格 | 处置 | 家 |
|---|---|---|
| 回合状态（可见面 `Snapshot.Task.Status` + 存档面 `TaskContextProjection.Status` + `task_context.Status*`） | **合并**（原来两份词表：progressing vs running） | `dto.TurnStatus` |
| 会话可见状态（`model.SessionStatus`） | 契约枚举；存储面留 wire + 具名转换 | `dto.SessionStatus` |
| 计划状态（`model.PlanStatus`） | 契约枚举（与 plan_run 批次结果**分两格**） | `dto.PlanStatus` |
| 节点状态（`model.NodeStatus`） | 契约枚举；框架词折一次（带 ok） | `dto.NodeStatus` |
| 定时任务上次运行结果（`scheduledStatus*`） | 契约枚举 | `dto.ScheduleRunStatus` |
| goal 状态（`goal.Status`） | 契约枚举 + 终态判定进枚举 | `dto.GoalStatus` |
| 评审者状态（`goal.PeerState`） | 契约枚举 | `dto.PeerState` |
| 恢复单元状态（`resume.UnitStatus`） | 契约枚举（粗分三桶，由记录状态折一次） | `dto.UnitStatus` |
| `workunit.StatusQueued/StatusRunning` | 值**直接引**记录状态枚举（构造上同一份） | `dto.SubAgentNodeStatus` |
| `sessionstore` 其余状态字段 | 判定为**契约之下的 wire** + 逐格具名转换点；唯一的跨域判据（`terminalStackStatus`）逐词登记主人 | store 侧 |

刻意不动的两处写在总表 §2：工具调用视图词（`model.ToolCall.Status`，要连读方清单一起收）、
todo 三态（`dto.TodoItemStatus`，已在移除窗口内）。

## 2. 红 → 绿（原文）

### 2.1 回合状态：同一个回合说了两个词（M1）

先写会红的用例（`application/core/turn_status_single_word_test.go`：在飞回合的**可见面**与
**存档面**必须说同一个词），跑：

```
--- FAIL: TestTurnStatusSpeaksOneWordOnSnapshotAndArchive (0.00s)
    turn_status_single_word_test.go:45: 同一个回合状态说了两个词：存档面 "running"、可见面 "progressing"
    —— 这就是平行词表，两处只差一个手写映射，没有编译器看得见
FAIL	github.com/RedHuang-0622/seelex/application/core
```

第二处红是**编译级**（落盘读回还没收口）：

```
application\core\turn_status_single_word_test.go:58:19: undefined: TurnProgressing
application\core\turn_status_single_word_test.go:67:13: undefined: turnStatusOfRecord
... too many errors
```

改完（契约一格 + `TurnStatusOfRecord` 读回老词 `running`）：

```
--- PASS: TestTurnStatusSpeaksOneWordOnSnapshotAndArchive (0.00s)
--- PASS: TestTurnStatusOfRecordKeepsLegacyRunningAndNeverFoldsToTerminal (0.00s)
ok  	github.com/RedHuang-0622/seelex/application/core	0.243s
```

### 2.2 节点状态：阶段词把跑完的节点显成"待开始"（M4，真缺陷）

`AppendNodePhase(nodeID, "worktree_unmerged")` 与节点状态走**同一个事件字段**，而旧写法
`node.Status = PlanNodeStatus(event.Status)` 的 default 是 `pending`：一个跑完并交付
产出的节点，在收尾警告到达之后被显成"待开始"，还被从计划进度里扣掉（Progress 1 → 0）。

把旧写法（`else { node.Status = model.NodePending }`）临时放回去，跑回归用例
（`application/core/plan_node_phase_word_test.go`）：

```
--- FAIL: TestPlanNodePhaseWordDoesNotOverwriteNodeStatus (0.00s)
    plan_node_phase_word_test.go:48: 协调器投影（后台路径）：阶段词覆盖了节点状态 pending
    （跑完的节点被显成 pending）
FAIL	github.com/RedHuang-0622/seelex/application/core	0.235s
```

改成"只有**节点状态词**才推进节点状态"（`PlanNodeStatus` 带 ok；时间线保留事件原始词）：

```
ok  	github.com/RedHuang-0622/seelex/application/core	0.225s
```

### 2.3 `string(枚举值)` 静默陷阱（本轮踩到的，全部抓到）

枚举化之后 `string(x)` 照样编译过、`go vet` 也不报，但得到的是控制字符。本轮抓到并修掉的位置：

| 位置 | 症状 | 修法 |
|---|---|---|
| `task_context_state.go` checkpoint 汇总 | `string(model.NodeCompleted)` → `"\x07"`，Switch 匹配不上 → 跑完的节点 `CompletedWork` 为空 | `.String()` |
| `plan_tools.go` 建 checkpoint / 终态判定 / 事件写出 | `string(NodePending)`、`"failed"` 字面量 | `.String()` / 引常量 |
| `work_table.go` 证据前缀 | `"node:"+string(node.Status)` | `.String()` |
| `tui/plan.go` 节点上色 + `tui/goalteam.go` 作业计数 | `string(n.Status)`；`case "failed","killed"` | 参数改成枚举 / 引 `dto.AsyncState` |
| `seelebridge/task/tools.go` JSON 回复 | `"status": string(record.Status)` → 回复里是控制字符 | `.String()` |
| `gui/fork_live_probe_test.go` | 探针输出里的状态是控制字符 | `.String()` |
| goal 四处存档写出 + 三处测试 | `string(record.Status)` / `string(StatusCompleted)` | `.String()` |
| 两个既有用例（`session_archive_test.go` / `task_execution_test.go`） | `Checkpoint(..., string(NodeCompleted), ...)` → 用例先红：`restored projection ... CompletedWork 数量对不上` | `.String()` |

门禁侧：`stateFieldCastWhitelist` 从 3 条收到 **1 条**（旧 3 条里 2 条是 `work_table.go` 的
`string(node.Status)`、1 条是 `task_service.go` 的 `string(plan.Status)`——三处都因为
"那一格收成枚举"而必须改成 `.String()`，过期条目被门禁逼着删掉）。

## 3. 改动清单（文件 → 干了什么）

契约（新增 8 格，全部 `type uint8 + iota + words + 四方法转调 stateCodec`）：

- `application/contract/dto/turn_status.go`、`session_status.go`、`plan_status.go`、
  `node_status.go`、`schedule_run_status.go`、`goal_status.go`、`peer_state.go`、`unit_status.go`；
- `dto/node_status.go` 里的 `NodeStatusFromFramework`（框架词折一次，**带 ok**）；
- `dto/projection.go`：`GoalGovernanceView.Status/PeerState` 由 `string` 换成枚举。

具名转换点（落盘/边界，一格一个）：

| 转换点 | 家在 | 口径 |
|---|---|---|
| `TurnStatusOfRecord` | `application/core/task_context` | 老词 `running` → `Progressing`；认不得/空 → `Unknown`（不是终态） |
| `sessionStatusOfRecord` | `internal/adapters` | 认不得/空 → `SessionStatusUnknown`（不折成 idle/running） |
| `goal.StatusOfRecord` | `application/core/goal` | 认不得/空 → `GoalStatusUnknown`（随后 Reload 的位置语义修正栈顶 active） |
| `nodeStateOfRecord`（上一波） | `seelebridge` | 认不得 → `SubAgentUnknown`（不炸、不当终态） |

写方/读方迁移（按格）：

- 回合状态：`chat.go`、`history_safety.go`、`input.go`、`session_cold_read.go`、
  `context_runtime/coordinator.go`、`task_context/*`（含 `_TaskStateFor` 里那两行手写映射**删除**）。
- 会话可见状态：`service_snapshot.go`、`session_history.go`、`session_draft.go`、
  `session_lifecycle.go`、`archive_session.go`、`resident_lru.go`、`session_runtime/{archive,scope}.go`、
  `internal/adapters/session_workspace_ports.go`；`tui/view.go` 的 `awaiting_approval` 字面量收掉。
- 计划/节点状态：`task_context/plan_projection.go`（计划级判定折成节点状态再判）、
  `task_context/plan_transcript.go`（`PlanNodeStatus` → 带 ok 的折词）、`plan_tools.go`（4 处写方）、
  `work_table.go`（打点）、`subagent_view/coordinator.go`（详情状态，`""` 哨兵改 `NodeUnknown`）、
  `tui/plan.go`（上色参数改枚举）、`seelebridge/node/agent_node.go`（状态词引契约 +
  阶段词落 `node_phase_words.go`）。
- 其余格：`seelebridge/scheduler/scheduler.go`、`application/core/goal/*`（含审计/看板/存档）、
  `application/core/resume/resume.go`、`application/core/goal_coordinator.go`、`tui/goalteam.go`、
  `seelebridge/workunit/session.go`（「在跑」子集引契约）。
- store 侧（`sessionstore`）：`terminalStackStatus` 逐词登记主人、`BoundaryStatus` 收成具名常量。

门禁：

- `e2e/subagent_status_vocabulary_gate_test.go`：新增 **7 格**范围（回合 / 会话可见 / 计划 /
  节点 / 定时任务上次运行结果 / goal / 评审者）；「后台作业状态」补上 `tui/goalteam.go` 这个读方；
  白名单从 3 条增到 4 条（新增节点格里"工具调用视图词"那一条，写清同词不同格与去向）。
- `e2e/state_enum_cast_gate_test.go`：白名单 3 → 1 条。

## 4. 既有用例的机械适配清单（逐条列明理由）

| 文件 | 适配 | 理由 |
|---|---|---|
| `application/core/{context_compact*,context_hook,session_archive,session_cold_read,service_chat,task_execution,task_service}_test.go` | `model.TaskX` / `task_context.StatusX` / 裸 `TaskX` → `TurnX` / `dto` 常量 | 类型改名/合并后的机械改名，断言语义未动 |
| `application/core/session_archive_test.go`、`session_runtime/catalog_title_realstore_test.go` | 字符串字段处补 `.String()` / 用 `dto.ParseSessionStatus` 复刻边界口径 | 枚举 → 存档 wire 的显式转换 |
| `application/core/{session_archive,task_execution}_test.go` | `Checkpoint(..., string(NodeCompleted), ...)` → `.String()` | 同上（不改这个就会把 checkpoint 状态写成控制字符） |
| `application/core/subagent_view/adapt_test.go` | `nodeStatusFromTaskStatus("mystery") != ""` → `!= model.NodeUnknown` | "未知"的表示从空串换成枚举零值，语义相同 |
| `application/core/work_table_test.go`、`work_table_ab_test.go` | `PlanNodeEventInfo.Status: NodeX` → `NodeX.String()`；`NodeStatus("running")` → `NodeRunning.String()` | 时间线字段按设计保留**事件的原始词**（字符串） |
| `application/core/task_service_test.go` | `Events[0].Status != NodeCompleted` → `!= NodeCompleted.String()` | 同上 |
| `application/core/{session_status,session_runtime,migration,gui/headless_goal,tui/goalteam,tui/tui,repro_*_test.go}` | 引入枚举常量 / `.String()` | 同上 |
| `tui/goalteam_test.go` | 夹具的 `Status: "running"` → `dto.GoalActive`，期望 `"running"` → `"active"` | 夹具原来用的是一个**goal 状态格里不存在的词**；枚举化把它挡在门外，夹具必须用真词（这是唯一一处改了"期望值"的适配，逐条写明） |

## 5. 读数

### 5.1 全量（本轮最终，工作区干净）

```
gofmt -l application seelebridge gui tui sessionstore internal e2e main.go
  （只剩 6 个本轮未触碰的既有文件，见 §6.4；本轮改过的文件全部干净）
go build ./...   → 空
go vet ./...     → 空
go test ./... -count=1
  → exit 0，85 行，0 FAIL（含根包 headless 真实装配冒烟、gui、gui/terminal、tui、
    sessionstore、seelebridge 全族、e2e 两张门禁、e2e/scenario、workspace、plugin、mcpstack）
```

提交：`e3b2d4a`（回合状态）→ `d78a7b6`（会话可见状态）→ `2f74815`（计划 + 节点状态，
含阶段词覆盖真缺陷修复）→ `9efac91`（workunit「在跑」引契约）→ `2efb062`（定时任务上次
运行结果）→ `d227934`（goal + 评审者）→ `f8782b1`（sessionstore 收口）；每个提交都跑过
对应包的用例与两张门禁，最后再做一次全量。

### 5.2 性能量级（**不主张快慢**）

同一支 benchmark（与上一波同一口径）：

```
BenchmarkTeamworkBoardSnapshot-8    6949    177173 ns/op    167513 B/op    1067 allocs/op
（上一波同一支：                   7707    164161 ns/op    167514 B/op    1067 allocs/op）
```

分配次数与字节数**逐项对齐**（1067 allocs/op、167513 vs 167514 B/op）——枚举化没有引入
新的堆分配（词表是数组常量，`.String()` 只是下标取值）；ns/op 同量级（本次高约 8%，在本机
跑间噪声范围内，**不主张快慢**）。

### 5.3 冒烟

- **有**：根包 `go test .` 的 headless 真实装配冒烟（真实 Dependencies + 真实 runtime 装配，
  会话/plan/task 链路走一遍）——本轮全量里绿（`ok github.com/RedHuang-0622/seelex 45.278s`）。
- **没有**：真 API 冒烟（`SEELEX_LIVE_SMOKE=1` + `config/accounts.yaml`）与 GUI 手工点按冒烟。
  两波同一缺口，写在 §6.3。


## 6. 未决项

1. **工具调用视图词**（`model.ToolCall.Status`：running|completed|failed）刻意不枚举：它不是判定面
   （快照/transcript wire 的展示口径），且与"工具事件状态"（running|success|error）同词不同格；
   收它要连读方清单一起做（总表 §2 已登记）。
2. **todo 三态**（`dto.TodoItemStatus`）刻意不枚举：它在自己的移除窗口内（权威状态是
   `TaskRecord.Status`），唯一转换点是 `seelebridge/task.TodoToTaskStatus`。
3. **真 API 冒烟**（`SEELEX_LIVE_SMOKE=1` + `config/accounts.yaml`）与 **GUI 手工点按冒烟**：
   本轮与上一轮同一缺口（桌面纪律：只读检查、不合成输入），未做。
4. **`gofmt -l` 对 6 个本轮未触碰的既有文件仍报**（`sessionstore/teamwork_items.go`、
   `sessionstore/board_test.go`、`seelebridge/teamwork/teamwork.go`、`seelebridge/runtime_teamwork_board*.go`、
   `application/core/teamwork_board_projection_test.go`）：是 HEAD 就有的对齐问题，本轮按纪律
   **不顺手格式化无关文件**，只报告。
