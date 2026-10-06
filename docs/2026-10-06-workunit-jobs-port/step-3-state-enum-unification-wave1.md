# 状态机枚举统一 · 第一波落地记录

- 用户口径：**"状态用枚举和 iota 来规范，不要用零散的字符串做硬编码比较"**；以及
  "记录状态 / 工具事件词 / 工具调用词 / 回执词**都是一件事**：状态机没有枚举统一"。
- 地图（每一格在哪、谁还没做）：`docs/arch/state-machine-inventory.md`。本文是**这一波的落地记录**。
- 基线：`d8a6619`。**只换类型形状**：不动状态机语义、不并表、不改两张作业表与表机制。

## 1. 改动清单

### 1.1 唯一一份编码口径（把"每格各写一遍"收成一处）

| 文件 | 改动 |
|---|---|
| `application/contract/dto/state_codec.go`（新） | `stateCodec`：`word` / `ordinal` / `marshal` / `unmarshal` 四个方法一份实现（越界折回"未知"、错误消息带格子名、未知词报错且**不动原值**）。每格只剩"类型 + words 表 + 4 个转调方法" |
| `dto/async_run.go`、`dto/plan_run.go` | 上一批手写的 `String/Parse/Marshal/Unmarshal`（各一遍）改成转调 `stateCodec` |

### 1.2 新枚举化的三格（字段类型 + 写方 + 读方）

| 文件 | 改动 |
|---|---|
| `dto/subagent.go` | **记录状态**：`SubAgentNodeStatus` 从 `type string` → `uint8 + iota`（`SubAgentUnknown/Queued/Running/Done/Failed/Interrupted`）+ `subAgentNodeStatusWords` + 4 方法 |
| `dto/subagent_recovery.go` | `SubagentRecoveryView.Status string` → `SubAgentNodeStatus`（恢复投影也进类型） |
| `dto/subagent_live.go` | **工具事件状态**：新增 `ToolEventStatus`（`Unknown/Running/Success/Error`）；`SubagentTool.Status`、`SubagentToolEvent.Status` 两处字段换类型 |
| `dto/task.go` | **task 状态**：`TaskStatus` 从 `type string` → `uint8 + iota`（8 词 + `TaskStatusUnknown`）；`TaskTracePoint.Status` 换类型（`TaskRecord.Status` 本来就是 `TaskStatus`） |
| `seelebridge/session/tool_events.go` | 写：`Status: "running"/"success"/"error"` → 枚举 |
| `application/core/tool_hooks.go` | 写：`status := "success"/"error"` → 枚举；写 `model.ToolCall`（wire）处显式 `.String()` |
| `application/core/subagent_view/coordinator.go` | 读：`e.Status == "running"` → 枚举；`nodeStatusFromTaskStatus` 的 6 个字面量 case → 契约枚举的对外词（边界映射，subject 是工作表格行的混合字符串） |
| `tui/state.go` | 读：状态图标 switch 的三个 `case "running"/"success"/"error"` → 枚举对外词 |
| `seelebridge/task/task.go` | 写：`string(TaskPending)` / `string(status)` → 枚举值原样（打点与条目同格） |
| `application/core/work_table.go` | 读：`string(record.Status)` → `.String()`；`point.Status` / `tool.Status` → `.String()`（视图行是**混合字段**：task 行 + 子代理行沿用 done/failed 的显示映射，保持字符串） |
| `seelebridge/runtime_subagent_recovery.go`、`runtime_subagent_resume.go` | **落盘格的唯一转换点** `nodeStateOfRecord(wire string) dto.SubAgentNodeStatus`（认不得的词 → `SubAgentUnknown`，**不炸、也不是终态**）；三处判终态的读点改成类型比较 |
| `seelebridge/workunit_team.go` | `teamUnitStatusDone/Failed` 从 const 改 var（`dto.SubAgent*.String()` 不是常量表达式），词仍只有契约一处 |
| `gui/headless_subagent_test.go`、`application/core/service_plan_test.go` 等 | 既有用例的**机械适配**（见 §4） |

### 1.3 门禁

| 文件 | 改动 |
|---|---|
| `e2e/subagent_status_vocabulary_gate_test.go` | 加两格范围：**工具事件状态**（写方/读方 4 个文件 + 取值面）、**task 状态**（2 个文件 + 8 词取值面） |
| `e2e/state_enum_cast_gate_test.go`（新） | **枚举化之后的静默陷阱**门禁：全仓禁 `string(<枚举值>)`（常量/本层别名形式），另有"声明过状态面的文件里禁 `string(x.Status)`"这一条启发式判据（带两条登记理由的例外） |

## 2. 删除清单（旧写法 → 去向 → 新唯一位置）

| 旧位置 | 旧写法 | 去向 | 新唯一位置 |
|---|---|---|---|
| `dto/subagent.go` | `SubAgentQueued/Running/Done/Failed` 四个 `= "字面量"` | 删除（升级成 iota） | 同文件 `subAgentNodeStatusWords` |
| `dto/task.go` | `TaskPending…TaskInterrupted` 八个 `= "字面量"` | 删除（升级成 iota） | 同文件 `taskStatusWords` |
| `dto/subagent_live.go` | 两个 `Status string` 字段（无词表） | 删除字段的字符串形态 | `dto.ToolEventStatus` + `toolEventStatusWords` |
| `seelebridge/session/tool_events.go:101,106,110` | `Status: "running"/"success"`、`= "error"` | 删除字面量 | `dto.ToolEvent*` |
| `application/core/tool_hooks.go:174,176` | `status, errorText := "success", ""` / `= "error", …` | 删除字面量 | `dto.ToolEventSuccess/Error` |
| `application/core/subagent_view/coordinator.go:283-292` | `case "pending"/"queued"/"running","doing"/"completed","done"/"failed","interrupted"` | 删除字面量 | 契约枚举的对外词（`dto.Task*.String()` / `dto.SubAgent*.String()`） |
| `tui/state.go:35,37,39` | `case "running"/"success"/"error"` | 删除字面量 | `dto.ToolEvent*.String()` |
| `seelebridge/task/task.go:360,427` | `string(TaskPending)` / `string(status)` | 删除转换 | 枚举值（字段已是 `TaskStatus`） |
| `application/core/work_table.go:81` | `status := string(record.Status)` | 删除（**这是本波踩到的静默陷阱**） | `record.Status.String()` |
| `seelebridge/runtime_subagent_resume.go:355-356` | `subagentNodeStatusDone/Failed = string(dto.SubAgentDone)`（const，得到控制字符） | 删除（改成具名转换点） | `nodeStateOfRecord` |
| `seelebridge/runtime_subagent_recovery.go:97`、`runtime_subagent_resume.go:330,527` | 拿落盘字符串比 `subagentNodeStatus*` | 删除 | `nodeStateOfRecord(...) == dto.SubAgentFailed` 等**类型**比较 |
| `dto/async_run.go`、`dto/plan_run.go` | 每格各写一遍四个方法 | 删除（收成一份） | `state_codec.go` 的 `stateCodec` |

## 3. 红 → 绿

### 3.1 编译级红（枚举化的正常入口：字段换类型 → 编译器列出所有判定面）

```
$ go test ./application/contract/dto/ -run "Enum|Wire|Rejects" -count=1
application\contract\dto\state_machines_test.go:117:10: undefined: JobReceiptStatus   # ← 见 §5：回执照旧不枚举
$ go build ./...
seelebridge\task\task.go:360:21: cannot use string(TaskPending) (constant "\x01" of type string) as dto.TaskStatus value in struct literal
seelebridge\task\task.go:427:84: cannot use string(status) ... as dto.TaskStatus value
seelebridge\session\tool_events.go:101:13: cannot use "running" (untyped string constant) as dto.ToolEventStatus value in struct literal
…
```

### 3.2 **静默陷阱**的红（编译器与 `go vet` 都漏，是用例先红抓到的）

`string(枚举值)` 在枚举化之后**照样编译过、`go vet ./...` 也不报**，但得到的是控制字符：

```
# 全仓测试（跑在修之前）
--- FAIL: TestBuildWorkTableMapsPlanNodes     # row.Status = "\x05"（want "completed"）
--- FAIL: TestBuildWorkTableMapsTodoItems     # row.Status = "\x01"（want "pending"）
--- FAIL: TestBuildWorkTableMapsSubagentTasks
--- FAIL: TestUpdateWorkItemStatusTodoThreeStates
--- FAIL: TestRefreshWorkTableSnapshotPublishesSubagentRows
--- FAIL: TestRestoredCrashLeftoversMarkedInterrupted   # 落盘记录判终态失配
--- FAIL: TestSubagentConclusionFollowsMainAndAnchorsRebuild
```

修法（两处根因）：`application/core/work_table.go:81` 的 `string(record.Status)` → `.String()`；
`seelebridge` 的 `string(dto.SubAgentDone)` 别名 → `nodeStateOfRecord` 这一个具名转换点。
并补了 `e2e/state_enum_cast_gate_test.go`（20 条以内的小门禁）把这个形态钉死。

### 3.3 新门禁先红 → 绿

```
$ go test ./e2e/ -run "TestSubagentStatusVocabularyGate$" -count=1
--- FAIL: TestSubagentStatusVocabularyGate
  [工具事件状态] application/core/subagent_view/coordinator.go:287 [switch 状态分支] 状态词字面量 "running"：case "running", "doing":
  [工具事件状态] application/core/tool_hooks.go:49  [状态字段] "running"：tool := &ToolCall{… Status: "running"}
  [工具事件状态] application/core/tool_hooks.go:53  [状态字段] "running"
  [工具事件状态] tui/state.go:35 [switch 状态分支] "running"：case "running":
  [工具事件状态] tui/state.go:37 [switch 状态分支] "success"：case "success":
  [工具事件状态] tui/state.go:39 [switch 状态分支] "error"：case "error":
$ go test ./e2e/ -run "TestSubagentStatusVocabularyGate$" -count=1
ok  	github.com/RedHuang-0622/seelex/e2e	0.402s
```

**六处命中没走白名单，全部改成引契约**（白名单只留给"确认是另一张表"的位置）。

## 4. 既有用例的机械适配（断言语义逐条对照）

| 文件:行 | 旧写法 | 新写法 | 为什么不改语义 |
|---|---|---|---|
| `seelebridge/session/tool_events_test.go:18,24` | `Status: "running"/"success"` | 枚举常量 | 断言仍是"回调与订阅各收到一次" |
| `seelebridge/subagent_events_test.go:46,49,72` | `!= "running"/"success"/"error"` | 枚举常量 | 断言仍是三种状态的投影 |
| `application/core/service_plan_test.go:255,259,269,292` | `Status: "running"` / `= "success"` / `!= "success"` | 枚举常量 | 断言仍是"投影里状态=成功、参数与结果被截断" |
| `application/core/work_table_test.go:30,170,177,178,196` | `Status: "success"/"running"`、trace 样本词 `"done"` | 枚举常量 / `dto.TaskCompleted.String()` | 断言仍是"行状态镜像/打点保序"；样本词从**不在格子里的自由字符串**换成格子里的词（old `"done"` 在 task 格根本不存在），第 193 行 `s1a.Status != "done"` 的期望**没动**（它来自子代理行的显示映射） |
| `application/core/work_table_race_test.go:51`、`work_table_ab_test.go:75` | `Status: "success"` | 枚举常量 | 断言仍是并发/AB 下的行内容 |
| `application/core/subagent_detail_lock_regression_test.go:35,57` | `Status: "running"` | `dto.SubAgentRunning` / `dto.ToolEventRunning`（**两格各一处**） | 断言仍是锁序不反转 |
| `application/core/session_cold_read_test.go:90` | `Status: "done"` | `dto.TaskCompleted` | 该行只做冷读种子；断言只看 `Task.Summary`（"done"），与条目状态无关 |
| `application/core/work_table_subagent_status_test.go:22` | `sub: ""`（空字符串=认不得） | `sub: dto.SubAgentUnknown`（**声明过的"未知"值**） | 断言就是"认不得按保守口径标 interrupted"——口径一字未动 |
| `seelebridge/task/registry_test.go:53` | `SetStatus("todo:1", "whatever", "")` | `SetStatus("todo:1", TaskCompleted, "")` | 该值只是填充：断言是"未知 **id** 被拒（not found）" |
| `gui/bridge_test.go:1215,1257`、`gui/subagent_route_smoke_test.go:57,60`、`gui/headless_subagent_test.go:52` | 状态字面量 | 枚举常量（写 `model.ToolCall` 那处用 `.String()`） | 断言仍是事件转发与投影 |
| `seelebridge/node_first_person_live_smoke_test.go:190` | `tool.Status == "success"` | 枚举常量 | 同上（该用例默认 SKIP，见 §6） |
| `seelebridge/stage_preview_judgment_test.go:130-131` | 两行"本层别名 == 契约词" | **删除那两行** | 别名已不存在（改成 `nodeStateOfRecord`），保留会是恒真断言；其余 9 行（workunit / teamUnit / session 的再导出）**原样保留**，互锁仍在 |

## 5. 刻意**不枚举**的一格（原计划被证据推翻 → 改口径）

回执状态（`asyncPayload.Status`：accepted | observed | killed | already_finished | retired）**不做枚举**。
理由（清点写方之后的事实）：同一个字段里还流着 Seele `jobs.Manager` 生产的词
（`progress` / `finished`，来自 `manager.Status(...)` 的载荷）。把框架的词收进我们的契约枚举 = 框架加词
我们就 `Unmarshal` 报错。这一格的口径改为：**边界字段**（保持字符串 + 工具侧只写我们自己那五个词 +
门禁白名单逐条登记理由）。原计划"连 job_manage 的四种 op 一起收"因此作废，登记为
`state-machine-inventory.md §2` 的边界格。

## 6. 命令读数

| 命令 | 读数 |
|---|---|
| `gofmt -l`（本轮改动文件） | 无输出 |
| `go build ./...` | 无输出 |
| `go vet ./...` | 无输出 |
| `go test ./application/core/ -count=1` | `ok … 15.8s` |
| `go test ./seelebridge/ -count=1` | `ok … 30.6s` |
| `go test ./e2e/ -count=1` | `ok … 0.892s`（两条门禁全绿） |
| `go test ./... -count=1`（全仓，含根包 main / gui / tui / sessionstore） | 见文末"全量读数" |
| `go test ./seelebridge/ -run TestNodeFirstPersonLiveSmoke -v` | `--- SKIP`（真 API 冒烟要 `SEELEX_LIVE_SMOKE=1` + `config/accounts.yaml`；**未覆盖**，与前两波同一缺口） |
| 性能热点量级 | 本波无新增热路径：改的是"字符串比较 → 整数比较"与边界上的 `.String()`。上一波同口径读数：`BenchmarkTeamworkBoardSnapshot-8  7707  164161 ns/op  167514 B/op  1067 allocs/op`（**不主张快慢**） |
| 全局冒烟 | 可用的入口：`go test .`（根包 headless 真实装配冒烟，覆盖 teammate 作业行 + 会话落盘 + 工具事件链）；GUI 手工点按**未做**（桌面纪律：只读检查、不合成输入），与前两波一致登记 |

## 7. 未决项

1. **`workunit.Status*`**：与记录状态同词面（守卫用例逐词断言相等）——先判定"是不是同一格"，是则转调契约。
2. **`application/model` 的平行词表**（`model.TaskStatus` / `model.SessionStatus*` / `model.NodeStatus`）：同名不同机器且与 `dto` 平行，是"状态机没有枚举统一"的核心症状；要逐格定"合并 / 改名 / 登记为独立格"。
3. `application/core/task_context` 的 `StatusRunning/StatusFailed`、`seelebridge/scheduler` 的 `scheduledStatus*`、`sessionstore` 其余状态字段（fork_store / pending_tail / attempt_cache / board / compact_frames / conversation / project_record）、goal 的 `Status`/`PeerState`/看板 `open|closed`。
4. **落盘格的未知词**：记录状态那一格已定（`nodeStateOfRecord` → `SubAgentUnknown`，用例钉住"不等于终态"）；其余落盘格要照同一个口径逐格回答。
5. `string(枚举值)` 门禁的**第一条判据是启发式**（只认枚举常量名 + 声明过状态面的文件里的字段形态，不是类型系统）。真正的兜底仍是：字段类型（编译器）→ 用例 → 门禁。
