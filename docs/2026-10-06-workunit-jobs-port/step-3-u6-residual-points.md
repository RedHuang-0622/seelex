# U6 残留写/读点 #1–#5 枚举化 + 门禁补清单（审计 §4 · wi-7）

- 角色：`progress-unify`（work item `wi-7-u6-residual`, 里程碑 `m2-audit-open`）
- 基线：main 头 **`0adef97`**（开工 `git status --porcelain` 空、`git status -sb` = 干净）
- 口径来源：用户指令 **"状态用枚举和 iota 来规范，不要用零散的字符串做硬编码比较"**；
  本批的点出自 `docs/2026-10-06-workunit-jobs-port/step-3-audit.md` §4（U6 表，14 行）。
- 同题上一波做法与纪律：`step-3-state-enum-unification-wave3.md`；总表（唯一地图）：
  `docs/arch/state-machine-inventory.md`。

---

## 1. 改动文件清单

| 文件 | 改动 | 性质 |
|---|---|---|
| `seelebridge/fork/tool.go` | `:212` / `:257` 回执 `jobs[].state` 转调契约枚举（+2 行注释） | #1 #2 生产面 |
| `seelebridge/runtime_role_turn.go` | `:490` 一帧转调；`:502`/`:504` 两帧改成**枚举类型局部变量** + 字段处 `.String()` | #3 #4 生产面 |
| `application/core/service.go` | `:125` 读方改成 `dto.ParseToolEventStatus(...) == dto.ToolEventRunning`（+3 行注释） | #5 生产面（读方） |
| `application/core/task_context/task_service.go` | `:356` `check_node` 回执 `"node_status"` 转调 `model.NodeCompleted.String()` | **本波新发现（见 §6.4）** |
| `e2e/subagent_status_vocabulary_gate_test.go` | 两格补写方/读方文件清单（+3 文件）；扫描器补**第六形态 map 状态键**；新增阴性对照函数 + 头部注释同步 | 门禁 |
| `docs/arch/state-machine-inventory.md` | §4 判据分层第 3 条 "五种形态" → "六种形态（含 map 状态键）"；§3 登记 `#6` 那一格 | 文档 |
| `docs/2026-10-06-workunit-jobs-port/step-3-u6-residual-points.md` | 本文件 | 文档 |

**未动**：`seelebridge/runtime_teamwork_board.go`（#6）、`plan/tool_provider.go`（#7）、
`events_unified.go`（#8）、`internal/telemetry/summary.go`（#9）、`tools/async_exec.go:1032`（#10）。
**未改** `dto` 取值面、未加词、未动既有用例的断言（只在门禁文件里**新增**一个测试函数）。

---

## 2. 逐点判定表

| # | 文件:行（改动前） | 判定 | 理由 | 改动后 |
|---|---|---|---|---|
| 1 | `seelebridge/fork/tool.go:212` | **转调** | 这一栏就是**后台作业状态那一格**（`dto.AsyncState`）；同文件 `:243`/`:255` 调 `deps.Jobs.Complete` 用的已经是 `dto.AsyncStateFailed`/`AsyncStateDone` —— 同一文件同一格两种写法。该文件此前**不在**门禁"后台作业状态"写方清单里 | `"state": dto.AsyncStateRunning.String()` |
| 2 | `seelebridge/fork/tool.go:257` | **转调** | 同上（结果复用档的回执，形状与真跑一批逐字段一致） | `"state": dto.AsyncStateDone.String()` |
| 3 | `seelebridge/runtime_role_turn.go:490` | **转调** | `dto.RoleToolActivity.Status` 的字段注释自己写着"取 running \| success \| error（与 `SubagentToolEvent` 同词表）"= **工具事件状态那一格**；写点写裸字面量且不在该格写方清单 | `Status: dto.ToolEventRunning.String()` |
| 4 | `seelebridge/runtime_role_turn.go:502/504` | **转调** | 同上（第二帧写点，成功/出错两支） | `status, message := dto.ToolEventSuccess, ""` / `status, message = dto.ToolEventError, info.Error.Error()`，字段处 `Status: status.String()` |
| 5 | `application/core/service.go:125` | **转调（读方）** | 读的正是 #3/#4 写的那个字段：`strings.TrimSpace(event.Status) == "running"` 是同一格的**第二份词**。改成具名读法 `dto.ParseToolEventStatus` + 枚举比较 | `if status, ok := dto.ParseToolEventStatus(strings.TrimSpace(event.Status)); ok && status == dto.ToolEventRunning` |
| 6 | `seelebridge/runtime_teamwork_board.go:305–306` | **不动，只登记** | 另一格：取值面 `running\|free`（"这个人此刻在不在干活"），`dto` 无对应枚举 | 见 §6.1 |
| 7 | `seelebridge/plan/tool_provider.go:517/521` | **保留（边界）** | 读的是框架 `workplanTypes.WorkPlanResult.NodeResults[].Status`（Seele 的词）；同文件写 `plan_run` 结果已走 `dto.PlanRunStatus` | 白名单 2 条原样（本波未动） |
| 8 | `seelebridge/events_unified.go:89` | **保留（边界）** | 框架 `SummaryEvent.Status` | — |
| 9 | `seelebridge/internal/telemetry/summary.go:27/193` | **保留（边界）** | 框架遥测状态词 | — |
| 10 | `seelebridge/tools/async_exec.go:1032` | **仍开放** | 回执状态词（`observed\|killed\|already_finished\|finished`），要连 `job_manage` 四种 op 一批收 | 白名单第 1 条原样 |

**#1/#2/#3/#4 的 wire 逐字不变**（`.String()` 给出的就是 `running`/`done`/`success`/`error`）；
**#5 的判定行为逐字不变**（认不得的词照旧当非"在跑"，即 completed 帧 —— 加注释写清，
不是静默改语义）。**没有一处对外形状变化**（`AsyncState*`/`ToolEvent*` 的词没动）。

---

## 3. 门禁先红后绿原文

### 3.0 基线（未改任何东西，门禁绿）

```
ok  	github.com/RedHuang-0622/seelex/e2e	0.253s
```

### 3.1 先补清单 → 红（**但只红了 3 点，#1/#2 没红**）

按"下一份同样的字面量会红"的要求先把三个文件写进两格的写方/读方清单，跑门禁：

```
--- FAIL: TestSubagentStatusVocabularyGate (0.05s)
    [工具事件状态] application/core/service.go:125 [状态比较] 状态词字面量 "running"：if strings.TrimSpace(event.Status) == "running" {
    [工具事件状态] seelebridge/runtime_role_turn.go:490 [状态字段] 状态词字面量 "running"：Status:        "running",
    [工具事件状态] seelebridge/runtime_role_turn.go:502 [状态赋值] 状态词字面量 "success"：status, message := "success", ""
    [工具事件状态] seelebridge/runtime_role_turn.go:504 [状态赋值] 状态词字面量 "error"：status, message = "error", info.Error.Error()
FAIL	github.com/RedHuang-0622/seelex/e2e	0.206s
```

**#1/#2 一条都没报** —— 这是本波抓到的一个**门禁自身缺口**：既有五种形态
（比较/赋值/标识符键字段/switch 分支/常量声明）都看不见
`map[string]string{"state": "running"}`，因为那里的键是**字符串字面量**而不是标识符。
"受理回执"这类载荷天生就是 map，正是本项目 `fork_subagents` 的写法。留着它，
"补了清单"也只是看着覆盖了、其实没判。

### 3.2 补扫描器第六形态（map 状态键）+ 阴性对照 → 红（六点全中）

```
--- FAIL: TestSubagentStatusVocabularyGate (0.05s)
    [后台作业状态] seelebridge/fork/tool.go:212 [map 状态键] 状态词字面量 "running"："handle": item.handle, "id": item.spec.ID, "state": "running",
    [后台作业状态] seelebridge/fork/tool.go:257 [map 状态键] 状态词字面量 "done"："handle": handle, "id": spec.ID, "state": "done",
    [工具事件状态] application/core/service.go:125 [状态比较] 状态词字面量 "running"：…
    [工具事件状态] seelebridge/runtime_role_turn.go:490 [状态字段] …
    [工具事件状态] seelebridge/runtime_role_turn.go:502 [状态赋值] …
    [工具事件状态] seelebridge/runtime_role_turn.go:504 [状态赋值] …
    [回合状态] application/core/task_context/task_service.go:356 [map 状态键] 状态词字面量 "completed"："status": "accepted", "node_id": input.NodeID, "node_status": "completed",
    [计划状态] application/core/task_context/task_service.go:356 [map 状态键] …
    [节点状态] application/core/task_context/task_service.go:356 [map 状态键] …
FAIL	github.com/RedHuang-0622/seelex/e2e	0.232s
```

（`task_service.go:356` 是三格的清单里都列了这个文件、而三格的取值面都含 `completed`，
所以同一条字面量报三次 —— 它是 §6.4 的新发现，不在 #1–#5 内。）

### 3.3 转调 #1–#5（+ §6.4 那一处）→ 绿

```
=== RUN   TestSubagentStatusVocabularyGate
--- PASS: TestSubagentStatusVocabularyGate (0.05s)
=== RUN   TestSubagentStatusVocabularyGateCatchesViolations
    --- PASS: …/状态比较 …/状态赋值 …/状态字段 …/switch_状态分支 …/状态常量声明
=== RUN   TestSubagentStatusVocabularyGateCatchesMapStateKey
--- PASS: TestSubagentStatusVocabularyGateCatchesMapStateKey (0.00s)
PASS
ok  	github.com/RedHuang-0622/seelex/e2e	0.240s
```

新增的阴性对照 `TestSubagentStatusVocabularyGateCatchesMapStateKey` 两半都验：
`map[string]string{"state": "running"}` 必须判红；`{"hint": "running", "state": "accepted"}`
（键不是状态位 / 词不在本格取值面）**不许**判红 —— 免得第六形态变成"见到熟悉的词就报"。
既有五形态的对照函数（`...CatchesViolations`）**一字未改**。

---

## 4. 命令读数

```
gofmt -l <改动文件>  → 空（先跑 -d 确认只有本波触碰的那一行重排；未顺手格式化基线未对齐的文件）
go build ./...       → 空
go vet ./...         → 空
go vet ./e2e/ ./seelebridge/ ./seelebridge/fork/ ./application/core/ ./application/core/task_context/ → 空
go test ./e2e/ -count=1                                        → ok 0.918s
go test ./seelebridge/fork/ ./seelebridge/ ./application/... -count=1
                     → exit 0；26 行全 ok（fork 2.251s；seelebridge 55.525s；application/core 31.218s；
                       application/core/task_context 1.679s；contract/dto 1.543s；其余 application/* 全 ok）
```

`e2e` 包内另有 `state_enum_cast_gate_test.go`（不许 `string(枚举值)`）随包绿。

---

## 5. 门禁 scope / 白名单读数（只增不减）

| 项 | 前 | 后 | 说明 |
|---|---|---|---|
| 格数 `statusVocabularyScopes` | 13 | **13** | 不新开格（#1–#5 全部落在既有格上） |
| 「后台作业状态」文件数 | 10 | **11** | +`seelebridge/fork/tool.go`（写方：受理回执与整批终态） |
| 「工具事件状态」文件数 | 4 | **6** | +`seelebridge/runtime_role_turn.go`（写方）、+`application/core/service.go`（读方） |
| 白名单 `allowedStatusLiterals` | 3 | **3** | **未动**：没有为过门禁放宽一条，没有新增条目 |
| 扫描形态 | 5 | **6** | +map 状态键（§3.1 的缺口）；头部注释同步 |
| 门禁文件的白名单/清单删除 | — | **0 处** | 只增不减 |

`fork/tool.go` 折作业终态时读的 `"completed"`/`"aborted"`（`:189`/`:190`）是 **plan_run 结果里的
框架节点状态词**（框架 workplan 的 `NodeBase.Status`，与 #7 同源）：它不在"后台作业状态"这一格的
**取值面**里，所以门禁按取值面判、不越界判它；这一点已写进门禁头部注释（免得后人以为是漏判）。

---

## 6. 登记（本波**不动**）+ 一处新发现

### 6.1 `#6` teammate 人状态 —— 应登记进总表 §2/§3，归哪一格**待定**

`seelebridge/runtime_teamwork_board.go:305–306`（`teamworkMemberRunning = "running"` /
`teamworkMemberFree = "free"`）——这是**另一格**：取值面 `running|free`，回答"这个人此刻在不在干活"，
`dto` 里没有对应枚举（字段是 `dto.TeamworkMemberView.Status`）。**归哪一格待定**（自成一格
`dto.TeamworkMemberStatus`，还是折进"记录状态"的读侧投影）。已按"只登记"写进
`docs/arch/state-machine-inventory.md` §3 一行；代码一行未动。

### 6.2 `#7`/`#8`/`#9` 框架词边界

`plan/tool_provider.go:517/521`（框架节点状态）、`events_unified.go:89`、`internal/telemetry/summary.go:27/193`
（框架遥测状态）——词不由我们定义，收进契约 = 框架加词我们就报错。保留，白名单 2 条原样。

### 6.3 `#10` `tools/async_exec.go:1032 "killed"` 仍开放

回执状态词（`observed|killed|already_finished|finished`），与 `job_manage` 四种 op 一批；
白名单第 1 条原样保留，**未动**。

### 6.4 新发现（**超出 #1–#5**，因第六形态暴露，已一并收口）

`application/core/task_context/task_service.go:356`：`check_node` 的回执 JSON
`{"status":"accepted","node_id":…,"node_status":"completed"}` 里那个 `"completed"` 是
**节点状态那一格**（`dto.NodeStatus`）的第二份词 —— 同一个函数往上 10 行给打点写的已经是
`model.NodeCompleted.String()`。修法是纯转调，**wire 逐字不变**（`String()` 就是 `"completed"`），
无语义变更。

明说：这一处**不在本轮工作正文的 #1–#5 里**。之所以顺手收，是因为补了第六形态之后
"不改它就只剩放宽白名单"这一条路，而红线写着"不许为了门禁好过而把白名单放宽"。
若确要严格限界，回退这一行会让门禁立刻红（`task_service.go` 那一处在三格上各判一次）。

### 6.5 未动、可留待人来定的一处

审计 §4 末尾那条口径读数（"记录状态"格声明的取值面是 `{queued,running,done,failed}`，
而 `dto.SubAgentNodeStatus` 现在有 6 个值，多 `unknown`/`interrupted`）**仍未处置** ——
要么补进门禁取值面、要么写清"它们是读侧产物、不是写侧词"。这不是缺陷，需要人判，本波不越权改。

---

## 7. 未做（明说）

- 未跑真机档（`SEELEX_LIVE_SMOKE`）与 GUI 手工冒烟（既有缺口；桌面纪律只做只读检查）；
- 未读、未提交 `config/accounts.yaml` / `*.local.yaml`；
- 未改 `step-3-audit.md` §4 表格本身（那是 wi-4 的交付物），本文件即对本批 #1–#5 的落地回答；
- 未做 `git clean`/`reset`，未顺手格式化基线未对齐的文件。
